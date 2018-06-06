/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"log"
	"os"
	"strconv"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/common"

	"github.com/satori/uuid"
)

const (
	propertyFilename = "ap_props.json"
	backupFilename   = "ap_props.json.bak"
	defaultFilename  = "ap_defaults.json"
	minConfigVersion = 10
)

var (
	propdir = flag.String("propdir", "./",
		"directory in which the property files should be stored")
	propTreeFile string
	upgradeHooks []func() error
)

func propTreeStore() error {
	if propTreeFile == "" {
		return nil
	}

	node, err := propertyInsert("@/apversion")
	if err != nil {
		return err
	}
	node.Value = common.GitVersion

	s, err := json.MarshalIndent(propTreeRoot, "", "  ")
	if err != nil {
		log.Fatalf("Failed to construct properties JSON: %v\n", err)
	}
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

	err = ioutil.WriteFile(propTreeFile, s, 0644)
	if err != nil {
		log.Printf("Failed to write properties file: %v\n", err)
	}

	return err
}

func propTreeImport(data []byte) error {
	var newRoot pnode

	metrics.treeSize.Set(float64(len(data)))
	err := json.Unmarshal(data, &newRoot)
	if err == nil {
		patchTree("@", &newRoot, "")
		propTreeRoot = &newRoot
	}
	return err
}

func propTreeLoad(name string) error {
	var file []byte
	var err error

	file, err = ioutil.ReadFile(name)
	if err != nil {
		log.Printf("Failed to load properties file %s: %v\n", name, err)
		return err
	}

	if err = propTreeImport(file); err != nil {
		if nerr := oldPropTreeParse(file); nerr != nil {
			log.Printf("Failed to import properties from %s: %v\n",
				name, err)
			return err
		}
	}

	return nil
}

func addUpgradeHook(version int32, hook func() error) {
	if version > apcfg.Version {
		msg := fmt.Sprintf("Upgrade hook %d > current max of %d\n",
			version, apcfg.Version)
		panic(msg)
	}

	if upgradeHooks == nil {
		upgradeHooks = make([]func() error, apcfg.Version+1)
	}
	upgradeHooks[version] = hook
}

func versionTree() error {
	upgraded := false
	version := 0

	node, _ := propertyInsert("@/cfgversion")
	if node != nil && node.Value != "" {
		version, _ = strconv.Atoi(node.Value)
	}
	if version < minConfigVersion {
		return fmt.Errorf("obsolete properties file")
	}
	if version > int(apcfg.Version) {
		return fmt.Errorf("properties file is newer than the software")
	}

	for version < int(apcfg.Version) {
		log.Printf("Upgrading properties from version %d to %d\n",
			version, version+1)
		version++
		if upgradeHooks[version] != nil {
			if err := upgradeHooks[version](); err != nil {
				return fmt.Errorf("upgrade failed: %v", err)
			}
		}
		node.Value = strconv.Itoa(version)
		upgraded = true
	}

	if upgraded {
		if err := propTreeStore(); err != nil {
			return fmt.Errorf("Failed to write properties: %v", err)
		}
	}
	return nil
}

/*
 * After loading the initial property values, we need to walk the tree to set
 * the parent pointers, attach any non-default operations, and possibly insert
 * into the expiration heap
 */
func patchTree(name string, node *pnode, path string) {
	node.name = name
	if len(path) > 0 {
		node.path = path + "/" + name
	} else {
		node.path = name
	}
	node.index = -1
	propAttachOps(node)
	for childName, child := range node.Children {
		child.parent = node
		patchTree(childName, child, node.path)
	}
	if node.Expires != nil {
		expirationUpdate(node)
	}
}

func dumpTree(name string, node *pnode, level int) {
	indent := ""
	for i := 0; i < level; i++ {
		indent += "  "
	}
	e := ""
	if node.Expires != nil {
		e = node.Expires.Format("2006-01-02T15:04:05")
	}
	fmt.Printf("%s%s: %s  %s\n", indent, name, node.Value, e)
	for name, child := range node.Children {
		dumpTree(name, child, level+1)
	}
}

func propTreeInit() {
	var err error

	propTreeFile = *propdir + propertyFilename
	if aputil.FileExists(propTreeFile) {
		err = propTreeLoad(propTreeFile)
	} else {
		err = fmt.Errorf("File missing")
	}

	if err != nil {
		log.Printf("Unable to load properties: %v", err)
		backupfile := *propdir + backupFilename
		if aputil.FileExists(backupfile) {
			err = propTreeLoad(backupfile)
		} else {
			err = fmt.Errorf("File missing")
		}

		if err != nil {
			log.Printf("Unable to load backup properties: %v", err)
		} else {
			log.Printf("Loaded properties from backup file")
		}
	}

	if err != nil {
		log.Printf("No usable properties files.  Loading defaults.\n")
		defaultFile := *propdir + defaultFilename
		err := propTreeLoad(defaultFile)
		if err != nil {
			log.Fatal("Unable to load default properties")
		}
		applianceUUID := uuid.NewV4().String()
		if node, _ := propertyInsert("@/uuid"); node != nil {
			propertyUpdate(node, applianceUUID, nil)
		}

		// XXX: this needs to come from the cloud - not hardcoded
		applianceSiteID := "7410"
		if node, _ := propertyInsert("@/siteid"); node != nil {
			propertyUpdate(node, applianceSiteID, nil)
		}
	}

	if err == nil {
		if err = versionTree(); err != nil {
			log.Fatalf("Failed version check: %v\n", err)
		}
	}

	dumpTree("root", propTreeRoot, 0)
}
