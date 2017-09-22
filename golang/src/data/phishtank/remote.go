/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package phishtank

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// RemoteSource is a CSV source that can be updated.
type RemoteSource struct {
	BasicCSVSource // mutex?
	url            string
	modtime        time.Time
}

const phishKey = "bd4ea8a80e25662e85f349c84bf300995ef013528c8201455edaeccf7426ec5e"

// Phishtank produces a new RemoteSource for parsing the Phishtank file at path.
//
// Metadata about this data source:
// http://data.phishtank.com/....
// Updated Hourly
// [Complete on one retrieval, or needs to be accumulated?]
// CSV

// https://pixabay.com/en/fish-market-seafood-fish-428058/
// https://pixabay.com/p-1191938/
func Phishtank(path string) RemoteSource {
	return RemoteSource{
		BasicCSVSource{
			BasicSource{
				name:          "phishtank",
				filepath:      path,
				defaultWeight: -5,
				weightMap:     map[Kind]int{},
			},
			map[Kind][]int{
				Dns:  []int{1}, // doesn't seem to work...
				Url:  []int{1},
				Ipv4: []int{1},
			},
		},
		fmt.Sprintf(
			"http://data.phishtank.com/data/%s/online-valid.csv", phishKey),
		time.Time{},
	}
}

// MDL produces a new RemoteSource for parsing a Malware Domain List file
// located at path.
//
// Metadata:
// http://www.malwaredomainlist.com/
// uses http://www.malwaredomainlist.com/mdlcsv.php?inactive=off
// updated relatively infrequently, but open to commercial use
func MDL(path string) RemoteSource {
	return RemoteSource{
		BasicCSVSource{
			BasicSource{
				name:          "mdl",
				filepath:      path,
				defaultWeight: -6,
				weightMap:     map[Kind]int{},
			},
			map[Kind][]int{
				Dns:  []int{1, 3},
				Url:  []int{1, 3},
				Ipv4: []int{1, 2, 3},
			},
		},
		"http://www.malwaredomainlist.com/mdlcsv.php?inactive=off",
		time.Time{},
	}
}

// Update updates the file at filepath if data at path either doesn't exist or
// is outdated. For now, isn't called anywhere.
func (ds *RemoteSource) Update() error {
	// check if needs updating/creating
	client := &http.Client{}
	req, err := http.NewRequest("GET", ds.url, nil)
	req.Header.Add("If-Modified-Since", ds.modtime.Format(http.TimeFormat))

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 304 {
		// hasn't been modified
		return nil
	} else if resp.StatusCode != 200 {
		return fmt.Errorf("update request returned with status code %d",
			resp.StatusCode)
	}

	// save to temp file
	dir, file := filepath.Split(ds.filepath)
	tmpPath := dir + "tmp." + file
	tmpFile, err := os.OpenFile(tmpPath,
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return fmt.Errorf("failed to create temp file %s: %v",
			tmpPath, err)
	}
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("failed to download to temp file %s: %v",
			tmpPath, err)
	}

	// rename temp to real file
	if err := os.Rename(tmpPath, ds.filepath); err != nil {
		return fmt.Errorf("failed while moving %s to %s: %v", tmpPath,
			ds.filepath, err)
	}

	ds.modtime = time.Now()
	return nil
}

// AutoUpdate updates data source with a delay determined by freq.
// For now, not used.
func (ds RemoteSource) AutoUpdate(freq time.Duration) {
	ticker := time.NewTicker(freq)
	go func() {
		for {
			if err := ds.Update(); err != nil {
				log.Printf("Failed to autoload file: %v", err)
			}
			<-ticker.C
		}
	}()
}
