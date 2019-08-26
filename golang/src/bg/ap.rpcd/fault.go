/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"context"
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"bg/cloud_rpc"
	"bg/common/faults"

	"github.com/pkg/errors"
)

const (
	uploadedRetain = 72 * time.Hour // keep faults for 3 days
	maxRetain      = 100            // max number of faults to retain
)

var (
	forceFaults = flag.Bool("force-faults", false, "always send all faults")
)

func earlier(i, j string) bool {
	_, _, t1, _ := faults.ParseFileName(i)
	_, _, t2, _ := faults.ParseFileName(j)

	return t1.Before(t2)
}

func sendFaults(ctx context.Context, client cloud_rpc.EventClient) error {
	var err error

	faultDir := plat.ExpandDirPath("__APDATA__", "faults")

	files, err := ioutil.ReadDir(faultDir)
	if err != nil {
		return errors.Wrapf(err, "Could not read dir %s", faultDir)
	}

	// Build a chronological list of all the local fault reports
	faultList := make([]string, 0)
	for _, file := range files {
		n := file.Name()

		if _, _, _, err := faults.ParseFileName(n); err == nil {
			faultList = append(faultList, n)
		}
	}
	sort.Slice(faultList, func(i, j int) bool {
		return earlier(faultList[i], faultList[j])
	})

	// Build a list of all fault reports that still need to be uploaded
	// and/or deleted.
	reapList := make([]string, 0)
	if excessFaults := len(faultList) - maxRetain; excessFaults > 0 {
		reapList = append(reapList, faultList[:excessFaults]...)
	}

	uploadList := make([]string, 0)
	endRetention := time.Now().Add(-1 * uploadedRetain)
	for _, file := range faultList {
		_, state, when, _ := faults.ParseFileName(file)
		if strings.Contains(state, "uploaded") {
			if when.Before(endRetention) {
				reapList = append(reapList, file)
			}

			if !*forceFaults {
				continue
			}
		}

		if client != nil {
			uploadList = append(uploadList, file)
		}
	}

	for _, name := range uploadList {
		var report []byte
		path := filepath.Join(faultDir, name)
		if report, err = ioutil.ReadFile(path); err != nil {
			slog.Warnf("failed to read fault report %s: %s",
				path, err)

		} else if err = publishEventSerialized(ctx, client,
			"cloud_rpc.FaultReport", "faults", report); err != nil {
			slog.Warnf("failed to upload %s: %v", name, err)

		} else {
			// Mark the report as uploaded
			newPath := strings.TrimSuffix(path, "json")
			newPath += "uploaded.json"
			os.Rename(path, newPath)
		}
	}

	for _, name := range reapList {
		path := filepath.Join(faultDir, name)
		os.Remove(path)
	}

	return nil
}

func faultLoop(ctx context.Context, client cloud_rpc.EventClient, wg *sync.WaitGroup, doneChan chan bool) {
	var done bool

	slog.Infof("faults loop starting")
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for !done {
		err := sendFaults(ctx, client)
		if err != nil {
			slog.Errorf("Failed faults: %s", err)
		}
		select {
		case done = <-doneChan:
		case <-ticker.C:
		}
	}
	slog.Infof("faults loop done")
	wg.Done()
}
