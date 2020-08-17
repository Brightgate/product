/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"bg/cloud_models/appliancedb"
	"bg/cloud_models/appliancedb/mocks"
	"bg/cloud_rpc"

	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/storage"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/satori/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

var fakeGCSServer *fakestorage.Server

func getFakeStorageClient(_ context.Context) (*storage.Client, error) {
	return fakeGCSServer.Client(), nil
}

func mkOrgUUID(n int) uuid.UUID {
	return uuid.Must(uuid.FromString(fmt.Sprintf("00000000-0000-0000-0000-%012d", n)))
}

func mkSiteUUID(n int) uuid.UUID {
	return uuid.Must(uuid.FromString(fmt.Sprintf("00000000-0000-0000-0001-%012d", n)))
}

func mkAppUUID(n int) uuid.UUID {
	return uuid.Must(uuid.FromString(fmt.Sprintf("00000000-0000-0000-0002-%012d", n)))
}

func mkReleaseUUID(n int) uuid.UUID {
	return uuid.Must(uuid.FromString(fmt.Sprintf("00000000-0000-0000-0003-%012d", n)))
}

func testUpgradeComplete(ctx context.Context, ds appliancedb.DataStore, logs *observer.ObservedLogs,
	assert *require.Assertions, ts time.Time, relUU, appUU, siteUU uuid.UUID,
	commits map[string]string) []observer.LoggedEntry {
	tsProto, err := ptypes.TimestampProto(ts)
	assert.NoError(err)
	report := &cloud_rpc.UpgradeReport{
		Result:      cloud_rpc.UpgradeReport_REPORT,
		RecordTime:  tsProto,
		ReleaseUuid: relUU.String(),
		Commits:     commits,
	}
	reportBytes, err := proto.Marshal(report)
	assert.NoError(err)

	msg := &pubsub.Message{
		Attributes: map[string]string{
			"appliance_uuid": appUU.String(),
			"site_uuid":      siteUU.String(),
		},
		Data: reportBytes,
	}
	upgradeMessage(ctx, ds, appUU, siteUU, msg)
	return logs.TakeAll() // truncate the logs for the next test
}

func testUpgradeInstalled(ctx context.Context, ds appliancedb.DataStore, logs *observer.ObservedLogs,
	assert *require.Assertions, ts time.Time, relUU, appUU, siteUU uuid.UUID, upgradeErr error,
	upgradeOutput string) []observer.LoggedEntry {
	tsProto, err := ptypes.TimestampProto(ts)
	assert.NoError(err)
	report := &cloud_rpc.UpgradeReport{
		RecordTime:  tsProto,
		ReleaseUuid: relUU.String(),
		Output:      []byte(upgradeOutput),
	}
	if upgradeErr != nil {
		report.Result = cloud_rpc.UpgradeReport_FAILURE
		report.Error = upgradeErr.Error()
	} else {
		report.Result = cloud_rpc.UpgradeReport_SUCCESS
	}
	reportBytes, err := proto.Marshal(report)
	assert.NoError(err)

	msg := &pubsub.Message{
		Attributes: map[string]string{
			"appliance_uuid": appUU.String(),
			"site_uuid":      siteUU.String(),
		},
		Data: reportBytes,
	}
	upgradeMessage(ctx, ds, appUU, siteUU, msg)
	return logs.TakeAll() // truncate the logs for the next test
}

func TestUpgradeMessage(t *testing.T) {
	mkArtifactArray := func(m map[string]string) []appliancedb.ReleaseArtifact {
		var arr []appliancedb.ReleaseArtifact
		for repo, commit := range m {
			b, err := hex.DecodeString(commit)
			if err != nil {
				panic(err)
			}
			arr = append(arr, appliancedb.ReleaseArtifact{
				Repo:   repo,
				Commit: b,
			})
		}
		return arr
	}

	mockReleases := []appliancedb.Release{
		{
			UUID:     mkReleaseUUID(1),
			Creation: time.Now(),
			Platform: "mt7623",
			Commits: mkArtifactArray(map[string]string{
				"R1": "11111111",
				"R2": "22222222",
			}),
		},
	}

	mockOrgs := []appliancedb.Organization{
		{
			UUID: mkOrgUUID(1),
			Name: "org1",
		},
	}

	mockSites := []appliancedb.CustomerSite{
		{
			UUID:             mkSiteUUID(1),
			OrganizationUUID: mockOrgs[0].UUID,
			Name:             "site1",
		},
	}

	mockAppliances := []appliancedb.ApplianceID{
		{
			ApplianceUUID: mkAppUUID(1),
			SiteUUID:      mockSites[0].UUID,
		},
	}

	getStorageClient = getFakeStorageClient
	fakeGCSServer = fakestorage.NewServer([]fakestorage.Object{})
	defer fakeGCSServer.Stop()

	ds := &mocks.DataStore{}
	ds.Test(t)

	// For upgrade reports (REPORT)
	for _, rel := range mockReleases {
		ds.On("GetRelease", mock.Anything, rel.UUID).Return(&rel, nil)
	}
	ds.On("GetRelease", mock.Anything, uuid.Nil).Return(nil, nil)
	ds.On("GetRelease", mock.Anything, mock.Anything).Return(nil, appliancedb.NotFoundError{})
	ds.On("SetCurrentRelease", mock.Anything, mock.AnythingOfType("uuid.UUID"),
		mock.AnythingOfType("uuid.UUID"), mock.AnythingOfType("time.Time"),
		mock.AnythingOfType("map[string]string")).Return(nil)

	// For upgrade reports (SUCCESS/FAILURE)
	ds.On("SetUpgradeResults", mock.Anything, mock.AnythingOfType("time.Time"),
		mock.AnythingOfType("uuid.UUID"), mock.AnythingOfType("uuid.UUID"),
		mock.AnythingOfType("bool"), mock.AnythingOfType("sql.NullString"),
		mock.AnythingOfType("string")).Return(nil)
	for _, app := range mockAppliances {
		bucket := "bg-appliance-data-" + app.SiteUUID.String()
		ds.On("CloudStorageByUUID", mock.Anything, app.SiteUUID).Return(
			&appliancedb.SiteCloudStorage{
				Bucket:   bucket,
				Provider: "gcs",
			}, nil)
		fakeGCSServer.CreateBucket(bucket)
	}

	defer ds.AssertExpectations(t)

	assert := require.New(t)
	ctx := context.Background()

	// Set up logging.  Since upgradeMessage() doesn't return an error, or
	// have other side effects outside the mocked database, many of our
	// assertions will be on the contents of the logs.
	zcore, logs := observer.New(zap.DebugLevel)
	log = zap.New(zcore)
	slog = log.Sugar()

	// Reports of completion
	tUR := func(ts time.Time, relUU, appUU, siteUU uuid.UUID, commits map[string]string) []observer.LoggedEntry {
		return testUpgradeComplete(ctx, ds, logs, assert, ts, relUU, appUU, siteUU, commits)
	}

	// Send a basic upgrade report and make sure it goes through cleanly.
	appUU := mockAppliances[0].ApplianceUUID
	siteUU := mockSites[0].UUID
	logEntries := tUR(time.Now(), mockReleases[0].UUID, appUU, siteUU, nil)
	assert.Len(logEntries, 1)
	assert.Equal("Set current release", logEntries[0].Message)

	// Send an upgrade report for a release that doesn't exist.
	logEntries = tUR(time.Now(), mkReleaseUUID(99), appUU, siteUU, nil)
	assert.Len(logEntries, 2)
	assert.Equal("failed to retrieve release from database", logEntries[1].Message)

	// Send an upgrade report with the nil release and no commits, and make
	// sure we only get the one "Set current release" message.
	logEntries = tUR(time.Now(), uuid.Nil, appUU, siteUU, nil)
	assert.Len(logEntries, 1)
	assert.Equal("Set current release", logEntries[0].Message)

	// Send an upgrade report with the nil release and some commits.
	logEntries = tUR(time.Now(), uuid.Nil, appUU, siteUU,
		map[string]string{
			"R1": "11111111",
			"R2": "22222222",
		})
	assert.Len(logEntries, 1)
	assert.Equal("Set current release", logEntries[0].Message)

	// Send an upgrade report with commits, specified by partial hashes and
	// "git describe" decoration matching how the release was defined.
	logEntries = tUR(time.Now(), mockReleases[0].UUID, appUU, siteUU,
		map[string]string{
			"R1": "11111111",
			"R2": "22222222",
		})
	assert.Len(logEntries, 1)
	assert.Equal("Set current release", logEntries[0].Message)

	// Send an upgrade report with commits, specified by partial hashes that
	// don't match how the release was defined.
	logEntries = tUR(time.Now(), mockReleases[0].UUID, appUU, siteUU,
		map[string]string{
			"R1": "11111110",
			"R2": "22222220",
		})
	assert.Len(logEntries, 2)
	assert.Equal("Set current release", logEntries[0].Message)
	assert.Equal("Reported release UUID doesn't match commits in database",
		logEntries[1].Message)
	// The log context should have six fields: appliance and site UUID, plus
	// the four fields we check explicitly.
	assert.Len(logEntries[1].Context, 6)
	assert.Equal("11111110", logEntries[1].ContextMap()["reported_R1"])
	assert.Equal("22222220", logEntries[1].ContextMap()["reported_R2"])
	assert.Equal("11111111", logEntries[1].ContextMap()["expected_R1"])
	assert.Equal("22222222", logEntries[1].ContextMap()["expected_R2"])

	// Send an upgrade report with commits, specified by partial hashes and
	// "git describe" decoration matching how the release was defined.
	logEntries = tUR(time.Now(), mockReleases[0].UUID, appUU, siteUU,
		map[string]string{
			"R1": "1.0-beta.1.0.1-12-g11111111-dirty",
			"R2": "1.0-beta.1.0.1-12-g22222222",
		})
	assert.Len(logEntries, 1)
	assert.Equal("Set current release", logEntries[0].Message)

	// Send an upgrade report with commits, specified by partial hashes and
	// "git describe" decoration that don't match how the release was
	// defined.
	logEntries = tUR(time.Now(), mockReleases[0].UUID, appUU, siteUU,
		map[string]string{
			"R1": "1.0-beta.1.0.1-12-g11111110-dirty",
			"R2": "1.0-beta.1.0.1-12-g22222220",
		})
	assert.Len(logEntries, 2)
	assert.Equal("Set current release", logEntries[0].Message)
	assert.Equal("Reported release UUID doesn't match commits in database",
		logEntries[1].Message)
	// The log context should have six fields: appliance and site UUID, plus
	// the four fields we check explicitly.
	assert.Len(logEntries[1].Context, 6)
	assert.Equal("11111110", logEntries[1].ContextMap()["reported_R1"])
	assert.Equal("22222220", logEntries[1].ContextMap()["reported_R2"])
	assert.Equal("11111111", logEntries[1].ContextMap()["expected_R1"])
	assert.Equal("22222222", logEntries[1].ContextMap()["expected_R2"])

	// Reports of installation
	tUI := func(ts time.Time, relUU, appUU, siteUU uuid.UUID, err error, out string) []observer.LoggedEntry {
		return testUpgradeInstalled(ctx, ds, logs, assert, ts, relUU, appUU, siteUU, err, out)
	}

	// Submit a report of successful upgrade.
	logEntries = tUI(time.Now(), mockReleases[0].UUID, appUU, siteUU, nil, "happy happy joy joy")
	assert.Len(logEntries, 1)
	assert.Equal("archived successful upgrade log", logEntries[0].Message)
	objects, _, err := fakeGCSServer.ListObjects("bg-appliance-data-"+siteUU.String(), "", "", false)
	assert.NoError(err)
	assert.Len(objects, 1)
	assert.Equal("bg-appliance-data-"+siteUU.String(), objects[0].BucketName)
	assert.Equal("happy happy joy joy", string(objects[0].Content))
	assert.Contains(objects[0].Name, "upgrade_log/")
	assert.Contains(objects[0].Name, appUU.String())
	assert.Contains(objects[0].Name, "-success")

	// Submit a report of failed upgrade.  We don't have a way to clear the
	// bucket between runs, so we rely on the fact that we get objects back
	// sorted by name.
	logEntries = tUI(time.Now().Add(time.Second), mockReleases[0].UUID, appUU, siteUU,
		errors.New("oopsies"), "Oh, noes!")
	assert.Len(logEntries, 1)
	assert.Equal("archived failed upgrade log", logEntries[0].Message)
	objects, _, err = fakeGCSServer.ListObjects("bg-appliance-data-"+siteUU.String(), "", "", false)
	assert.NoError(err)
	assert.Len(objects, 2)
	assert.Equal("bg-appliance-data-"+siteUU.String(), objects[1].BucketName)
	assert.Equal("oopsies\n\nOh, noes!", string(objects[1].Content))
	assert.Contains(objects[1].Name, "upgrade_log/")
	assert.Contains(objects[1].Name, appUU.String())
	assert.Contains(objects[1].Name, "-failure")
}

