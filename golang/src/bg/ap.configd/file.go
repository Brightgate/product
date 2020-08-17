/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// Default properties are stored in __APPACKAGE__/etc.
// Active and backup properties are stored in __APDATA__/configd.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/base_def"
	"bg/common"
	"bg/common/bgioutil"
	"bg/common/cfgapi"
	"bg/common/cfgtree"

	"github.com/satori/uuid"
)

const (
	staticDir   = "__APPACKAGE__/etc"
	propertyDir = "__APDATA__/configd"

	propertyFilename = "ap_props.json"
	backupFilename   = "ap_props.json.bak"
	baseFilename     = "configd.json"
	// snapshot format is <year><month><day><hour><minute>
	snapFormat       = "200601021504"
	minConfigVersion = 32
)

var (
	propTreeDir          *os.File
	propTreeLoaded       bool
	propTreeStoreTrigger = make(chan bool, 32)

	archiveTimes []time.Time

	upgradeHooks = make([]func() error, cfgapi.Version+1)
)

func propFileRename(old, new string) bool {
	var renamed bool

	oldPath := plat.ExpandDirPath(propertyDir, old)
	if aputil.FileExists(oldPath) {
		newPath := plat.ExpandDirPath(propertyDir, new)
		if err := os.Rename(oldPath, newPath); err != nil {
			slog.Warnf("rename(%s, %s) failed: %v",
				oldPath, newPath, err)
		} else {
			renamed = true
		}
	}

	return renamed
}

func updateSnapshots() bool {
	var syncNeeded bool
	var last time.Time
	var lastDay, lastHour string

	if l := len(archiveTimes); l > 0 {
		last = archiveTimes[l-1]
	}

	// If the last snapshot is more than 5 minutes old, take a new one
	now := time.Now()
	if last.Add(5 * time.Minute).Before(now) {
		newFile := propertyFilename + "." + now.Format(snapFormat)
		slog.Debugf("Creating new snapshot: %s", newFile)
		if propFileRename(backupFilename, newFile) {
			archiveTimes = append(archiveTimes, now)
			syncNeeded = true
		}
	}

	// Clean up old snapshots
	del := make([]time.Time, 0)
	keep := make([]time.Time, 0)
	keptDays := 0
	for _, t := range archiveTimes {
		var delete bool

		// If the timestamp is more than 24 hours old, we only save one
		// per day - up to a total of 30 days.
		if t.Add(24 * time.Hour).Before(now) {
			dayStr := t.Format("20060102")
			if dayStr == lastDay || keptDays >= 30 {
				delete = true
			} else {
				keptDays++
			}
			lastDay = dayStr
		}

		// If the timestamp is more than an hour old, we only save one
		// per hour
		if t.Add(time.Hour).Before(now) {
			hourStr := t.Format("2006010215")
			if hourStr == lastHour {
				delete = true
			}
			lastHour = hourStr
		}

		if delete {
			del = append(del, t)
		} else {
			keep = append(keep, t)
		}
	}
	archiveTimes = keep

	for _, t := range del {
		name := propertyFilename + "." + t.Format(snapFormat)
		path := plat.ExpandDirPath(propertyDir, name)
		slog.Debugf("Removing old snapshot: %s", path)
		if err := os.Remove(path); err != nil {
			slog.Warnf("Error removing %s: %v", path, err)
		} else {
			syncNeeded = true
		}
	}

	return syncNeeded
}

func propTreeWriter(exitSignal chan bool, wg *sync.WaitGroup) {
	var lastWrite time.Time
	var reported bool

	name := plat.ExpandDirPath(propertyDir, propertyFilename)
	t := time.NewTicker(time.Second)

	propTree.Lock()
	oldHash := propTree.Root().Hash()
	propTree.Unlock()
	done := false
	for !done {
		var data []byte
		var force bool

		select {
		case done = <-exitSignal:
		case <-t.C:
		case force = <-propTreeStoreTrigger:
			slog.Infof("sync forced")
		}

		if !propTreeLoaded {
			continue
		}

		if !force && time.Since(lastWrite) < *storeFreq {
			continue
		}

		propTree.Lock()
		curHash := propTree.Root().Hash()
		if !bytes.Equal(curHash, oldHash) {
			// Don't bothed marshaling and writing the tree if the
			// contents haven't changed since the last write.
			data = propTree.Export(true)
			oldHash = curHash
		}
		propTree.Unlock()

		if len(data) == 0 {
			continue
		}

		slog.Debugf("syncing tree to disk")
		lastWrite = time.Now()
		metrics.treeSize.Set(float64(len(data)))

		syncNeeded := updateSnapshots()
		if propFileRename(propertyFilename, backupFilename) {
			syncNeeded = true
		}

		if syncNeeded {
			// Force directory metadata out to disk.
			if err := propTreeDir.Sync(); err != nil {
				slog.Warnf("Failed to sync properties dir: %v", err)
			}
		}

		if err := bgioutil.WriteFileSync(name, data, 0644); err != nil {
			slog.Warnf("Failed to write properties file: %v", err)
			if !reported {
				reported = true
				aputil.ReportError("failed to write %s: %v",
					name, err)
			}
		} else {
			reported = false
		}

		// Force directory metadata out to disk.
		if err := propTreeDir.Sync(); err != nil {
			slog.Warnf("Failed to sync properties dir: %v", err)
		}
	}
	wg.Done()
}

// Try to load a config tree from the given file.
func propTreeLoad(fullPath string) (*cfgtree.PTree, error) {
	slog.Debugf("Loading %s", fullPath)

	if !aputil.FileExists(fullPath) {
		return nil, fmt.Errorf("file missing")
	}

	file, err := ioutil.ReadFile(fullPath)
	if err != nil {
		slog.Warnf("Failed to load %s: %v", fullPath, err)
		return nil, err
	}

	tree, err := cfgtree.NewPTree("@/", file)
	if err == nil {
		metrics.treeSize.Set(float64(len(file)))
	} else {
		err = fmt.Errorf("importing %s: %v", fullPath, err)
	}

	return tree, err
}

func addUpgradeHook(version int32, hook func() error) {
	if version > cfgapi.Version {
		msg := fmt.Sprintf("Upgrade hook %d > current max of %d\n",
			version, cfgapi.Version)
		panic(msg)
	}

	upgradeHooks[version] = hook
}

func versionTree() error {
	upgraded := false

	node, _ := propTree.GetNode("@/cfgversion")
	if node == nil {
		return fmt.Errorf("properties file missing @/cfgversion")
	}

	version, err := strconv.Atoi(node.Value)
	if err != nil {
		return fmt.Errorf("illegal version '%s': %v", node.Value, err)
	}
	if version < minConfigVersion {
		return fmt.Errorf("obsolete properties file")
	}
	if version > int(cfgapi.Version) {
		return fmt.Errorf("properties file is newer than the software")
	}

	propTree.ChangesetInit()
	for version < int(cfgapi.Version) {
		slog.Infof("Upgrading properties from version %d to %d",
			version, version+1)
		version++
		if upgradeHooks[version] != nil {
			if err := upgradeHooks[version](); err != nil {
				propTree.ChangesetRevert()
				return fmt.Errorf("upgrade failed: %v", err)
			}
		}
		propTree.Set("@/cfgversion", strconv.Itoa(version), nil)
		upgraded = true
	}
	propTree.ChangesetCommit()

	if upgraded {
		propTreeStoreTrigger <- true
	}
	return nil
}

// Find all of the property files on the system: current, backup, and snapshots.
// Return them in reverse chronological order.
func getPropFiles() []string {
	goodRE := regexp.MustCompile(propertyFilename + `\.(\d{12})`)

	rval := []string{propertyFilename, backupFilename}

	dir := plat.ExpandDirPath(propertyDir)
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		slog.Warnf("reading directory %s: %v", dir, err)
	}

	// ReadDir() returns files in alphabetical order, so we need to process
	// them backwards.
	for i := len(files); i > 0; i-- {
		n := files[i-1].Name()
		if m := goodRE.FindStringSubmatch(n); len(m) > 1 {

			// Turn the time tag on the file back into a timestamp
			if t, err := time.Parse(snapFormat, m[1]); err == nil {
				// Remember both the file name and the
				// timestamp
				rval = append(rval, n)
				archiveTimes = append(archiveTimes, t)
			}
		}
	}

	return rval
}

func propTreeInit(defaults *cfgtree.PNode) error {
	var err error
	var newTree bool
	var tree *cfgtree.PTree

	// Open the properties file's enclosing directory; we'll fsync its
	// metadata after each write.
	propTreeDir, err = os.Open(plat.ExpandDirPath(propertyDir))
	if err != nil {
		slog.Warnf("Unable to open properties dir: %v", err)
	}

	propFiles := getPropFiles()
	for _, name := range propFiles {
		fullPath := plat.ExpandDirPath(propertyDir, name)
		tree, err = propTreeLoad(fullPath)
		if err == nil {
			if name != propertyFilename {
				slog.Infof("Loaded properties from backup: %s",
					fullPath)
			}
			break
		}
		slog.Warnf("Unable to load %s: %v", fullPath, err)
	}

	if tree == nil {
		slog.Infof("No usable properties files.  Using defaults.")
		tree = cfgtree.GraftTree("@", defaults)
		newTree = true
	}

	propTree = tree
	propTreeLoaded = true

	if newTree {
		propTree.ChangesetInit()
		applianceUUID := uuid.NewV4().String()
		if err := propTree.Add("@/uuid", applianceUUID, nil); err != nil {
			slog.Fatalf("Unable to set UUID: %v", err)
		}

		applianceSiteID := "setup." + base_def.GATEWAY_CLIENT_DOMAIN
		if err := propTree.Add("@/siteid", applianceSiteID, nil); err != nil {
			slog.Fatalf("Unable to set SiteID: %v", err)
		}
		propTree.ChangesetCommit()
		propTreeStoreTrigger <- true
	}

	if err = versionTree(); err != nil {
		err = fmt.Errorf("failed version check: %v", err)
	}

	if err == nil {
		propTree.ChangesetInit()
		propTree.Add("@/apversion", common.GitVersion, nil)
		propTree.ChangesetCommit()
	}
	if *verbose {
		tree.Dump(os.Stdout)
	}
	return err
}

func loadDefaults() (defaults *cfgtree.PNode, descs []propDescription, err error) {
	var base struct {
		Defaults     cfgtree.PNode
		Descriptions []propDescription
	}

	if !aputil.FileExists(plat.ExpandDirPath(staticDir)) {
		cwd, _ := os.Getwd()

		err = fmt.Errorf("missing properties directory: %s (%s)", plat.ExpandDirPath(staticDir), cwd)
		return
	}

	baseFile := plat.ExpandDirPath(staticDir, baseFilename)
	if !aputil.FileExists(baseFile) {
		err = fmt.Errorf("missing defaults file: %s", baseFile)
		return
	}

	data, rerr := ioutil.ReadFile(baseFile)
	if rerr != nil {
		err = fmt.Errorf("failed to read %s: %v", baseFile, rerr)
		return
	}

	if rerr := json.Unmarshal(data, &base); rerr != nil {
		err = fmt.Errorf("failed to parse %s: %v", baseFile, rerr)
		return
	}

	defaults = &base.Defaults
	descs = base.Descriptions
	return
}

