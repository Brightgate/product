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

	"bg/common/cfgtree"
)

type multiStore struct {
	stores []configStore
}

func (ms *multiStore) get(ctx context.Context, uuid string) (*cfgtree.PTree, error) {
	var err error
	for _, s := range ms.stores {
		var tree *cfgtree.PTree
		tree, err = s.get(ctx, uuid)
		if err == nil {
			return tree, err
		}
	}
	return nil, err
}

func (ms *multiStore) add(store configStore) {
	slog.Infof("Adding configuration store [%d] %T: %s",
		len(ms.stores), store, store)
	ms.stores = append(ms.stores, store)
}

func newMultiStore() (configStore, error) {
	ms := multiStore{}
	var store configStore = &ms
	return store, nil
}
