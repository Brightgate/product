/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"bg/ap_common/platform"
	"bg/cloud_rpc"
	"bg/common/mfg"
	"bg/common/release"

	"github.com/pkg/errors"
	"github.com/satori/uuid"

	"github.com/golang/protobuf/ptypes"
	"github.com/spf13/afero"
)

var (
	errSameRelease = errors.New("Target release matches current release")
)

// If it looks like we're running with a self-assigned serial number, notify
// the cloud.
func validateNodeID(ctx context.Context, tclient cloud_rpc.EventClient,
	nodeID string) {

	serial, err := mfg.NewExtSerialFromString(nodeID)
	if err == nil {
		if mfg.IsExtSerialRandom(serial) {
			exc := &cloud_rpc.SerialException{
				Timestamp:    ptypes.TimestampNow(),
				SerialNumber: nodeID,
			}
			err := publishEvent(ctx, tclient, "exception", exc)
			if err != nil {
				slog.Warnf("failed to publish %v", exc)
			}
		}
	} else {
		slog.Warnf("failed to parse nodeID %s: %v", nodeID, err)
	}
}

func upgradeLoop(ctx context.Context, client cloud_rpc.ReleaseManagerClient,
	tclient cloud_rpc.EventClient, wg *sync.WaitGroup, doneChan chan bool) {
	var done bool

	curRelUU, err := getCurrentRelease()
	if err != nil {
		slog.Warnf("Couldn't determine current release UUID: %v", err)
	}

	commitMap := getCurrentCommits()

	if err = reportRelease(ctx, tclient, curRelUU, commitMap); err != nil {
		slog.Errorf("Unable to report release information: %v", err)
	} else {
		slog.Infof("Reported %s (%v) as the currently running release",
			curRelUU, commitMap)
	}

	releaseChan := make(chan struct{})
	nodeID, err := plat.GetNodeID()
	if err != nil {
		slog.Errorf("Unable to determine node ID; cannot detect "+
			"upgrade requests: %v", err)
		<-doneChan
		wg.Done()
		return
	}
	validateNodeID(ctx, tclient, nodeID)

	targetPath := fmt.Sprintf("^@/nodes/%s/target_release", nodeID)
	config.HandleChange(targetPath, func(path []string, val string, expires *time.Time) {
		releaseChan <- struct{}{}
	})

	slog.Infof("upgrade loop starting")

	for !done {
		select {
		case done = <-doneChan:
		case <-releaseChan:
			slog.Info("Got signal to upgrade")

			if err = doUpgradeAndReport(ctx, client, tclient, curRelUU); err != nil {
				if err == errSameRelease {
					slog.Warn(err)
					continue
				}
				slog.Error(err)
			}
		}
	}

	slog.Infof("upgrade loop done")
	wg.Done()
}

func fetchReleaseDescriptor(ctx context.Context, client cloud_rpc.ReleaseManagerClient) (
	*cloud_rpc.ReleaseResponse, error) {
	ctx, err := applianceCred.MakeGRPCContext(ctx)
	if err != nil {
		slog.Fatalf("Failed to make GRPC credential: %+v", err)
	}

	clientDeadline := time.Now().Add(*rpcDeadline)
	ctx, ctxcancel := context.WithDeadline(ctx, clientDeadline)
	defer ctxcancel()

	return client.FetchDescriptor(ctx, &cloud_rpc.ReleaseRequest{})
}

// Returns the UUID of the currently running release from the JSON file left
// behind by the upgrade (if it can be found).
func getCurrentRelease() (*uuid.UUID, error) {
	crjPath := plat.ExpandDirPath(platform.APPackage, "etc", "release.json")
	crjBytes, err := ioutil.ReadFile(crjPath)
	if err != nil {
		slog.Errorf("Unable to read currently-running release descriptor at %s: %v", crjPath, err)
		return nil, err
	}

	cr, err := unmarshalRelease(string(crjBytes))
	if err != nil {
		slog.Errorf("Unable to unmarshal JSON from %s: %v", crjPath, err)
		return nil, err
	}

	return &cr.Release.UUID, nil
}

// Figures out as best it can what commits of what repos are running on the
// system.  Not all repos deliver this information in any form, and not all
// repos deliver the full commit hash.
func getCurrentCommits() map[string]string {
	commitMap := make(map[string]string)

	apversion, err := config.GetProp("@/apversion")
	if err == nil {
		commitMap["PS"] = apversion
	}

	// Read the WRT commit hash out of /etc/openwrt_version.  By default,
	// it's got more information than that; this allows for it to be just
	// the commit hash, in case we override it in our build.
	if src, err := ioutil.ReadFile("/etc/openwrt_version"); err == nil {
		src = bytes.TrimSpace(src)
		b := make([]byte, hex.DecodedLen(len(src)))
		if _, err := hex.Decode(b, src); err == nil {
			commitMap["WRT"] = string(src)
		} else {
			pat := regexp.MustCompile(`^r.*-([[:xdigit:]]*)$`)
			if matches := pat.FindSubmatch(src); len(matches) == 2 {
				commitMap["WRT"] = string(matches[1])
			}
		}
	}

	getCurrentCommitsPkgs(afero.NewOsFs(), commitMap)

	// Pull version information for VUB, once it's available.

	return commitMap
}

func getCurrentCommitsPkgs(fs afero.Fs, commitMap map[string]string) {
	verDirPath := plat.ExpandDirPath(platform.APRoot, "etc", "bg-versions.d")
	verDir, err := fs.Open(verDirPath)
	if err != nil {
		return
	}

	names, err := verDir.Readdirnames(0)
	if err != nil {
		return
	}

	// repoMap maps repo names to a set of commit IDs
	repoMap := make(map[string]map[string]bool)

	// pkgMap maps repo names to a map of repo and package
	// names to commit IDs
	pkgMap := make(map[string]map[string]string)

	for _, name := range names {
		path := filepath.Join(verDirPath, name)
		commit, err := afero.ReadFile(fs, path)
		if err != nil {
			continue
		}

		commitStr := string(bytes.TrimSpace(commit))
		spl := strings.SplitN(name, ":", 2)
		if len(spl) == 1 {
			commitMap[name] = commitStr
		} else {
			repo := spl[0]
			if _, ok := repoMap[repo]; !ok {
				repoMap[repo] = make(map[string]bool)
				pkgMap[repo] = make(map[string]string)
			}
			repoMap[repo][commitStr] = true
			pkg := spl[1]
			// Split the package into name and version; only report
			// the name
			pkgSpl := strings.SplitN(pkg, "_", 2)
			pkgMap[repo][pkgSpl[0]] = commitStr
		}
	}

	for repo := range repoMap {
		if len(repoMap[repo]) == 1 {
			for commit := range repoMap[repo] {
				commitMap[repo] = commit
			}
		} else {
			for pkg, commit := range pkgMap[repo] {
				commitMap[repo+":"+pkg] = commit
			}
		}
	}

	return
}

// Reports to the cloud what we know about the currently-running release.
func reportRelease(ctx context.Context, tclient cloud_rpc.EventClient,
	relUU *uuid.UUID, commitMap map[string]string) error {
	report := &cloud_rpc.UpgradeReport{
		Result:     cloud_rpc.UpgradeReport_REPORT,
		RecordTime: ptypes.TimestampNow(),
		Commits:    commitMap,
	}
	if relUU != nil {
		report.ReleaseUuid = relUU.String()
	}
	return publishEvent(ctx, tclient, "upgrade", report)
}

func indentReleaseJSON(desc string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(desc), "", "  "); err != nil {
		return desc
	}
	return buf.String()
}

func unmarshalRelease(desc string) (release.Release, error) {
	var rel release.Release
	if err := json.Unmarshal([]byte(desc), &rel); err != nil {
		return rel, err
	}
	return rel, nil
}

func cleanupOldArtifacts(targetRelease string, curRelUU *uuid.UUID) {
	// Cleanup old artifact directories: anything that's not the target or
	// currently running release.  Log, but otherwise ignore, any errors.
	dir := plat.ExpandDirPath(platform.APData, "release")
	f, err := os.Open(dir)
	if err != nil {
		slog.Errorf("Couldn't open %s to remove old downloads: %v", dir, err)
	} else {
		names, err := f.Readdirnames(0)
		if err != nil {
			slog.Errorf("Failed to read %s: %v", dir, err)
		} else {
			for _, name := range names {
				if name == targetRelease ||
					(curRelUU != nil && name == curRelUU.String()) {
					continue
				}
				rdir := filepath.Join(dir, name)
				if err = os.RemoveAll(rdir); err != nil {
					slog.Errorf("Failed to remove %s: %v", rdir, err)
				} else {
					slog.Infof("Removed old download directory %s", rdir)
				}
			}
		}
	}

}

func doUpgrade(ctx context.Context, client cloud_rpc.ReleaseManagerClient,
	curRelUU *uuid.UUID) (string, []byte, error) {
	resp, err := fetchReleaseDescriptor(ctx, client)
	if err != nil {
		return "", nil, errors.Wrap(err, "Failed to fetch release descriptor")
	}

	rel, err := unmarshalRelease(resp.Release)
	if err != nil {
		return "", nil, errors.Wrap(err, "Failed to unmarshal release descriptor")
	}

	targetRelease := rel.Release.UUID.String()
	slog.Infof("Target release is %s", targetRelease)

	// If we're already running this release, bail out early
	if curRelUU != nil && targetRelease == curRelUU.String() {
		return targetRelease, nil, errSameRelease
	}

	dir := plat.ExpandDirPath(platform.APData, "release", targetRelease)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return targetRelease, nil, errors.Wrapf(err, "Failed to create release download directory %s", dir)
	}
	prettyBytes := indentReleaseJSON(resp.Release)
	err = ioutil.WriteFile(filepath.Join(dir, "release.json"), []byte(prettyBytes), 0644)
	if err != nil {
		return targetRelease, nil, errors.Wrapf(err, "Failed to write release descriptor")
	}

	// Double-check that the release matches the running platform.
	if rel.Platform != plat.GetPlatform() {
		return targetRelease, nil, errors.Errorf("Release %s is for platform %s, not %s",
			targetRelease, rel.Platform, plat.GetPlatform())
	}

	err = fetchArtifacts(ctx, rel, dir)
	if err != nil {
		return targetRelease, nil, errors.Wrapf(err, "Failed to fetch release artifacts")
	}

	out, err := plat.Upgrade(rel)
	return targetRelease, out, err
}

func doUpgradeAndReport(ctx context.Context, client cloud_rpc.ReleaseManagerClient,
	tclient cloud_rpc.EventClient, curRelUU *uuid.UUID) error {

	targetRelease, out, err := doUpgrade(ctx, client, curRelUU)

	var errText string
	if err != nil {
		errText = err.Error()
	}
	report := &cloud_rpc.UpgradeReport{
		RecordTime:  ptypes.TimestampNow(),
		ReleaseUuid: targetRelease,
		Output:      out,
		Error:       errText,
	}
	if err != nil {
		report.Result = cloud_rpc.UpgradeReport_FAILURE
	} else {
		report.Result = cloud_rpc.UpgradeReport_SUCCESS
	}
	pubErr := publishEvent(ctx, tclient, "upgrade", report)
	if pubErr != nil {
		slog.Errorf("Failed to publish upgrade failure event: %v",
			pubErr)
	}
	if out != nil {
		slog.Infof("Upgrade output:\n%s", string(out))
	}
	if err != nil {
		return errors.Wrap(err, "Failed to upgrade")
	}

	cleanupOldArtifacts(targetRelease, curRelUU)

	if os.Getenv("APROOT") == "/" {
		slog.Info("Upgrade complete; rebooting")
		mcpd.Reboot()
	} else {
		// It'd be nice if we could tell mcp to shut everything down and
		// restart itself (and then everything else), and that "reboot"
		// did that in this configuration.
		slog.Info("Upgrade complete; test mode means no reboot")
	}

	return nil
}

func fetchArtifacts(ctx context.Context, rel release.Release, dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.Wrapf(err, "failed to make directory %s", dir)
	}

	// Not all artifact filenames are the same as the installer expects
	// them, so map them to the expected names.  A missing entry means
	// they're the same, and a map to the empty string means it's not used.
	m := map[string]string{
		"root.squashfs":      "SQUASHFS",
		"uImage.itb":         "KERNEL",
		"uImage-ramdisk.itb": "",
		"u-boot-mtk.bin":     "UBOOT",
	}

	// XXX resume downloads when tmpfile already exists?
	// XXX Maybe link artifacts from already-downloaded release?
	for _, artifact := range rel.Artifacts {
		u, _ := url.Parse(artifact.URL)
		filename := filepath.Base(u.Path)
		dlFilename, ok := m[filename]
		if dlFilename == "" {
			if ok {
				slog.Infof("Skipping artifact %s", artifact.URL)
				continue
			} else {
				dlFilename = filename
			}
		}

		var h hash.Hash
		switch artifact.HashType {
		case "SHA256":
			h = sha256.New()
		default:
			return fmt.Errorf("Unknown hash type %q", artifact.HashType)
		}

		dlPathnameTmp := filepath.Join(dir, dlFilename+".tmp")
		slog.Infof("Downloading %s to %s", artifact.URL, dlPathnameTmp)
		f, err := os.Create(dlPathnameTmp)
		if err != nil {
			return errors.Wrapf(err, "failed to create artifact file at %s",
				dlPathnameTmp)
		}

		mw := io.MultiWriter(h, f)

		resp, err := http.Get(artifact.URL)
		if err != nil {
			f.Close()
			return errors.Wrapf(err, "failed to get artifact from URL %s",
				artifact.URL)
		}

		if _, err = io.Copy(mw, resp.Body); err != nil {
			f.Close()
			resp.Body.Close()
			return errors.Wrapf(err, "failed to download artifact from URL %s",
				artifact.URL)
		}
		f.Close()
		resp.Body.Close()

		hexHash := hex.EncodeToString(h.Sum(nil))
		if hexHash != artifact.Hash {
			// Signed URLs are really long due to a lot of lengthy
			// query parameters.  We could trim the query off, since
			// it normally shouldn't be interesting for diagnostic
			// purposes, but in case there's a problem with the
			// signing, it might be useful.
			return fmt.Errorf("%q hash of %q (%q) is %s, should be %s",
				artifact.HashType, artifact.URL, f.Name(), hexHash, artifact.Hash)
		}

		dlPathname := filepath.Join(dir, dlFilename)
		if err = os.Rename(dlPathnameTmp, dlPathname); err != nil {
			return errors.Wrapf(err, "failed to rename artifact temporary file %s",
				dlPathnameTmp)
		}
	}

	return nil
}

