/*
 * Copyright 2018 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package urlfetch

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"bg/common/bgioutil"

	"github.com/pkg/errors"
)

type downloadMeta struct {
	Time     time.Time
	Modified string
	Etag     string
	Size     int64
}

func getDownloadMeta(name string) (*downloadMeta, error) {
	var meta downloadMeta

	if _, err := os.Stat(name); os.IsNotExist(err) {
		return nil, nil
	}

	file, err := ioutil.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("failed to load %s: %v", name, err)
	}

	if err = json.Unmarshal(file, &meta); err != nil {
		return nil, fmt.Errorf("failed to load %s: %v", name, err)
	}

	return &meta, nil
}

func putDownloadMeta(name string, meta *downloadMeta) error {
	s, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("Failed to marshal record %v: %v", meta, err)
	}

	err = bgioutil.WriteFileSync(name, s, 0644)
	if err != nil {
		err = fmt.Errorf("Failed to write meta file %s: %v", name, err)
	}

	return err
}

// FetchURL downloads a file from 'url' and store it locally in 'target'.  We
// use the 'meta' file to cache ETag and/or Last-Modified headers, allowing us
// to avoid re-fetching unchanged data on subsequent calls.
//
func FetchURL(url, target, meta string) (bool, error) {
	var (
		old     *downloadMeta
		req     *http.Request
		resp    *http.Response
		outFile *os.File
		action  string
		bytes   int64
		err     error
	)

	if req, err = http.NewRequest("GET", url, nil); err != nil {
		return false, errors.Wrap(err, "unable to download "+url)
	}

	if meta != "" {
		if old, err = getDownloadMeta(meta); err != nil {
			log.Printf("Failed to get metadata for %s: %v\n",
				target, err)
		}

		if old != nil {
			if old.Etag != "" {
				req.Header.Add("If-None-Match", old.Etag)
			}
			if old.Modified != "" {
				req.Header.Add("If-Modified-Since", old.Modified)
			}
		}
	}

	client := &http.Client{}
	if resp, err = client.Do(req); err != nil {
		return false, errors.Wrap(err, "unable to connect to "+url)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 304 && old != nil {
		return false, nil
	} else if resp.StatusCode != 200 {
		return false, fmt.Errorf("unable to fetch %s: %s", url,
			resp.Status)
	}

	tmpFile := target + ".tmp"
	if outFile, err = os.Create(tmpFile); err != nil {
		return false, errors.Wrap(err, "failed to create "+tmpFile)
	}
	defer outFile.Close()

	if bytes, err = io.Copy(outFile, resp.Body); err != nil {
		os.Remove(tmpFile)
		return false, errors.Wrap(err, "failed to download "+url)
	}
	outFile.Sync()
	os.Rename(tmpFile, target)

	now := time.Now()
	if meta != "" {
		// XXX: emergingthreats adds this suffix to some tags, but
		// doesn't want to see it in the subsequent request.
		etag := resp.Header.Get("Etag")
		etag = strings.Replace(etag, "-gzip", "", 1)

		new := downloadMeta{
			Time:     now,
			Etag:     etag,
			Modified: resp.Header.Get("Last-Modified"),
			Size:     bytes,
		}
		putDownloadMeta(meta, &new)
	}

	if old == nil {
		action = "downloaded"
	} else {
		action = "refreshed"
	}
	log.Printf("%s: %s at %s\n", target, action, now.Format(time.RFC3339))

	return true, nil
}

