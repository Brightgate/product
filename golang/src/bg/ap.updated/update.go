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
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/common"

	"cloud.google.com/go/storage"
	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
)

const (
	pname = "ap.updated"
)

var (
	brokerd *broker.Broker
	config  *apcfg.APConfig

	updatePeriod = flag.Duration("update", 10*time.Minute,
		"frequency with which to check updates")
	uploadPeriod = flag.Duration("upload", 5*time.Minute,
		"frequency with which to upload refreshed data")

	uploadTimeout = flag.Duration("timeout", 10*time.Second,
		"time allowed for a single file upload")
	uploadErrMax = flag.Int("emax", 5,
		"upload errors allowed before we give up for now")

	updateBucket string

	uploadConfig struct {
		bucket     string
		serviceID  string
		credFile   string
		privateKey []byte
	}

	errNoBucket    = fmt.Errorf("no upload bucket configured")
	errNoServiceID = fmt.Errorf("no upload serviceID configured")
	errNoCreds     = fmt.Errorf("no cloud credentials defined")
	errBadCreds    = fmt.Errorf("ill-defined cloud credentials")
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

type uploadInfo struct {
	dir    string
	prefix string
}

var uploads = []uploadInfo{
	{
		dir:    "/var/spool/watchd/droplog",
		prefix: "drops",
	},
	{
		dir:    "/var/spool/watchd/stats",
		prefix: "stats",
	},
}

func configBucketChanged(path []string, val string, expires *time.Time) {
	if len(path) == 3 {
		switch path[1] {
		case "update":
			updateBucket = val
			log.Printf("Changed update bucket to %s\n", val)
		case "upload":
			uploadConfig.bucket = val
			log.Printf("Changed upload bucket to %s\n", val)
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
	metaRefreshed, err := common.FetchURL(url, latestFile, metaFile)
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
			_, err = common.FetchURL(url, target, "")
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

	refreshSig := make(chan os.Signal)
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
func upload(client *http.Client, obj, url string) error {
	data, err := os.Open(obj)
	if err != nil {
		return fmt.Errorf("failed to open %s: %v", obj, err)
	}
	defer data.Close()

	req, err := http.NewRequest("PUT", url, data)
	if err != nil {
		return fmt.Errorf("failed to create PUT request: %v", err)
	}

	res, err := client.Do(req)
	if err != nil {
		err = fmt.Errorf("PUT failed: %s", res.Status)
	} else {
		if res.StatusCode != http.StatusOK {
			buf := make([]byte, 256)
			if res != nil {
				res.Body.Read(buf)
			}
			err = fmt.Errorf("PUT failed: %s (%s)", res.Status,
				string(buf))
		}
		res.Body.Close()
	}

	return err
}

// Extract this service account's private key from the default credentials
// structure
func getPrivateKey() ([]byte, error) {
	var key []byte
	var err error

	ctx := context.Background()
	creds, _ := google.FindDefaultCredentials(ctx, storage.ScopeFullControl)
	if creds == nil {
		err = fmt.Errorf("no cloud credentials defined")
	} else {
		jwt, rerr := google.JWTConfigFromJSON(creds.JSON)
		if rerr != nil {
			err = fmt.Errorf("bad cloud credentials: %v", err)
		} else {
			key = jwt.PrivateKey
		}
	}
	return key, err
}

// For each of the objects in the provided list, generate a signed URL to which
// the object should be uploaded.  The results are returned in an object->URL
// map.
//
// XXX: Eventually, the signed URLs will be generated in the cloud, so we will
// not have to deploy the private key and google storage values to the
// appliance.
func generateSignedURLs(prefix string, objects []string) map[string]string {
	options := &storage.SignedURLOptions{
		GoogleAccessID: uploadConfig.serviceID,
		PrivateKey:     uploadConfig.privateKey,
		Method:         "PUT",
		Expires:        time.Now().Add(10 * time.Minute),
	}

	urls := make(map[string]string)
	for _, name := range objects {
		full := prefix + "/" + name
		url, err := storage.SignedURL(uploadConfig.bucket, full, options)
		if err != nil {
			log.Printf("Failed to create signed URL for %s: %v\n",
				full, err)
		} else {
			urls[name] = url
		}
	}
	return urls
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
func doUpload() {
	pairs := make(map[string]string)

	for _, t := range uploads {
		dir := aputil.ExpandDirPath(t.dir)
		urls := generateSignedURLs(t.prefix, getFilenames(dir, 5))
		for file, url := range urls {
			full := dir + "/" + file
			pairs[full] = url
		}
	}

	client := &http.Client{
		Timeout: *uploadTimeout,
	}

	errs := 0
	for file, url := range pairs {
		if err := upload(client, file, url); err != nil {
			log.Printf("failed to upload %s: %s\n", file, err)
			errs++
		} else if err := os.Remove(file); err != nil {
			log.Printf("unable to remove %s: %v\n", file, err)
		}
		if errs >= *uploadErrMax {
			log.Printf("%d uploads failed.  Giving up.\n", errs)
			break
		}
	}
}

func uploadInit() error {

	if b, _ := config.GetProp("@/cloud/upload/bucket"); b != "" {
		uploadConfig.bucket = b
	} else {
		return errNoBucket
	}

	if s, _ := config.GetProp("@/cloud/upload/serviceid"); s != "" {
		uploadConfig.serviceID = s
	} else {
		return errNoServiceID
	}

	if c, _ := config.GetProp("@/cloud/upload/creds"); c != "" {
		fullPath := aputil.ExpandDirPath(c)
		os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", fullPath)
	}

	p, err := getPrivateKey()
	if err == nil {
		uploadConfig.privateKey = p
	}
	return err
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
			doUpload()
		}

		select {
		case <-ticker.C:
		case done = <-doneChan:
		}
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

	if config, err = apcfg.NewConfig(brokerd, pname); err != nil {
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
