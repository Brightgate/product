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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

type localReadOpener struct {
	path string
}

func (lro localReadOpener) Open() (io.ReadCloser, error) {
	return os.Open(lro.path)
}

func (lro localReadOpener) String() string {
	return lro.path
}

func localArtifacts(prefix, platform string, commits map[string]string) ([]artifact, error) {
	if _, ok := artifactGlobs[platform]; !ok {
		return nil, fmt.Errorf("Unrecognized platform %q", platform)
	}

	platPath := filepath.Join(prefix, platform)
	fi, err := os.Stat(platPath)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", platPath)
	}

	artifacts := []artifact{}
	for repo, commit := range commits {
		repoPath := filepath.Join(platPath, repo)
		fi, err = os.Stat(repoPath)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", repoPath)
		}

		commitPath := filepath.Join(repoPath, commit)
		fi, err = os.Stat(commitPath)
		var hash, hashgen string
		var gen int
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
			globs, err := filepath.Glob(commitPath + "*")
			if err != nil {
				return nil, err
			}
			if len(globs) == 0 {
				return nil, fmt.Errorf("There are no directories that start with %s", commitPath)
			}

			// Iterate over the matches.  If there is more than one
			// hash, that's an error.  If there is one hash with
			// multiple versions, take the highest number.
			hashGenerations := make(map[string]int)
			for _, globMatch := range globs {
				matches := commitHashRE.FindStringSubmatch(globMatch)
				if matches == nil {
					continue
				}
				hash := matches[1]
				genStr := matches[2]
				var gen int
				if genStr != "" {
					gen, err = strconv.Atoi(genStr[1:])
					if err != nil {
						continue
					}
				}
				hashGenerations[hash] = max(gen, hashGenerations[hash])
			}
			if len(hashGenerations) == 0 {
				return nil, fmt.Errorf("There are no directories that start with %s", commitPath)
			} else if len(hashGenerations) > 1 {
				return nil, fmt.Errorf("Commit ID %s is ambiguous for repo %s", commit, repo)
			}
			for hash, gen = range hashGenerations {
			}
			hashgen = hash
			if gen > 0 {
				hashgen += "-" + strconv.Itoa(gen)
			}
			commitPath = filepath.Join(repoPath, hashgen)
			fi, err = os.Stat(commitPath)
			if err != nil {
				return nil, err
			}
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", commitPath)
		}

		for _, pattern := range artifactGlobs[platform][repo] {
			artifactPattern := filepath.Join(commitPath, pattern)
			globs, err := filepath.Glob(artifactPattern)
			if err != nil {
				panic(err)
			}
			if len(globs) == 0 {
				return nil, fmt.Errorf("No artifacts match pattern %s", artifactPattern)
			} else if len(globs) > 1 {
				return nil, fmt.Errorf("More than one artifact matches pattern %s", artifactPattern)
			}
			opener := localReadOpener{globs[0]}
			artifacts = append(artifacts, artifact{
				readOpener: opener,
				repo:       repo,
				commit:     hash,
				generation: gen,
				platform:   platform,
				filename:   filepath.Base(globs[0]),
			})
		}
	}

	return artifacts, nil
}
