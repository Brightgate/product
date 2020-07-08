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

package appliancedb

import (
	"bytes"
	"context"
	"encoding/hex"
	"math/rand"
	"testing"
	"time"

	"github.com/satori/uuid"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

func buildWRT(commit []byte, generation int) (*ReleaseArtifact, *ReleaseArtifact, *ReleaseArtifact) {
	if len(commit) == 0 {
		commit = make([]byte, 160/8) // SHA-1 length
		rand.Read(commit)
	}

	rootHash := make([]byte, 256/8) // SHA-256 length
	rand.Read(rootHash)
	kernelHash := make([]byte, 256/8)
	rand.Read(kernelHash)
	ramdiskHash := make([]byte, 256/8)
	rand.Read(ramdiskHash)

	return &ReleaseArtifact{
			Repo:       "WRT",
			Commit:     commit,
			Generation: generation,
			Platform:   "mt7623",
			Filename:   "root.squashfs",
			HashType:   "SHA256",
			Hash:       rootHash,
		}, &ReleaseArtifact{
			Repo:       "WRT",
			Commit:     commit,
			Generation: generation,
			Platform:   "mt7623",
			Filename:   "uImage.itb",
			HashType:   "SHA256",
			Hash:       kernelHash,
		}, &ReleaseArtifact{
			Repo:       "WRT",
			Commit:     commit,
			Generation: generation,
			Platform:   "mt7623",
			Filename:   "uImage-ramdisk.itb",
			HashType:   "SHA256",
			Hash:       ramdiskHash,
		}
}

func buildPS(commit []byte, generation int, platform string) *ReleaseArtifact {
	if len(commit) == 0 {
		commit = make([]byte, 160/8) // SHA-1 length
		rand.Read(commit)
	}

	bgappHash := make([]byte, 256/8) // SHA-256 length
	rand.Read(bgappHash)

	return &ReleaseArtifact{
		Repo:       "PS",
		Commit:     commit,
		Generation: generation,
		Platform:   platform,
		Filename:   "bg-appliance_0.0.1905071816-1_arm_cortex-a7_neon-vfpv4.ipk",
		HashType:   "SHA256",
		Hash:       bgappHash,
	}
}

func buildXS(commit []byte, generation int, platform string) *ReleaseArtifact {
	if len(commit) == 0 {
		commit = make([]byte, 160/8) // SHA-1 length
		rand.Read(commit)
	}

	hostapdHash := make([]byte, 256/8) // SHA-256 length
	rand.Read(hostapdHash)

	return &ReleaseArtifact{
		Repo:       "XS",
		Commit:     commit,
		Generation: generation,
		Platform:   platform,
		Filename:   "bg-hostapd_2.8-1_arm_cortex-a7_neon-vfpv4.ipk",
		HashType:   "SHA256",
		Hash:       hostapdHash,
	}
}

func testReleaseArtifacts(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)
	db := ds.(*ApplianceDB)

	rootRA, kernelRA, ramdiskRA := buildWRT(nil, 0)

	rootUUID, err := ds.InsertArtifact(ctx, *rootRA)
	assert.NoError(err)
	kernelUUID, err := ds.InsertArtifact(ctx, *kernelRA)
	assert.NoError(err)
	ramdiskUUID, err := ds.InsertArtifact(ctx, *ramdiskRA)
	assert.NoError(err)
	assert.NotEqual(rootUUID, kernelUUID)
	assert.NotEqual(rootUUID, ramdiskUUID)
	assert.NotEqual(kernelUUID, ramdiskUUID)

	// Insert an existing artifact; note that it violates the uniqueness
	// constraints, and doesn't add anything to the database.
	_, err = ds.InsertArtifact(ctx, *rootRA)
	assert.IsType(UniqueViolationError{}, err)
	assert.Equal("artifacts_platform_name_repo_name_commit_hash_generation_fi_key",
		err.(UniqueViolationError).Constraint)
	var artifactCount int
	err = db.GetContext(ctx, &artifactCount, `SELECT count(1) FROM artifacts`)
	assert.NoError(err)
	assert.Equal(3, artifactCount)
}

func testReleases(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	// Creating a release with no artifacts should fail
	_, err := ds.InsertRelease(ctx, []*ReleaseArtifact{}, nil)
	assert.Error(err)

	// Build the artifacts for one release
	rootRA, kernelRA, ramdiskRA := buildWRT(nil, 0)
	psRA := buildPS(nil, 0, "mt7623")
	xsRA := buildXS(nil, 0, "mt7623")

	// Add those artifacts to the database
	rootRA, err = ds.InsertArtifact(ctx, *rootRA)
	assert.NoError(err)
	kernelRA, err = ds.InsertArtifact(ctx, *kernelRA)
	assert.NoError(err)
	ramdiskRA, err = ds.InsertArtifact(ctx, *ramdiskRA)
	assert.NoError(err)
	psRA, err = ds.InsertArtifact(ctx, *psRA)
	assert.NoError(err)
	xsRA, err = ds.InsertArtifact(ctx, *xsRA)
	assert.NoError(err)

	// Create a release from some of those artifacts.  See that doing it
	// again with the same artifacts gives us back an error.
	artifacts := []*ReleaseArtifact{rootRA, kernelRA, ramdiskRA, psRA}
	mtRel1, err := ds.InsertRelease(ctx, artifacts, nil)
	assert.NoError(err)
	_, err = ds.InsertRelease(ctx, artifacts, nil)
	assert.Error(err)

	// Add the hostapd package in and make sure that we do get a new
	// release.  This makes sure that despite an existing release having a
	// subset of these artifacts, we can still create a new release with
	// these.
	artifacts = append(artifacts, xsRA)
	_, err = ds.InsertRelease(ctx, artifacts, nil)
	assert.NoError(err)

	// Check that GetRelease() will pull a release back out correctly.
	rel, err := ds.GetRelease(ctx, mtRel1)
	assert.NoError(err)
	assert.Equal(mtRel1, rel.UUID)
	assert.True(rel.OnePlatform)
	assert.Equal("mt7623", rel.Platform)
	assert.Len(rel.Commits, 4)
	for _, ra := range artifacts[:4] {
		found := false
		for _, commit := range rel.Commits {
			// The artifacts we get back don't include the UUID or
			// platform, so we can't compare the structs directly,
			// or use the UUID as the identity, so compare the rest
			// of the members.
			if bytes.Equal(ra.Hash, commit.Hash) {
				assert.Equal(ra.Repo, commit.Repo)
				assert.Equal(ra.Commit, commit.Commit)
				assert.Equal(ra.Generation, commit.Generation)
				assert.Equal(ra.Filename, commit.Filename)
				assert.Equal(ra.HashType, commit.HashType)
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Inserted artifact with hash %s, but it didn't come out again",
				hex.EncodeToString(ra.Hash))
		}
	}

	// Make sure that an unknown release returns a NotFoundError.
	_, err = ds.GetRelease(ctx, uuid.NewV4())
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	// Make sure that the nil release returns nil.
	nilRel, err := ds.GetRelease(ctx, uuid.Nil)
	assert.NoError(err)
	assert.Nil(nilRel)

	// Build a new appliance stack and create a new release with that.
	psRA2 := buildPS(nil, 0, "mt7623")
	psRA2, err = ds.InsertArtifact(ctx, *psRA2)
	assert.NoError(err)
	artifacts = []*ReleaseArtifact{rootRA, kernelRA, ramdiskRA, psRA2, xsRA}
	_, err = ds.InsertRelease(ctx, artifacts, nil)
	assert.NoError(err)

	// Decide we don't want to put hostapd into the release, after all, and
	// create a new release.  This tests that this set of artifacts being a
	// subset of an existing release won't block this release from being
	// published.
	artifacts = []*ReleaseArtifact{rootRA, kernelRA, ramdiskRA, psRA2}
	_, err = ds.InsertRelease(ctx, artifacts, nil)
	assert.NoError(err)

	// Build artifacts for x86
	psxRA := buildPS(psRA.Commit, 0, "x86")
	xsxRA := buildXS(xsRA.Commit, 0, "x86")

	// Add those artifacts to the database
	psxRA, err = ds.InsertArtifact(ctx, *psxRA)
	assert.NoError(err)
	xsxRA, err = ds.InsertArtifact(ctx, *xsxRA)
	assert.NoError(err)

	// Create the x86 release.
	_, err = ds.InsertRelease(ctx, []*ReleaseArtifact{psxRA, xsxRA}, nil)
	assert.NoError(err)

	// Test some metadata
	psRA3 := buildPS(nil, 0, "mt7623")
	psRA3, err = ds.InsertArtifact(ctx, *psRA3)
	assert.NoError(err)
	artifacts = []*ReleaseArtifact{rootRA, kernelRA, ramdiskRA, psRA3, xsRA}
	meta := map[string]string{"name": "my big fancy greek name"}
	greekReleaseUUID, err := ds.InsertRelease(ctx, artifacts, meta)
	assert.NoError(err)

	// Check that ListReleases works
	releases, err := ds.ListReleases(ctx)
	assert.NoError(err)
	// Make sure we get back as many as we've put in
	assert.Len(releases, 6)
	// Dive into the contents of one, first making sure that it's the one we
	// expect.
	assert.Equal(greekReleaseUUID, releases[5].UUID)
	assert.Equal("mt7623", releases[5].Platform)
	assert.Equal("my big fancy greek name", releases[5].Metadata["name"])
	assert.Len(releases[5].Metadata, 1)
	assert.ElementsMatch(releases[5].Commits, []ReleaseArtifact{
		{
			Repo:       rootRA.Repo,
			Commit:     rootRA.Commit,
			Generation: rootRA.Generation,
		},
		{
			Repo:       psRA3.Repo,
			Commit:     psRA3.Commit,
			Generation: psRA3.Generation,
		},
		{
			Repo:       xsRA.Repo,
			Commit:     xsRA.Commit,
			Generation: xsRA.Generation,
		},
	})

	// Tweak an artifact already in a release to be a different platform, so
	// that the release list will have an error indicating an inconsistent
	// release.
	db := ds.(*ApplianceDB)
	tweakArtifactPlatform := `
		UPDATE artifacts
		SET platform_name = $2
		WHERE artifact_uuid = $1`

	// Tweak the last release
	res, err := db.ExecContext(ctx, tweakArtifactPlatform, psRA3.UUID, "rpi3")
	assert.NoError(err)
	rows, err := res.RowsAffected()
	assert.NoError(err)
	assert.Equal(1, int(rows))
	releases, err = ds.ListReleases(ctx)
	assert.Error(err)
	assert.IsType(BadReleaseError{}, err)
	assert.NotPanics(func() { _ = err.Error() })
	assert.Len(releases, 5)
	// Make sure to tweak it back once we're done.
	_, err = db.ExecContext(ctx, tweakArtifactPlatform, psRA3.UUID, "mt7623")
	assert.NoError(err)

	// Get the current release for an appliance not in the database
	appUU := testID1.ApplianceUUID
	curRelUU, err := ds.GetCurrentRelease(ctx, appUU)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	// Get the target release for an appliance not in the database
	targRelUU, err := ds.GetTargetRelease(ctx, appUU)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	// Set the current release for an appliance not in the database
	curRelUU = greekReleaseUUID
	err = ds.SetCurrentRelease(ctx, appUU, curRelUU, time.Now().UTC(), nil)
	assert.IsType(ForeignKeyError{}, err, "%+v", err)
	assert.Equal("appliance_release_history_appliance_uuid_fkey", err.(ForeignKeyError).Constraint)

	// Set the target release for an appliance not in the database
	targRelUU = greekReleaseUUID
	err = ds.SetTargetRelease(ctx, appUU, targRelUU)
	assert.IsType(ForeignKeyError{}, err)
	assert.Equal("appliance_release_targets_appliance_uuid_fkey", err.(ForeignKeyError).Constraint)

	// Register the appliance, and try again.
	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)
	err = ds.SetCurrentRelease(ctx, appUU, curRelUU, time.Now().UTC(), nil)
	assert.NoError(err)
	err = ds.SetTargetRelease(ctx, appUU, targRelUU)
	assert.NoError(err)

	// Set the current release for a release not in the database
	newRelUU := uuid.NewV4()
	err = ds.SetCurrentRelease(ctx, appUU, newRelUU, time.Now().UTC(), nil)
	assert.IsType(ForeignKeyError{}, err)
	assert.Equal("appliance_release_history_release_uuid_fkey", err.(ForeignKeyError).Constraint)

	// Set the target release for a release not in the database
	err = ds.SetTargetRelease(ctx, appUU, newRelUU)
	assert.IsType(ForeignKeyError{}, err)
	assert.Equal("appliance_release_targets_release_uuid_fkey", err.(ForeignKeyError).Constraint)

	// Make sure what we get out is what we put in
	uu, err := ds.GetCurrentRelease(ctx, appUU)
	assert.NoError(err)
	assert.Equal(curRelUU, uu)
	uu, err = ds.GetTargetRelease(ctx, appUU)
	assert.NoError(err)
	assert.Equal(targRelUU, uu)

	// Make sure we can update the current release
	err = ds.SetCurrentRelease(ctx, appUU, mtRel1, time.Now().UTC(), nil)
	assert.NoError(err)
	uu, err = ds.GetCurrentRelease(ctx, appUU)
	assert.NoError(err)
	assert.Equal(mtRel1, uu)

	// Make sure we can update the target release
	err = ds.SetTargetRelease(ctx, appUU, mtRel1)
	assert.NoError(err)
	uu, err = ds.GetTargetRelease(ctx, appUU)
	assert.NoError(err)
	assert.Equal(mtRel1, uu)

	// Make sure we can set the current release to the null UUID
	err = ds.SetCurrentRelease(ctx, appUU, uuid.Nil, time.Now().UTC(), nil)
	assert.NoError(err)
	uu, err = ds.GetCurrentRelease(ctx, appUU)
	assert.NoError(err)
	assert.Equal(uuid.Nil, uu)

	// Make sure we can set the current release to the null UUID, with
	// commits.
	commitMap := map[string]string{
		"PS":  "0707077",
		"XS":  "4923719436dd41890c7d57b8d221da82bef3b8eb",
		"WRT": "d33cf56973",
	}
	then := time.Now().UTC()
	err = ds.SetCurrentRelease(ctx, appUU, uuid.Nil, then, commitMap)
	assert.NoError(err)
	uu, err = ds.GetCurrentRelease(ctx, appUU)
	assert.NoError(err)
	assert.Equal(uuid.Nil, uu)

	// Check that the commits got recorded correctly.
	commits := make(KVMap)
	err = db.GetContext(ctx, &commits, `
		SELECT repo_commits
		FROM appliance_release_history
		WHERE stage = 'complete' AND appliance_uuid = $1`,
		appUU)
	assert.NoError(err)
	assert.EqualValues(commitMap, commits)

	// Register the same data, and see that the timestamp doesn't change.
	err = ds.SetCurrentRelease(ctx, appUU, uuid.Nil, time.Now().UTC(), commitMap)
	assert.NoError(err)
	var now time.Time
	err = db.GetContext(ctx, &now, `
		SELECT updated_ts
		FROM appliance_release_history
		WHERE stage = 'complete' AND appliance_uuid = $1`,
		appUU)
	assert.NoError(err)
	// testify has WithinDuration, which we could use here, but the inverse,
	// which we'd need next, doesn't exist.  And until #780 is fixed, it
	// can't compare durations directly.
	assert.Less(int64(now.Sub(then)), int64(time.Microsecond))

	// Update one of the commits, see that the timestamp does change.
	commitMap["PS"] = "d4db33f"
	err = ds.SetCurrentRelease(ctx, appUU, uuid.Nil, time.Now().UTC(), commitMap)
	assert.NoError(err)
	err = db.GetContext(ctx, &now, `
		SELECT updated_ts
		FROM appliance_release_history
		WHERE stage = 'complete' AND appliance_uuid = $1`,
		appUU)
	assert.NoError(err)
	assert.Greater(int64(now.Sub(then)), int64(time.Microsecond))
}

func testReleaseStatus(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	// Build and insert three releases
	var releases []uuid.UUID
	commits := make(map[uuid.UUID]map[string]string)
	for i := 0; i < 3; i++ {
		rootRA, kernelRA, ramdiskRA := buildWRT(nil, 0)
		rootRA, err := ds.InsertArtifact(ctx, *rootRA)
		assert.NoError(err)
		kernelRA, err = ds.InsertArtifact(ctx, *kernelRA)
		assert.NoError(err)
		ramdiskRA, err = ds.InsertArtifact(ctx, *ramdiskRA)
		assert.NoError(err)
		psRA := buildPS(nil, 0, "mt7623")
		psRA, err = ds.InsertArtifact(ctx, *psRA)
		assert.NoError(err)
		xsRA := buildXS(nil, 0, "mt7623")
		xsRA, err = ds.InsertArtifact(ctx, *xsRA)
		assert.NoError(err)
		artifacts := []*ReleaseArtifact{rootRA, kernelRA, ramdiskRA, psRA, xsRA}
		rel, err := ds.InsertRelease(ctx, artifacts, nil)
		assert.NoError(err)
		releases = append(releases, rel)
		commits[rel] = map[string]string{
			"PS":  hex.EncodeToString(psRA.Commit),
			"XS":  hex.EncodeToString(xsRA.Commit),
			"WRT": hex.EncodeToString(rootRA.Commit),
		}
	}

	// Register some appliances
	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)
	mkOrgSiteApp(t, ds, &testOrg2, &testSite2, &testID2)
	mkOrgSiteApp(t, ds, &testOrg3, &testSite3, &testID3)

	// Give them current releases
	err := ds.SetCurrentRelease(ctx, testID1.ApplianceUUID, releases[0], time.Now().UTC(),
		commits[releases[0]])
	assert.NoError(err)
	err = ds.SetCurrentRelease(ctx, testID2.ApplianceUUID, releases[0], time.Now().UTC(),
		commits[releases[0]])
	assert.NoError(err)
	err = ds.SetCurrentRelease(ctx, testID3.ApplianceUUID, releases[0], time.Now().UTC(),
		commits[releases[0]])
	assert.NoError(err)

	// Get the release status for all three appliances, explicitly, and make
	// sure we get back three rows, each indicating the correct current
	// release, and no target release.
	apps := []uuid.UUID{testID1.ApplianceUUID, testID2.ApplianceUUID, testID3.ApplianceUUID}
	status, err := ds.GetReleaseStatusByAppliances(ctx, apps)
	assert.NoError(err)
	assert.Len(status, 3)
	assert.True(status[testID1.ApplianceUUID].CurrentReleaseUUID.Valid)
	assert.Equal(releases[0], status[testID1.ApplianceUUID].CurrentReleaseUUID.UUID)
	assert.False(status[testID1.ApplianceUUID].TargetReleaseUUID.Valid)
	assert.EqualValues(commits[releases[0]], status[testID1.ApplianceUUID].Commits)
	assert.True(status[testID2.ApplianceUUID].CurrentReleaseUUID.Valid)
	assert.Equal(releases[0], status[testID2.ApplianceUUID].CurrentReleaseUUID.UUID)
	assert.False(status[testID2.ApplianceUUID].TargetReleaseUUID.Valid)
	assert.EqualValues(commits[releases[0]], status[testID2.ApplianceUUID].Commits)
	assert.True(status[testID3.ApplianceUUID].CurrentReleaseUUID.Valid)
	assert.Equal(releases[0], status[testID3.ApplianceUUID].CurrentReleaseUUID.UUID)
	assert.False(status[testID3.ApplianceUUID].TargetReleaseUUID.Valid)
	assert.EqualValues(commits[releases[0]], status[testID3.ApplianceUUID].Commits)

	// Get the release status for all three appliances, implicitly, and make
	// sure we get back three rows, each indicating the correct current
	// release, and no target release.
	apps = []uuid.UUID{}
	status, err = ds.GetReleaseStatusByAppliances(ctx, apps)
	assert.NoError(err)
	assert.Len(status, 3)
	assert.True(status[testID1.ApplianceUUID].CurrentReleaseUUID.Valid)
	assert.Equal(releases[0], status[testID1.ApplianceUUID].CurrentReleaseUUID.UUID)
	assert.False(status[testID1.ApplianceUUID].TargetReleaseUUID.Valid)
	assert.EqualValues(commits[releases[0]], status[testID1.ApplianceUUID].Commits)
	assert.True(status[testID2.ApplianceUUID].CurrentReleaseUUID.Valid)
	assert.Equal(releases[0], status[testID2.ApplianceUUID].CurrentReleaseUUID.UUID)
	assert.False(status[testID2.ApplianceUUID].TargetReleaseUUID.Valid)
	assert.EqualValues(commits[releases[0]], status[testID2.ApplianceUUID].Commits)
	assert.True(status[testID3.ApplianceUUID].CurrentReleaseUUID.Valid)
	assert.Equal(releases[0], status[testID3.ApplianceUUID].CurrentReleaseUUID.UUID)
	assert.False(status[testID3.ApplianceUUID].TargetReleaseUUID.Valid)
	assert.EqualValues(commits[releases[0]], status[testID3.ApplianceUUID].Commits)

	// Get the release status for one of the appliances, and make sure we
	// get back one rows, indicating the correct current release, and no
	// target release.
	apps = []uuid.UUID{testID2.ApplianceUUID}
	status, err = ds.GetReleaseStatusByAppliances(ctx, apps)
	assert.NoError(err)
	assert.Len(status, 1)
	assert.True(status[testID2.ApplianceUUID].CurrentReleaseUUID.Valid)
	assert.Equal(releases[0], status[testID2.ApplianceUUID].CurrentReleaseUUID.UUID)
	assert.False(status[testID2.ApplianceUUID].TargetReleaseUUID.Valid)

	// Update one of the appliances.  Make sure that the release status only
	// has one current release (rather than all elements of the history it
	// now has) and the one we expect.
	err = ds.SetCurrentRelease(ctx, testID3.ApplianceUUID, releases[1],
		time.Now().UTC(), commits[releases[1]])
	assert.NoError(err)
	apps = []uuid.UUID{testID3.ApplianceUUID}
	status, err = ds.GetReleaseStatusByAppliances(ctx, apps)
	assert.NoError(err)
	assert.Len(status, 1)
	assert.True(status[testID3.ApplianceUUID].CurrentReleaseUUID.Valid)
	assert.Equal(releases[1], status[testID3.ApplianceUUID].CurrentReleaseUUID.UUID)
	assert.False(status[testID3.ApplianceUUID].TargetReleaseUUID.Valid)
	assert.EqualValues(commits[releases[1]], status[testID3.ApplianceUUID].Commits)

	// Set target releases, make sure everything seems right.
	err = ds.SetTargetRelease(ctx, testID1.ApplianceUUID, releases[1])
	assert.NoError(err)
	err = ds.SetTargetRelease(ctx, testID2.ApplianceUUID, releases[1])
	assert.NoError(err)
	err = ds.SetTargetRelease(ctx, testID3.ApplianceUUID, releases[1])
	assert.NoError(err)
	apps = []uuid.UUID{}
	status, err = ds.GetReleaseStatusByAppliances(ctx, apps)
	assert.NoError(err)
	assert.Len(status, 3)
	assert.True(status[testID1.ApplianceUUID].CurrentReleaseUUID.Valid)
	assert.Equal(releases[0], status[testID1.ApplianceUUID].CurrentReleaseUUID.UUID)
	assert.True(status[testID1.ApplianceUUID].TargetReleaseUUID.Valid)
	assert.Equal(releases[1], status[testID1.ApplianceUUID].TargetReleaseUUID.UUID)
	assert.True(status[testID2.ApplianceUUID].CurrentReleaseUUID.Valid)
	assert.Equal(releases[0], status[testID2.ApplianceUUID].CurrentReleaseUUID.UUID)
	assert.True(status[testID2.ApplianceUUID].TargetReleaseUUID.Valid)
	assert.Equal(releases[1], status[testID2.ApplianceUUID].TargetReleaseUUID.UUID)
	assert.True(status[testID3.ApplianceUUID].CurrentReleaseUUID.Valid)
	assert.Equal(releases[1], status[testID3.ApplianceUUID].CurrentReleaseUUID.UUID)
	assert.True(status[testID3.ApplianceUUID].TargetReleaseUUID.Valid)
	assert.Equal(releases[1], status[testID3.ApplianceUUID].TargetReleaseUUID.UUID)

	// Update one of the appliances to something that's not a release.
	nonRelCommits := make(map[string]string)
	for k, v := range commits[releases[2]] {
		nonRelCommits[k] = v
	}
	nonRelCommits["PS"] = "d4db33f"
	err = ds.SetCurrentRelease(ctx, testID3.ApplianceUUID, uuid.Nil,
		time.Now().UTC(), nonRelCommits)
	assert.NoError(err)
	status, err = ds.GetReleaseStatusByAppliances(ctx, []uuid.UUID{testID3.ApplianceUUID})
	assert.NoError(err)
	assert.Len(status, 1)
	assert.True(status[testID3.ApplianceUUID].CurrentReleaseUUID.Valid)
	assert.Equal(uuid.Nil, status[testID3.ApplianceUUID].CurrentReleaseUUID.UUID)
	assert.EqualValues(nonRelCommits, status[testID3.ApplianceUUID].Commits)

	// Make sure setting an arbitrary stage returns an error.
	err = ds.SetUpgradeStage(ctx, testID3.ApplianceUUID, uuid.Nil, time.Now().UTC(), "junk", true, "")
	assert.Error(err)

	// Set the upgrade stage to a valid value.
	err = ds.SetUpgradeStage(ctx, testID3.ApplianceUUID, uuid.Nil, time.Now().UTC(), "notified", true, "")
	assert.NoError(err)

	// Setting the upgrade stage for an unknown appliance should fail.
	err = ds.SetUpgradeStage(ctx, badUUID, uuid.Nil, time.Now().UTC(), "notified", true, "")
	assert.IsType(ForeignKeyError{}, err, "%+v", err)
	assert.Equal("appliance_release_history_appliance_uuid_fkey", err.(ForeignKeyError).Constraint)

	// Setting the upgrade stage with an unknown release should fail.
	err = ds.SetUpgradeStage(ctx, testID3.ApplianceUUID, badUUID, time.Now().UTC(), "notified", true, "")
	assert.IsType(ForeignKeyError{}, err, "%+v", err)
	assert.Equal("appliance_release_history_release_uuid_fkey", err.(ForeignKeyError).Constraint)
}

func TestFilterSlice(t *testing.T) {
	u := func(i int) uuid.UUID {
		if i > 255 {
			panic("Can't make uuid > 255")
		}
		b := make([]byte, 16)
		b[15] = byte(i)
		return uuid.Must(uuid.FromBytes(b))
	}

	// All true should filter out nothing
	r := []*Release{
		{UUID: u(0), OnePlatform: true},
		{UUID: u(1), OnePlatform: true},
		{UUID: u(2), OnePlatform: true},
	}

	platCheck := func(i int) bool {
		return !r[i].OnePlatform
	}

	assert := require.New(t)
	bad := filterSlice(&r, platCheck)
	assert.Len(bad, 0)
	assert.Len(r, 3)

	// Corner case: filter out first
	r = []*Release{
		{UUID: u(0), OnePlatform: false},
		{UUID: u(1), OnePlatform: true},
		{UUID: u(2), OnePlatform: true},
	}

	bad = filterSlice(&r, platCheck)
	assert.Len(bad, 1)
	assert.Equal(u(0), bad[0].UUID)
	assert.Len(r, 2)
	assert.ElementsMatch([]uuid.UUID{u(1), u(2)}, []uuid.UUID{r[0].UUID, r[1].UUID})

	// Corner case: filter out last
	r = []*Release{
		{UUID: u(0), OnePlatform: true},
		{UUID: u(1), OnePlatform: true},
		{UUID: u(2), OnePlatform: false},
	}

	bad = filterSlice(&r, platCheck)
	assert.Len(bad, 1)
	assert.Equal(u(2), bad[0].UUID)
	assert.Len(r, 2)
	assert.ElementsMatch([]uuid.UUID{u(0), u(1)}, []uuid.UUID{r[0].UUID, r[1].UUID})

	// All false should filter out everything.
	r = []*Release{
		{UUID: u(0), OnePlatform: false},
		{UUID: u(1), OnePlatform: false},
		{UUID: u(2), OnePlatform: false},
	}

	bad = filterSlice(&r, platCheck)
	assert.Len(bad, 3)
	assert.Len(r, 0)

	// Make sure two consecutive bad releases get removed.
	r = []*Release{
		{UUID: u(0), OnePlatform: true},
		{UUID: u(1), OnePlatform: false},
		{UUID: u(2), OnePlatform: false},
		{UUID: u(3), OnePlatform: true},
	}

	bad = filterSlice(&r, platCheck)
	assert.Len(bad, 2)
	assert.ElementsMatch([]uuid.UUID{u(1), u(2)}, []uuid.UUID{bad[0].UUID, bad[1].UUID})
	assert.Len(r, 2)
	assert.ElementsMatch([]uuid.UUID{u(0), u(3)}, []uuid.UUID{r[0].UUID, r[1].UUID})
}
