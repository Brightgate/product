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

// Compare a node from the in-core tree with the same node from the file.  They
// should have the same hash value, the same child properties, and all of their
// children should have the same hashes.
func treeCompare(core, file *cfgtree.PNode) {
	if bytes.Compare(core.Hash(), file.Hash()) != 0 {
		cv := "internal"
		fv := "internal"

		if len(core.Children) == 0 {
			cv = core.Value
		}
		if len(file.Children) == 0 {
			fv = file.Value
		}
		slog.Warnf("hash mismatch at %s.  core: %s  file: %s",
			core.Path(), cv, fv)
	}

	fc := make(map[string]*cfgtree.PNode)
	for p, fileNode := range file.Children {
		fc[p] = fileNode
	}
	for p, coreNode := range core.Children {
		if fileNode := fc[p]; fileNode != nil {
			treeCompare(coreNode, fileNode)
			delete(fc, p)
		} else {
			slog.Warnf("file is missing %s", coreNode.Path())
		}
	}
	for _, fileNode := range fc {
		slog.Warnf("core is missing %s", fileNode.Path())
	}
}

// compare the root hash values of the in-core tree with one reconsituted from
// the on-disk tree.  If the two hashes match, we assume the rest of the tree is
// fine.  If they don't, we do a detailed comparison.
func hashCompare() bool {
	miscompare := false

	fullPath := plat.ExpandDirPath(propertyDir, propertyFilename)
	fileTree, err := propTreeLoad(fullPath)
	if err != nil {
		slog.Warnf("unable to reload %s: %v", fullPath, err)
	} else {
		coreHash := propTree.Root().Hash()
		fileHash := fileTree.Root().Hash()
		if bytes.Compare(coreHash, fileHash) != 0 {
			treeCompare(propTree.Root(), fileTree.Root())
			miscompare = true
		}
	}
	return miscompare
}

// The occasional cloud refresh is OK, but frequent refreshes may be a signal
// that something has gone more deeply wrong.  If that happens, we do a full
// node-by-node comparison of our in-core tree and one reconstituted from the
// file.
func refreshEvent() {
	refreshLock.Lock()
	defer refreshLock.Unlock()

	err := refreshTracker.Tick()
	slog.Debugf("Refresh requested")
	if err != nil && time.Since(refreshReported) > time.Hour {
		if !propTree.Root().Validate() {
			slog.Errorf("tree is not internally consistent")
		} else {
			slog.Debugf("tree is internally consistent")
		}
		if hashCompare() {
			slog.Errorf("tree is not consistent with on-disk copy")
			refreshReported = time.Now()
		} else {
			slog.Debugf("tree is consistent with the on-disk copy")
		}
	}
}

func init() {
	refreshTracker = aputil.NewPaceTracker(2, time.Hour)
}
