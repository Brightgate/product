/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Default properties are stored in __APPACKAGE__/etc.
// Active and backup properties are stored in __APDATA__/configd.

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"

	"bg/ap_common/aputil"
	"bg/ap_common/platform"
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
	minConfigVersion = 12
)

var (
	propTreeDir    *os.File
	propTreeFile   string
	propTreeLoaded bool

	plat *platform.Platform

	upgradeHooks []func() error
)

func propTreeStore() error {
	var err error
	if !propTreeLoaded {
		return nil
	}

	s := propTree.Export(true)
	metrics.treeSize.Set(float64(len(s)))

	if aputil.FileExists(propTreeFile) {
		/*
		 * XXX: could store multiple generations of backup files,
		 * allowing for arbitrary rollback.  Could also take explicit
		 * 'checkpoint' snapshots.
		 */
		backupfile := plat.ExpandDirPath(propertyDir, backupFilename)
		err = os.Rename(propTreeFile, backupfile)
		if err != nil {
			slog.Warnf("Failed to rename current property file to backup: %v", err)
		}
		// Force directory metadata out to disk.
		err = propTreeDir.Sync()
		if err != nil {
			slog.Warnf("Failed to sync properties dir during backup: %v", err)
		}
	}

	err = bgioutil.WriteFileSync(propTreeFile, s, 0644)
	if err != nil {
		slog.Warnf("Failed to write properties file: %v", err)
	}
	// Force directory metadata out to disk.
	err = propTreeDir.Sync()
	if err != nil {
		slog.Warnf("Failed to sync properties dir: %v", err)
	}
	return err
}

func propTreeLoad(name string) (*cfgtree.PTree, error) {
	if !aputil.FileExists(propTreeFile) {
		return nil, fmt.Errorf("file missing")
	}

	file, err := ioutil.ReadFile(name)
	if err != nil {
		slog.Warnf("Failed to load %s: %v", name, err)
		return nil, err
	}

	tree, err := cfgtree.NewPTree("@/", file)
	if err == nil {
		metrics.treeSize.Set(float64(len(file)))
	} else {
		err = fmt.Errorf("importing %s: %v", name, err)
	}

	return tree, err
}

func addUpgradeHook(version int32, hook func() error) {
	if version > cfgapi.Version {
		msg := fmt.Sprintf("Upgrade hook %d > current max of %d\n",
			version, cfgapi.Version)
		panic(msg)
	}

	if upgradeHooks == nil {
		upgradeHooks = make([]func() error, cfgapi.Version+1)
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
		if err := propTreeStore(); err != nil {
			return fmt.Errorf("Failed to write properties: %v", err)
		}
	}
	return nil
}

func propTreeInit(defaults *cfgtree.PNode) error {
	var err error
	var newTree bool

	// Open the properties file's enclosing directory; we'll fsync its
	// metadata after each write.
	propTreeDir, err = os.Open(plat.ExpandDirPath(propertyDir))
	if err != nil {
		slog.Warnf("Unable to open properties dir: %v", err)
	}

	propTreeFile = plat.ExpandDirPath(propertyDir, propertyFilename)
	tree, err := propTreeLoad(propTreeFile)

	if err != nil {
		slog.Warnf("Unable to load properties: %v", err)
		backupfile := plat.ExpandDirPath(propertyDir, backupFilename)
		tree, err = propTreeLoad(backupfile)
		if err != nil {
			slog.Warnf("Unable to load backup properties: %v", err)
		} else {
			slog.Infof("Loaded properties from backup file")
		}
	}

	if err != nil {
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
		if err := propTreeStore(); err != nil {
			slog.Fatalf("Failed to write properties: %v", err)
		}
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
		tree.Dump()
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

func init() {
	plat = platform.NewPlatform()
}
