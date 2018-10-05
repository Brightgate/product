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
	"context"
	"fmt"
	"io/ioutil"
	"os"

	"bg/common/cfgtree"
)

type fileStore struct {
	dir string
}

func (store *fileStore) get(ctx context.Context, uuid string) (*cfgtree.PTree, error) {
	slog.Infof("Loading state for %s from file", uuid)

	path := store.dir + "/" + uuid + ".json"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		slog.Warnf("No config file at %s", path)
		return nil, fmt.Errorf("no such appliance: %s", uuid)
	}

	file, err := ioutil.ReadFile(path)
	if err != nil {
		slog.Warnf("Failed to load %s: %v\n", path, err)
		return nil, err
	}

	tree, err := cfgtree.NewPTree("@", file)
	if err != nil {
		err = fmt.Errorf("importing %s: %v", path, err)
	}

	return tree, err
}

func newFileStore(dir string) (configStore, error) {
	var store configStore = &fileStore{dir}
	return store, nil
}
