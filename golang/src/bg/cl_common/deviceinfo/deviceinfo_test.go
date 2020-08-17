/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package deviceinfo

import (
	"bg/base_msg"
	"bg/common/network"
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/golang/protobuf/proto"
	"github.com/satori/uuid"
	"github.com/stretchr/testify/require"
)

const mockSiteUUIDStr = "10000000-0000-0000-0000-000000000001"
const mockBucketName = "bg-appliance-data-" + mockSiteUUIDStr

const mockBadUUIDStr = "20000000-0000-0000-0000-000000000002"

const mockMAC = "00:11:22:33:44:55"
const mockTS = "1234567890"
const mockTSInt = 1234567890

func setupFakeCS(t *testing.T) (*storage.Client, *fakestorage.Server) {
	fakeCS := fakestorage.NewServer([]fakestorage.Object{})
	fakeCS.CreateBucket(mockBucketName)
	return fakeCS.Client(), fakeCS
}

func TestTuple(t *testing.T) {
	var err error
	assert := require.New(t)

	tup, err := NewTupleFromStrings(mockSiteUUIDStr, mockMAC, mockTS)
	assert.NoError(err)
	assert.Equal(int64(1234567890), tup.TS.Unix())
	assert.Equal(mockSiteUUIDStr+"/"+mockMAC+"/"+mockTS, tup.String())

	tup, err = NewTupleFromStrings("bad", mockMAC, mockTS)
	assert.Error(err)

	tup, err = NewTupleFromStrings(mockSiteUUIDStr, "bad", mockTS)
	assert.Error(err)

	tup, err = NewTupleFromStrings(mockSiteUUIDStr, mockMAC, "bad")
	assert.Error(err)
}

func TestGCSStore(t *testing.T) {
	assert := require.New(t)
	ctx := context.Background()

	csclient, csserver := setupFakeCS(t)
	defer csserver.Stop()

	mapper := func(ctx context.Context, uuid uuid.UUID) (string, string, error) {
		return "gcs", fmt.Sprintf("bg-appliance-data-%s", uuid), nil
	}
	store := NewGCSStore(csclient, mapper)

	uu := uuid.Must(uuid.FromString(mockSiteUUIDStr))
	assert.True(store.SiteExists(ctx, uu))

	uu = uuid.Must(uuid.FromString(mockBadUUIDStr))
	assert.False(store.SiteExists(ctx, uu))

	hwaddr, err := net.ParseMAC(mockMAC)
	assert.NoError(err)
	devInfo := &base_msg.DeviceInfo{
		MacAddress: proto.Uint64(network.HWAddrToUint64(hwaddr)),
	}
	uu = uuid.Must(uuid.FromString(mockSiteUUIDStr))
	ts := time.Unix(mockTSInt, 0)
	path, err := store.Write(ctx, uu, devInfo, ts)
	assert.NoError(err)
	assert.Equal("https://storage.cloud.google.com/bg-appliance-data-10000000-0000-0000-0000-000000000001/obs/00:11:22:33:44:55/device_info.1234567890.pb", path)

	di, err := store.Read(ctx, uu, mockMAC, ts)
	assert.NoError(err)
	assert.NotNil(di)
	assert.Equal(devInfo.GetMacAddress(), di.GetMacAddress())

	tup, err := NewTupleFromStrings(mockSiteUUIDStr, mockMAC, mockTS)
	assert.NoError(err)
	di, err = store.ReadTuple(ctx, tup)
	assert.NoError(err)
	assert.NotNil(di)
	assert.Equal(devInfo.GetMacAddress(), di.GetMacAddress())
}

