/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

/*
 * This daemon will periodically check the Brightgate cloud for updates to
 * blocklists, etc.  We currently assume that each dataset to be updated has a
 * 'latest' file, which contains the URL of the freshest data.  If an update is
 * found, the daemon will download it and modify the @/updates/<name> config
 * property, which will trigger the consuming daemon to refresh its internal
 * state.
 *
 * Future work:
 *
 *    - Rather than having a 'latest' file for each dataset, we might want to
 *      investigate having a single manifest itemizing the latest versions of
 *      each dataset.
 *
 *    - We currently have no versioning on the datasets.  There should be some
 *      annotation indicating minimum/maximum client versions a dataset can be
 *      consumed by.  Alternatively, each dataset could have a format version
 *      and the consuming software would specify the formats that it supports.
 *      Either way, we would need some sort of manifest rather than the simple
 *      'latest' file.
 *
 *    - There should be some dataset-specific callback to invoke when a dataset
 *      is refreshed.  This will probably be needed when we start downloading
 *      the vulnerability database, as we will need the ability to download new
 *      exploit-detection scripts as well.
 */

package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/cloud_rpc"
	"bg/common/archive"
	"bg/common/cfgapi"
	"bg/common/grpcutils"
	"bg/common/urlfetch"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

const (
	pname = "ap.updated"
)

var (
	brokerd       *broker.Broker
	config        *cfgapi.Handle
	rpcConn       *grpc.ClientConn
	storageClient cloud_rpc.CloudStorageClient
	applianceCred *grpcutils.Credential

	updatePeriod = flag.Duration("update", 10*time.Minute,
		"frequency with which to check updates")
	uploadPeriod = flag.Duration("upload", 5*time.Minute,
		"frequency with which to upload refreshed data")

	uploadTimeout = flag.Duration("timeout", 10*time.Second,
		"time allowed for a single file upload")
	uploadErrMax = flag.Int("emax", 5,
		"upload errors allowed before we give up for now")
	uploadBatchSize = flag.Int("bsize", 5, "upload batch size")

	connectFlag   = flag.String("connect", "", "Override connection endpoint in credential")
	enableTLSFlag = flag.Bool("enable-tls", true, "Enable Secure gRPC")
	deadlineFlag  = flag.Duration("rpc-deadline", time.Second*20, "RPC completion deadline")

	updateBucket string
)

type updateInfo struct {
	localDir   string
	localName  string
	latestName string
}

var updates = map[string]updateInfo{
	"ip_blocklist": {
		localDir:   "/var/spool/watchd/",
		localName:  "ip_blocklist.csv",
		latestName: "ip_blocklist.latest",
	},
	"dns_blocklist": {
		localDir:   "/var/spool/antiphishing/",
		localName:  "dns_blocklist.csv",
		latestName: "dns_blocklist.latest",
	},
	"dns_allowlist": {
		localDir:   "/var/spool/antiphishing/",
		localName:  "dns_allowlist.csv",
		latestName: "dns_allowlist.latest",
	},
	// "vulnerabilities": {
	// localDir:   "/var/spool/watchd/",
	// localName:  "vuln-db.json",
	// latestName: "vuln-db.latest",
	// },
}

type uploadType struct {
	dir    string
	prefix string
	ctype  string
}

var uploadTypes = []uploadType{
	{
		"/var/spool/watchd/droplog",
		"drops",
		archive.DropContentType,
	},
	{
		"/var/spool/watchd/stats",
		"stats",
		archive.StatContentType,
	},
}

type oneUpload struct {
	source    string
	signedURL *cloud_rpc.SignedURL
	uType     uploadType
}

func configBucketChanged(path []string, val string, expires *time.Time) {
	if len(path) == 3 {
		switch path[1] {
		case "update":
			updateBucket = val
			log.Printf("Changed update bucket to %s\n", val)
		default:
			log.Printf("unrecognized bucket: %s (%v)\n", path[1], path)
		}
	}
}

func refresh(u *updateInfo) (bool, error) {
	targetDir := aputil.ExpandDirPath(u.localDir)
	target := targetDir + u.localName
	latestFile := "/var/tmp/" + u.latestName
	metaFile := latestFile + ".meta"

	if !aputil.FileExists(targetDir) {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return false, fmt.Errorf("failed to create %s: %v",
				targetDir, err)
		}
	}

	url := updateBucket + "/" + u.latestName
	metaRefreshed, err := urlfetch.FetchURL(url, latestFile, metaFile)
	if err != nil {
		return false, fmt.Errorf("unable to download %s: %v", url, err)
	}

	dataRefreshed := false
	if metaRefreshed || !aputil.FileExists(target) {
		b, rerr := ioutil.ReadFile(latestFile)
		if rerr != nil {
			err = fmt.Errorf("unable to read %s: %v", latestFile, err)
		} else {
			sourceName := string(b)
			url = updateBucket + "/" + sourceName
			_, err = urlfetch.FetchURL(url, target, "")
		}
		if err == nil {
			log.Printf("Updated %s\n", target)
			dataRefreshed = true
		} else {
			os.Remove(metaFile)
		}
	}

	return dataRefreshed, err
}

func refreshOne(name string) {
	update := updates[name]
	refreshed, err := refresh(&update)
	if err != nil {
		log.Printf("Failed to update %s: %v\n", name, err)
	} else if refreshed {
		log.Printf("%s refreshed\n", name)
		tstr := time.Now().Format(time.RFC3339)
		prop := "@/updates/" + name
		config.CreateProp(prop, tstr, nil)
	}
}

func updateLoop(wg *sync.WaitGroup, doneChan chan bool) {
	var done bool

	updateBucket, _ = config.GetProp("@/cloud/update/bucket")
	if updateBucket == "" {
		log.Printf("no update bucket defined\n")
	}

	refreshSig := make(chan os.Signal, 1)
	signal.Notify(refreshSig, syscall.SIGHUP)

	ticker := time.NewTicker(*updatePeriod)
	for !done {
		if updateBucket != "" {
			for name := range updates {
				refreshOne(name)
			}
		}

		select {
		case <-refreshSig:
			log.Printf("Received SIGHUP.  Refreshing updates.\n")
		case <-ticker.C:
		case done = <-doneChan:
		}
	}
	wg.Done()
}

// PUT one file to the provided URL
func upload(client *http.Client, u oneUpload) error {
	data, err := os.Open(u.source)
	if err != nil {
		return fmt.Errorf("failed to open %s: %v", u.source, err)
	}
	defer data.Close()

	req, err := http.NewRequest("PUT", u.signedURL.Url, data)
	if err != nil {
		return fmt.Errorf("failed to create PUT request: %v", err)
	}
	req.Header.Set("Content-Type", u.uType.ctype)

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("PUT failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		buf := make([]byte, 256)
		res.Body.Read(buf)
		return fmt.Errorf("PUT failed: %s (%s)", res.Status, string(buf))
	}
	// Make sure to read the response so that keepalive will work
	io.Copy(ioutil.Discard, res.Body)
	return err
}

// For each of the objects in the provided list, use the cloud service to
// generate a signed URL to which the object should be uploaded.  The results
// are returned as a list of SignedURL objects which name the origin object
// as well as the URL to use.
func generateSignedURLs(rpcClient cloud_rpc.CloudStorageClient,
	uType *uploadType,
	objects []string) ([]*cloud_rpc.SignedURL, error) {

	if len(objects) == 0 {
		return []*cloud_rpc.SignedURL{}, nil
	}

	req := cloud_rpc.GenerateURLRequest{
		Objects:     objects,
		Prefix:      uType.prefix,
		ContentType: uType.ctype,
		HttpMethod:  "PUT",
	}

	ctx, err := applianceCred.MakeGRPCContext(context.Background())
	if err != nil {
		return []*cloud_rpc.SignedURL{}, errors.Wrapf(err, "Failed to make RPC context")
	}
	ctx, ctxcancel := context.WithDeadline(ctx, time.Now().Add(*deadlineFlag))
	defer ctxcancel()

	response, err := rpcClient.GenerateURL(ctx, &req)
	if err != nil {
		return []*cloud_rpc.SignedURL{}, errors.Wrapf(err,
			"Failed to generate signed URLs %v: %v", objects, err)
	}
	return response.Urls, nil
}

// Get a list of the filenames in a directory, up to a caller-specified limit
func getFilenames(dir string, max int) []string {
	names := make([]string, 0)

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Printf("Unable to get contents of %s: %v\n", dir, err)
	} else {
		for i, f := range files {
			if i == max {
				break
			}
			names = append(names, f.Name())
		}
	}
	return names
}

// Periodically upload the accumulated statistics and dropped packet data to the
// cloud, deleting the local copy.  The upload is done to a "signed URL", which
// encapsulates the target bucket and object name, and which serves as a
// capability allowing this client to create an object in the Brightgate cloud
// storage.
//
// The service account needs to have the "Storage Object Admin" role, or the
// object.create and object.delete ACLs.  We need "delete" to allow us to
// overwrite an existing file.  Without this, we might upload an object, either
// crash or lose the network before getting confirmation, and a subsequent
// (unnecessary) retry would fail.  Alternatively, we could attempt to read the
// cloud data to determine whether a retry was necessary.
func doUpload(rpcClient cloud_rpc.CloudStorageClient) {
	uploads := make([]oneUpload, 0)

	for _, t := range uploadTypes {
		dir := aputil.ExpandDirPath(t.dir)
		urls, err := generateSignedURLs(rpcClient, &t,
			getFilenames(dir, *uploadBatchSize))
		if err != nil {
			log.Printf("Couldn't generate signed URLs for upload: %v", err)
			continue
		}
		for _, url := range urls {
			u := oneUpload{
				source:    filepath.Join(dir, url.Object),
				signedURL: url,
				uType:     t,
			}
			uploads = append(uploads, u)
		}
	}

	client := &http.Client{
		Timeout: *uploadTimeout,
	}

	errs := 0
	for _, u := range uploads {
		if err := upload(client, u); err != nil {
			log.Printf("failed to upload %s: %s\n", u.source, err)
			errs++
		} else if err := os.Remove(u.source); err != nil {
			log.Printf("unable to remove %s: %v\n", u.source, err)
		}
		if errs >= *uploadErrMax {
			log.Printf("%d uploads failed.  Giving up.\n", errs)
			break
		}
	}
}

func uploadInit() error {
	var err error
	applianceCred, err = grpcutils.SystemCredential()
	if err != nil {
		log.Printf("Failed to build credential: %s", err)
		return err
	}
	if !*enableTLSFlag {
		log.Printf("Connecting insecurely due to '-enable-tls=false' flag (developers only!)")
	}

	if *connectFlag == "" {
		*connectFlag, err = config.GetProp("@/cloud/svc_rpc")
		if err != nil {
			return fmt.Errorf("need @/cloud/svc_rpc or -connect")
		}
	}

	rpcConn, err = grpcutils.NewClientConn(*connectFlag, *enableTLSFlag, pname)
	if err != nil {
		log.Printf("Failed to make RPC client: %+v", err)
		return err
	}
	storageClient = cloud_rpc.NewCloudStorageClient(rpcConn)
	return nil
}

func uploadLoop(wg *sync.WaitGroup, doneChan chan bool) {
	var done bool
	var lastError error

	initted := false
	ticker := time.NewTicker(*uploadPeriod)
	for !done {
		if !initted {
			err := uploadInit()
			if err == nil {
				initted = true
			} else if err != lastError {
				log.Printf("%v\n", err)
				lastError = err
			}
		}

		if initted {
			doUpload(storageClient)
		}

		select {
		case <-ticker.C:
		case done = <-doneChan:
		}
	}
	if rpcConn != nil {
		rpcConn.Close()
	}
	wg.Done()
}

func main() {
	var wg sync.WaitGroup

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()

	mcpd, err := mcp.New(pname)
	if err != nil {
		log.Printf("cannot connect to mcp\n")
	}

	brokerd := broker.New(pname)
	defer brokerd.Fini()

	config, err = apcfg.NewConfigd(brokerd, pname, cfgapi.AccessInternal)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}
	config.HandleChange(`^@/cloud/.*/bucket`, configBucketChanged)

	mcpd.SetState(mcp.ONLINE)

	stopUpdate := make(chan bool)
	stopUpload := make(chan bool)

	wg.Add(2)
	go updateLoop(&wg, stopUpdate)
	go uploadLoop(&wg, stopUpload)

	exitSig := make(chan os.Signal, 2)
	signal.Notify(exitSig, syscall.SIGINT, syscall.SIGTERM)

	s := <-exitSig
	log.Printf("Received signal '%v'.  Exiting.\n", s)

	stopUpdate <- true
	stopUpload <- true

	wg.Wait()
}
