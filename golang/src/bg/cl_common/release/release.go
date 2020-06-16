/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"bg/cloud_models/appliancedb"
	"bg/common/release"
)

const (
	urlScheme = "gs"
	urlBase   = "bg-appliance-artifacts"

	// DefaultArtifactPrefix is the expected default URL prefix for artifact
	// objects.
	DefaultArtifactPrefix = urlScheme + "://" + urlBase
)

var (
	commitHashRE = regexp.MustCompile(`/([[:xdigit:]]{40})(-\d*)?`)

	artifactGlobs = map[string]map[string][]string{
		"mt7623": {
			"PS":  {"bg-appliance_*_arm_cortex-a7_neon-vfpv4.ipk"},
			"VUB": {"u-boot-mtk.bin"},
			"WRT": {"root.squashfs", "uImage-ramdisk.itb", "uImage.itb"},
			"XS": {
				"bg-hostapd_*_arm_cortex-a7_neon-vfpv4.ipk",
				"bg-rdpscan_*_arm_cortex-a7_neon-vfpv4.ipk",
			},
		},
		"rpi3": {
			"PS": {"bg-appliance_*_armhf.deb"},
			"XS": {"bg-hostapd_*_armhf.deb"},
		},
		"x86": {
			"PS": {"bg-appliance_*_amd64.deb"},
			"XS": {"bg-hostapd_*_amd64.deb"},
		},
	}
)

type readOpener interface {
	Open() (io.ReadCloser, error)
}

type artifact struct {
	readOpener
	repo       string
	commit     string
	generation int
	platform   string
	filename   string
}

func max(i, j int) int {
	if i > j {
		return i
	}
	return j
}

// CreateRelease populates the database with the necessary information to define
// a release, returning the Release object representing that information.
func CreateRelease(db appliancedb.DataStore, prefix, platform, name string, repoCommits map[string]string) (*release.Release, error) {
	ctx := context.Background()
	var artifacts []artifact
	var err error
	closer := func() {}
	if strings.HasPrefix(prefix, "gs://") {
		artifacts, closer, err = gcsArtifacts(ctx, prefix, platform, repoCommits)
	} else if strings.HasPrefix(prefix, "https://") || strings.HasPrefix(prefix, "http://") {
		return nil, fmt.Errorf("HTTP(S) URLs not supported")
	} else {
		artifacts, err = localArtifacts(prefix, platform, repoCommits)
	}
	defer closer()
	if err != nil {
		return nil, err
	}

	dbArtifacts := make([]*appliancedb.ReleaseArtifact, len(artifacts))
	for i, a := range artifacts {
		r, err := a.Open()
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		if _, err := io.Copy(h, r); err != nil {
			r.Close()
			return nil, err
		}
		r.Close()

		commitBytes, err := hex.DecodeString(a.commit)
		if err != nil {
			return nil, err
		}
		ra := appliancedb.ReleaseArtifact{
			Repo:       a.repo,
			Commit:     commitBytes,
			Generation: a.generation,
			Platform:   a.platform,
			Filename:   a.filename,
			HashType:   "SHA256",
			Hash:       h.Sum(nil),
		}
		na, err := db.InsertArtifact(ctx, ra)
		switch err.(type) {
		case nil, appliancedb.UniqueViolationError:
			break
		default:
			return nil, err
		}
		dbArtifacts[i] = na
	}

	metadata := map[string]string{
		"name": name,
	}
	relUU, err := db.InsertRelease(ctx, dbArtifacts, metadata)
	if err != nil {
		return nil, err
	}
	dbRel, err := db.GetRelease(ctx, relUU)
	if err != nil {
		return nil, err
	}

	release := FromDBRelease(dbRel)
	return &release, nil
}

// FromDBRelease returns an object with the specific information needed by the
// appliance to tell it how to upgrade.
func FromDBRelease(r *appliancedb.Release) release.Release {
	artArr := make([]release.Artifact, len(r.Commits))
	for i, art := range r.Commits {
		commithashgen := hex.EncodeToString(art.Commit)
		if art.Generation > 0 {
			commithashgen += "-" + strconv.Itoa(art.Generation)
		}
		artURL := url.URL{
			Scheme: urlScheme,
			Path: path.Join(urlBase, r.Platform, art.Repo,
				commithashgen, art.Filename),
		}
		artArr[i] = release.Artifact{
			URL:      artURL.String(),
			Hash:     hex.EncodeToString(art.Hash),
			HashType: art.HashType,
		}
	}

	rel := release.Release{
		Release: release.UUName{
			UUID: r.UUID,
			Name: r.Metadata["name"],
		},
		Platform:  r.Platform,
		Artifacts: artArr,
		Metadata:  map[string]string(r.Metadata),
	}
	rel.SortArtifacts()
	return rel
}
