/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/satori/uuid"

	"bg/cl_common/daemonutils"
	"bg/cl_common/pgutils"
	"bg/cloud_models/appliancedb"
	"bg/common/cfgapi"
	"bg/common/cfgtree"
)

type dbStore struct {
	connInfo string
	handle   appliancedb.DataStore
}

func (db *dbStore) String() string {
	return pgutils.CensorPassword(db.connInfo)
}

func (db *dbStore) get(ctx context.Context, uuidStr string) (*cfgtree.PTree, error) {
	_, slog := daemonutils.EndpointLogger(ctx)

	slog.Infof("Loading state for %s from DB", uuidStr)

	err := db.handle.Ping()
	if err != nil {
		slog.Warnf("failed to ping DB: %s", err)
		// Try once to reconnect
		if err = db.connect(); err != nil {
			slog.Errorf("failed to reconnect to DB")
			return nil, err
		}
		slog.Infof("reconnected to DB")
	}

	u, err := uuid.FromString(uuidStr)
	if err != nil {
		slog.Errorf("invalid UUID: %s", uuidStr)
		return nil, err
	}

	store, err := db.handle.ConfigStoreByUUID(ctx, u)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			return nil, cfgapi.ErrNoConfig
		}
		slog.Errorf("failed to query appliance DB: %v", err)
		return nil, err
	}

	tree, err := cfgtree.NewPTree(rootPath, store.Config)
	if err != nil {
		slog.Errorf("failed to create configuration tree: %v", err)
		return nil, err
	}

	if !bytes.Equal(tree.Root().Hash(), store.RootHash) {
		err = fmt.Errorf("config tree hash (%s) doesn't match stored value (%s)",
			hex.EncodeToString(tree.Root().Hash()), hex.EncodeToString(store.RootHash))
		slog.Error(err)
		return nil, err
	}

	return tree, nil
}

func (db *dbStore) set(ctx context.Context, uuidStr string, tree *cfgtree.PTree) error {
	_, slog := daemonutils.EndpointLogger(ctx)

	slog.Infof("Storing state for %s to DB", uuidStr)

	err := db.handle.Ping()
	if err != nil {
		slog.Warnf("failed to ping DB: %s", err)
		// Try once to reconnect
		if err = db.connect(); err != nil {
			slog.Errorf("failed to reconnect to DB")
			return err
		}
		slog.Infof("reconnected to DB")
	}

	u, err := uuid.FromString(uuidStr)
	if err != nil {
		slog.Errorf("invalid UUID: %s", uuidStr)
		return err
	}

	store := &appliancedb.SiteConfigStore{
		RootHash:  tree.Root().Hash(),
		TimeStamp: time.Now(),
		Config:    tree.Export(false),
	}

	err = db.handle.UpsertConfigStore(ctx, u, store)
	if err != nil {
		slog.Errorf("failed to store config: %v", err)
		return err
	}
	return nil
}

func (db *dbStore) connect() error {
	err := dbConnect(db.connInfo)
	db.handle = cachedDBHandle
	return err
}

func newDBStore(connInfo string) (configStore, error) {
	dbs := dbStore{connInfo: connInfo}
	err := dbs.connect()
	if err != nil {
		slog.Warnf("%v", err)
		return nil, err
	}

	slog.Infof("Connected to appliance DB for config store")

	var store configStore = &dbs
	return store, nil
}

