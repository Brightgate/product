/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/common/cfgtree"
)

var (
	refreshLock     sync.Mutex
	refreshTracker  *aputil.PaceTracker
	refreshReported time.Time
)

// generate flattened versions of the live and exported copies of the config
// tree.  Do a node-by-node comparison of the values and hashes in each tree.
func flatCompare(exported *string) error {
	exportedTree, err := cfgtree.NewPTree("@/", []byte(*exported))
	if err != nil {
		slog.Fatalf("can't unpack tree: %v", err)
	}
	expFlat := exportedTree.Flatten()
	liveFlat := propTree.Flatten()

	errs := 0
	for prop, node := range expFlat {
		liveNode, ok := liveFlat[prop]
		if !ok {
			slog.Warnf("missing from live tree: %s", prop)
			errs++
			continue
		}
		delete(liveFlat, prop)

		if node.Value != liveNode.Value {
			slog.Warnf("mismatch: %s  exported: %q  live: %q",
				prop, node.Value, liveNode.Value)
			errs++

		} else if !bytes.Equal(node.Hash, liveNode.Hash) {
			buf := new(bytes.Buffer)
			binary.Write(buf, binary.LittleEndian, node.Hash)
			expHash := fmt.Sprintf("%x", buf.Bytes())

			buf = new(bytes.Buffer)
			binary.Write(buf, binary.LittleEndian, liveNode.Hash)
			liveHash := fmt.Sprintf("%x", buf.Bytes())

			slog.Warnf("hash mismatch: %s exported: %s live: %s",
				prop, expHash, liveHash)
			errs++
		}
	}
	for prop := range liveFlat {
		slog.Warnf("missing from exported tree: %s", prop)
		errs++
	}

	if errs != 0 {
		err = fmt.Errorf("tree comparison failed with %d errs", errs)
	}

	return err
}

// The occasional cloud refresh is OK, but frequent refreshes may be a signal
// that something has gone more deeply wrong.  If that happens, we do a full
// node-by-node comparison of our in-core tree and the one being pushed to the
// cloud.
func refreshEvent(exported *string) {
	refreshLock.Lock()
	defer refreshLock.Unlock()

	err := refreshTracker.Tick()
	slog.Infof("Full tree refresh requested")
	if err != nil && time.Since(refreshReported) > time.Hour {
		if !propTree.Root().Validate() {
			slog.Errorf("tree is not internally consistent")
		} else {
			slog.Debugf("tree is internally consistent")
		}
		if err = flatCompare(exported); err != nil {
			slog.Errorf("%s", err)
			refreshReported = time.Now()
		} else {
			slog.Debugf("exported and live trees are consistent")
		}
	}
}

// dump a flattened version of the tree to a .csv file
func dumpTree(t *cfgtree.PTree, file io.Writer) {
	kinds := map[bool]string{true: "leaf", false: "internal"}

	w := csv.NewWriter(file)

	flat := t.Flatten()
	for prop, node := range flat {
		buf := new(bytes.Buffer)
		binary.Write(buf, binary.LittleEndian, node.Hash)
		hash := fmt.Sprintf("%x", buf.Bytes())

		l := []string{prop, node.Value, kinds[node.Leaf], hash}
		w.Write(l)
	}
	w.Flush()
}

func init() {
	refreshTracker = aputil.NewPaceTracker(2, time.Hour)
}
