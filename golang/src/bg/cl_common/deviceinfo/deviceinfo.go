/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package deviceinfo

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"path"
	"strconv"
	"time"

	"bg/base_msg"
	"bg/common/network"

	"github.com/pkg/errors"
	"github.com/satori/uuid"

	"cloud.google.com/go/storage"
	"github.com/golang/protobuf/proto"
)

const gcsBaseURL = "https://storage.cloud.google.com/"

// Tuple represents the identity of a DeviceInfo in a compound way.  This
// makes it a bit easier to move DeviceInfo identity around without
// having to keep track of all three parameters
type Tuple struct {
	SiteUUID uuid.UUID
	MAC      string
	TS       time.Time
}

// NewTupleFromStrings creates a new Tuple from the provided siteUUID, mac
// and timestamp.  It checks them for validity.
func NewTupleFromStrings(siteUUID, mac, ts string) (Tuple, error) {
	uu, err := uuid.FromString(siteUUID)
	if err != nil {
		return Tuple{}, errors.Wrapf(err, "bad uuid %s", siteUUID)
	}
	timeInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return Tuple{}, errors.Wrapf(err, "bad timestamp %s", ts)
	}
	_, err = net.ParseMAC(mac)
	if err != nil {
		return Tuple{}, errors.Wrapf(err, "bad mac %s", mac)
	}
	return Tuple{
		SiteUUID: uu,
		MAC:      mac,
		TS:       time.Unix(timeInt, 0),
	}, nil
}

func (t Tuple) String() string {
	return fmt.Sprintf("%s/%s/%d", t.SiteUUID.String(), t.MAC, t.TS.Unix())
}

// Store represents a storage mechanism for DeviceInfo records, organized into
// a Site UUID / Mac Address / Timestamp logical hierarchy.
type Store interface {
	Name() string
	SiteExists(ctx context.Context, siteUUID uuid.UUID) (bool, error)
	Write(ctx context.Context, siteUUID uuid.UUID, devInfo *base_msg.DeviceInfo, ts time.Time) (string, error)
	NewReader(ctx context.Context, siteUUID uuid.UUID, mac string, ts time.Time) (io.Reader, error)
	Read(ctx context.Context, siteUUID uuid.UUID, mac string, ts time.Time) (*base_msg.DeviceInfo, error)
	ReadTuple(ctx context.Context, tuple Tuple) (*base_msg.DeviceInfo, error)
}

// CloudStorageUUIDMapper represents an interface which maps a Site UUID to
// a storage provider (like "gcs") and location (bucket name).  Typically,
// the appliancedb is used to provide this functionality, but in some cases
// (i.e. cl-obs, which has its own site list) a more relaxed mapping is used.
type CloudStorageUUIDMapper func(context.Context, uuid.UUID) (string, string, error)

// GCSStore is a Google Cloud Storage implementation of the DeviceInfoStore
// interface.
type GCSStore struct {
	client     *storage.Client
	uuidMapper CloudStorageUUIDMapper
}

// NewGCSStore creates a new instance of GCSStore based on a storage.Client and
// a site-to-storage mapping function.
func NewGCSStore(client *storage.Client, uuidMapper CloudStorageUUIDMapper) *GCSStore {
	return &GCSStore{
		client:     client,
		uuidMapper: uuidMapper,
	}
}

func (g *GCSStore) getBucket(ctx context.Context, siteUU uuid.UUID) (*storage.BucketHandle, error) {
	provider, bucketName, err := g.uuidMapper(ctx, siteUU)
	if err != nil {
		return nil, errors.Wrapf(err, "could not get Cloud Storage record for %s", siteUU)
	}
	if provider != "gcs" {
		return nil, errors.Errorf("not implemented for provider %s", provider)
	}
	return g.client.Bucket(bucketName), nil
}

func (g *GCSStore) writeCSObject(ctx context.Context, siteUU uuid.UUID, filePath string, data []byte) (string, error) {
	bkt, err := g.getBucket(ctx, siteUU)
	if err != nil {
		return "", err
	}

	obj := bkt.Object(filePath)
	w := obj.NewWriter(ctx)
	p := gcsBaseURL + path.Join(obj.BucketName(), obj.ObjectName())
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return "", errors.Wrapf(err, "failed writing to %s", p)
	}
	if err := w.Close(); err != nil {
		return "", errors.Wrapf(err, "failed closing %s", p)
	}

	return p, nil
}

// Name returns the name of the store.
func (g *GCSStore) Name() string { return "gcs" }

// SiteExists tests whether the site appears to exist in the store.
func (g *GCSStore) SiteExists(ctx context.Context, siteUUID uuid.UUID) (bool, error) {
	bkt, err := g.getBucket(ctx, siteUUID)
	if err != nil {
		return false, err
	}
	if _, err = bkt.Attrs(ctx); err != nil {
		if err == storage.ErrObjectNotExist {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Write emits a DeviceInfo to the store.
func (g *GCSStore) Write(ctx context.Context, siteUUID uuid.UUID, devInfo *base_msg.DeviceInfo, ts time.Time) (string, error) {
	hwaddr := network.Uint64ToHWAddr(devInfo.GetMacAddress())
	filename := fmt.Sprintf("device_info.%d.pb", int(ts.Unix()))
	filePath := path.Join("obs", hwaddr.String(), filename)

	out, err := proto.Marshal(devInfo)
	if err != nil {
		return "", errors.Wrap(err, "marshal failed")
	}
	return g.writeCSObject(ctx, siteUUID, filePath, out)
}

// NewReader creates an io.Reader for getting a DeviceInfo from the store.
func (g *GCSStore) NewReader(ctx context.Context, siteUUID uuid.UUID, mac string, ts time.Time) (io.Reader, error) {
	bkt, err := g.getBucket(ctx, siteUUID)
	if err != nil {
		return nil, err
	}

	filename := fmt.Sprintf("device_info.%d.pb", int(ts.Unix()))
	filePath := path.Join("obs", mac, filename)
	rdr, err := bkt.Object(filePath).NewReader(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "couldn't make reader for %s %s", siteUUID, filePath)
	}

	return rdr, nil
}

// Read gets a DeviceInfo from the store.
func (g *GCSStore) Read(ctx context.Context, siteUUID uuid.UUID, mac string, ts time.Time) (*base_msg.DeviceInfo, error) {
	rdr, err := g.NewReader(ctx, siteUUID, mac, ts)
	if err != nil {
		return nil, err
	}

	buf, err := ioutil.ReadAll(rdr)
	if err != nil {
		return nil, errors.Wrap(err, "couldn't ReadAll")
	}

	di := &base_msg.DeviceInfo{}
	err = proto.Unmarshal(buf, di)
	if err != nil {
		return nil, errors.Wrap(err, "couldn't unmarshal inventory record")
	}
	return di, nil
}

// ReadTuple gets a DeviceInfo from the store using Tuple provided.
func (g *GCSStore) ReadTuple(ctx context.Context, tuple Tuple) (*base_msg.DeviceInfo, error) {
	return g.Read(ctx, tuple.SiteUUID, tuple.MAC, tuple.TS)
}

// NullStore is an implementation of the DeviceInfoStore which throws
// all of the input data away.
type NullStore struct{}

// NewNullStore creates a new NullStore
func NewNullStore() *NullStore {
	return &NullStore{}
}

// Name returns the name of the Store
func (n *NullStore) Name() string { return "null" }

// SiteExists tests whether the site appears to exist in the store; in this
// case it always returns false.
func (n *NullStore) SiteExists(ctx context.Context, siteUUID uuid.UUID) (bool, error) {
	return false, nil
}

// Write emits a DeviceInfo to the store.  In this case it does nothing.
// in order not to lose data.
func (n *NullStore) Write(ctx context.Context, uuid uuid.UUID, devInfo *base_msg.DeviceInfo, ts time.Time) (string, error) {
	return "<nullstore>", nil
}

// NewReader creates an io.Reader for getting a DeviceInfo from the store.
func (n *NullStore) NewReader(ctx context.Context, siteUUID uuid.UUID, mac string, ts time.Time) (io.Reader, error) {
	return nil, errors.New("no such deviceinfo (null store)")
}

// Read gets a DeviceInfo from the store.
func (n *NullStore) Read(ctx context.Context, siteUUID uuid.UUID, mac string, ts time.Time) (*base_msg.DeviceInfo, error) {
	return nil, errors.New("no such deviceinfo (null store)")
}

// ReadTuple gets a DeviceInfo from the store using Tuple provided.
func (n *NullStore) ReadTuple(ctx context.Context, tuple Tuple) (*base_msg.DeviceInfo, error) {
	return nil, errors.New("no such deviceinfo (null store)")
}
