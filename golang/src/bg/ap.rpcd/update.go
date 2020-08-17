/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/cloud_rpc"
	"bg/common/archive"
	"bg/common/urlfetch"

	"context"
)

var (
	// XXX: these two could benefit from being made dynamic
	updatePeriod = apcfg.Duration("update_period", 10*time.Minute,
		false, nil)
	uploadPeriod = apcfg.Duration("upload_period", 5*time.Minute,
		false, nil)
	uploadTimeout = apcfg.Duration("upload_timeout", 10*time.Second,
		true, nil)
	uploadErrMax    = apcfg.Int("upload_errmax", 5, true, nil)
	uploadBatchSize = apcfg.Int("bsize", 5, true, nil)

	updateBucket string
)

type updateInfo struct {
	localDir   string
	localName  string
	latestName string
}

var updates = map[string]updateInfo{
	"ip_blocklist": {
		localDir:   "__APDATA__/watchd/",
		localName:  "ip_blocklist.csv",
		latestName: "ip_blocklist.latest",
	},
	"dns_blocklist": {
		localDir:   "__APDATA__/antiphishing/",
		localName:  "dns_blocklist.csv",
		latestName: "dns_blocklist.latest",
	},
	"dns_allowlist": {
		localDir:   "__APDATA__/antiphishing/",
		localName:  "dns_allowlist.csv",
		latestName: "dns_allowlist.latest",
	},
	"pass_list": {
		localDir:   "__APDATA__/defaultpass/",
		localName:  "vendor_defaults.csv",
		latestName: "vendor_defaults.latest",
	},
	// "vulnerabilities": {
	// localDir:   "__APDATA__/watchd/",
	// localName:  "vuln-db.json",
	// latestName: "vuln-db.latest",
	// },
}

type uploadType struct {
	dir    string
	prefix string
	suffix string
	ctype  string
}

var uploadTypes = []uploadType{
	{
		"__APDATA__/watchd/droplog",
		"drops",
		".json",
		archive.DropContentType,
	},
	{
		"__APDATA__/watchd/stats",
		"stats",
		".json",
		archive.StatContentType,
	},
	{
		"__APDATA__/watchd/droplog",
		"drops",
		".gob",
		archive.DropBinaryType,
	},
	{
		"__APDATA__/watchd/stats",
		"stats",
		".gob",
		archive.StatBinaryType,
	},
}

type oneUpload struct {
	source    string
	signedURL *cloud_rpc.SignedURL
	uType     uploadType
}

func configBucketChanged(val string) {
	updateBucket = val
}

func refresh(u *updateInfo) (bool, error) {
	targetDir := plat.ExpandDirPath(u.localDir)
	target := plat.ExpandDirPath(targetDir, u.localName)
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
			err = fmt.Errorf("unable to read %s: %v", latestFile, rerr)
		} else {
			sourceName := strings.TrimSpace(string(b))
			url = updateBucket + "/" + sourceName
			_, err = urlfetch.FetchURL(url, target, "")
		}
		if err == nil {
			slog.Infof("Updated %s", target)
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
		slog.Warnf("Failed to update %s: %v", name, err)
	} else if refreshed {
		slog.Infof("%s refreshed", name)
		tstr := time.Now().Format(time.RFC3339)
		prop := "@/updates/" + name
		config.CreateProp(prop, tstr, nil)
	}
}

func updateLoop(wg *sync.WaitGroup, doneChan chan bool) {
	var done bool

	updateBucket, _ = config.GetProp("@/cloud/update/bucket")
	if updateBucket == "" {
		slog.Warnf("no update bucket defined")
	}

	slog.Infof("update loop starting")
	refreshSig := make(chan os.Signal, 1)
	signal.Notify(refreshSig, syscall.SIGHUP)

	ticker := time.NewTicker(*updatePeriod)
	defer ticker.Stop()
	for !done {
		if updateBucket != "" {
			for name := range updates {
				refreshOne(name)
			}
		}

		select {
		case <-refreshSig:
			slog.Infof("Received SIGHUP.  Refreshing updates.")
		case <-ticker.C:
		case done = <-doneChan:
		}
	}
	slog.Infof("update loop done")
	wg.Done()
}

// PUT one file to the provided URL
func upload(client *http.Client, u oneUpload) error {
	data, err := os.Open(plat.ExpandDirPath(u.source))
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
	ctx, ctxcancel := context.WithDeadline(ctx, time.Now().Add(*rpcDeadline))
	defer ctxcancel()

	response, err := rpcClient.GenerateURL(ctx, &req)
	rpcHealthUpdate(err == nil)
	if err != nil {
		return []*cloud_rpc.SignedURL{}, errors.Wrapf(err,
			"Failed to generate signed URLs %v: %v", objects, err)
	}
	return response.Urls, nil
}

// Get a list of the filenames in a directory, up to a caller-specified limit
func getFilenames(dir, suffix string, max int) []string {
	names := make([]string, 0)

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		slog.Warnf("Unable to get contents of %s: %v", dir, err)
	} else {
		for i, f := range files {
			if i == max {
				break
			}
			if strings.HasSuffix(f.Name(), suffix) {
				names = append(names, f.Name())
			}
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
		dir := plat.ExpandDirPath(t.dir)
		urls, err := generateSignedURLs(rpcClient, &t,
			getFilenames(dir, t.suffix, *uploadBatchSize))
		if err != nil {
			slog.Warnf("Couldn't generate signed URLs for upload: %v", err)
			continue
		}
		for _, url := range urls {
			u := oneUpload{
				source:    filepath.Join(dir, url.Object),
				signedURL: url,
				uType:     t,
			}
			slog.Debugf("uploading %s as %s", u.source, t.ctype)
			uploads = append(uploads, u)
		}
	}

	client := &http.Client{
		Timeout: *uploadTimeout,
	}

	errs := 0
	for _, u := range uploads {
		if err := upload(client, u); err != nil {
			slog.Warnf("failed to upload %s: %s", u.source, err)
			errs++
		} else if err := os.Remove(u.source); err != nil {
			slog.Warnf("unable to remove %s: %v", u.source, err)
		}
		if errs >= *uploadErrMax {
			slog.Warnf("%d uploads failed.  Giving up.", errs)
			break
		}
	}
}

func uploadLoop(client cloud_rpc.CloudStorageClient, wg *sync.WaitGroup,
	doneChan chan bool) {

	var done bool

	slog.Infof("upload loop starting")
	ticker := time.NewTicker(*uploadPeriod)
	defer ticker.Stop()
	for !done {
		doUpload(client)

		select {
		case <-ticker.C:
		case done = <-doneChan:
		}
	}
	slog.Infof("upload loop done")
	wg.Done()
}

