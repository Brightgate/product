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
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"bg/ap_common/aputil"
	"bg/base_def"
	"bg/common"
	"bg/common/cfgapi"
	"bg/common/cfgtree"

	"github.com/satori/uuid"
)

const (
	propertyFilename = "ap_props.json"
	backupFilename   = "ap_props.json.bak"
	baseFilename     = "configd.json"
	minConfigVersion = 12
)

var (
	propdir = flag.String("propdir", "/etc",
		"directory in which the property files should be stored")
	propTreeFile   string
	propTreeLoaded bool

	upgradeHooks []func() error
)

func propTreeStore() error {
	if !propTreeLoaded {
		return nil
	}

	propTree.Add("@/apversion", common.GitVersion, nil)
	s := propTree.Export(true)
	metrics.treeSize.Set(float64(len(s)))

	if aputil.FileExists(propTreeFile) {
		/*
		 * XXX: could store multiple generations of backup files,
		 * allowing for arbitrary rollback.  Could also take explicit
		 * 'checkpoint' snapshots.
		 */
		backupfile := *propdir + backupFilename
		os.Rename(propTreeFile, backupfile)
	}

	err := ioutil.WriteFile(propTreeFile, s, 0644)
	if err != nil {
		slog.Warnf("Failed to write properties file: %v", err)
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

	tree, err := cfgtree.NewPTree("@", file)
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

func dumpTree(indent string, node *cfgtree.PNode) {
	e := ""
	if node.Expires != nil {
		e = node.Expires.Format("2006-01-02T15:04:05")
	}
	fmt.Printf("%s%s: %s  %s\n", indent, node.Name(), node.Value, e)
	for _, child := range node.Children {
		dumpTree(indent+"  ", child)
	}
}

func propTreeInit(defaults *cfgtree.PNode) error {
	var err error

	propTreeFile = *propdir + propertyFilename
	tree, err := propTreeLoad(propTreeFile)

	if err != nil {
		slog.Warnf("Unable to load properties: %v", err)
		backupfile := *propdir + backupFilename
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
		applianceUUID := uuid.NewV4().String()
		if err := tree.Add("@/uuid", applianceUUID, nil); err != nil {
			slog.Fatalf("Unable to set UUID: %v", err)
		}

		applianceSiteID := "setup." + base_def.GATEWAY_CLIENT_DOMAIN
		if err := tree.Add("@/siteid", applianceSiteID, nil); err != nil {
			slog.Fatalf("Unable to set SiteID: %v", err)
		}
	}

	propTree = tree
	propTreeLoaded = true
	if err = versionTree(); err != nil {
		err = fmt.Errorf("failed version check: %v", err)
	}

	if *verbose {
		root, _ := tree.GetNode("@/")
		dumpTree("", root)
	}
	return err
}

func loadDefaults() (defaults *cfgtree.PNode, descs []propDescription, err error) {
	var base struct {
		Defaults     cfgtree.PNode
		Descriptions []propDescription
	}

	if !strings.HasSuffix(*propdir, "/") {
		*propdir = *propdir + "/"
	}
	*propdir = aputil.ExpandDirPath(*propdir)
	if !aputil.FileExists(*propdir) {
		err = fmt.Errorf("missing properties directory: %s", *propdir)
		return
	}

	baseFile := *propdir + baseFilename
	if !aputil.FileExists(baseFile) {
		err = fmt.Errorf("missing defaults file: %s", baseFile)
		return
	}

	data, rerr := ioutil.ReadFile(baseFile)
	if rerr != nil {
		err = fmt.Errorf("failed to read %s: %v", baseFile, rerr)
		return
	}

	if rerr := json.Unmarshal(data, &base); err != nil {
		err = fmt.Errorf("failed to parse %s: %v", baseFile, rerr)
		return
	}

	defaults = &base.Defaults
	descs = base.Descriptions
	return
}
