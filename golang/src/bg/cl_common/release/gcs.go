/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package release

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

type gcsReadOpener struct {
	object *storage.ObjectHandle
}

func (gro gcsReadOpener) Open() (io.ReadCloser, error) {
	return gro.object.NewReader(context.TODO())
}

func (gro gcsReadOpener) String() string {
	// XXX This probably should be the full URL
	return gro.object.ObjectName()
}

func gcsArtifacts(ctx context.Context, prefix, platform string, commits map[string]string) (
	[]artifact, func(), error) {
	closer := func() {}
	if _, ok := artifactGlobs[platform]; !ok {
		return nil, closer, fmt.Errorf("Unrecognized platform %q", platform)
	}

	credFile := os.Getenv("B10E_CLRELEASE_GCS_CREDFILE")
	if credFile == "" {
		return nil, closer, fmt.Errorf("No credentials file set: put path in $B10E_CLRELEASE_GCS_CREDFILE")
	}

	client, err := storage.NewClient(ctx, option.WithCredentialsFile(credFile))
	if err != nil {
		return nil, closer, err
	}
	// We can't close the client in this function, or the openers we return
	// will segfault due to members of the client being nil.
	closer = func() { client.Close() }

	prefixURL, err := url.Parse(prefix)
	if err != nil {
		return nil, closer, err
	}
	if prefixURL.Scheme != "gs" {
		return nil, closer, fmt.Errorf("GCS prefix scheme must be 'gs'")
	}

	bucketName := prefixURL.Hostname()
	prefixPath := prefixURL.Path
	bucket := client.Bucket(bucketName)

	platPath := path.Join(prefixPath, platform)

	artifacts := []artifact{}
	for repo, commit := range commits {
		repoPath := path.Join(platPath, repo)
		commitPath := path.Join(repoPath, commit)

		hashGenerations := make(map[string]int)
		oi := bucket.Objects(ctx, &storage.Query{Delimiter: "/", Prefix: commitPath})
		for {
			objAttrs, err := oi.Next()
			if err == iterator.Done {
				break
			} else if err != nil {
				return nil, closer, err
			}
			matches := commitHashRE.FindStringSubmatch(objAttrs.Prefix)
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
			return nil, closer, fmt.Errorf("There are no directories that start with %s", commitPath)
		} else if len(hashGenerations) > 1 {
			return nil, closer, fmt.Errorf("Commit ID %s is ambiguous for repo %s", commit, repo)
		}
		var hash string
		var gen int
		for hash, gen = range hashGenerations {
		}
		hashgen := hash
		if gen > 0 {
			hashgen += "-" + strconv.Itoa(gen)
		}
		commitPath = path.Join(repoPath, hashgen)

		for _, pattern := range artifactGlobs[platform][repo] {
			artifactPattern := path.Join(commitPath, pattern)
			globs := []string{}
			oi = bucket.Objects(ctx, &storage.Query{Delimiter: "", Prefix: commitPath})
			for {
				objAttrs, err := oi.Next()
				if err == iterator.Done {
					break
				} else if err != nil {
					return nil, closer, err
				}
				matches, err := filepath.Match(artifactPattern, objAttrs.Name)
				if err != nil {
					panic(err)
				}
				if matches {
					globs = append(globs, objAttrs.Name)
				}
			}
			if len(globs) == 0 {
				return nil, closer, fmt.Errorf("No artifacts match pattern %s", artifactPattern)
			} else if len(globs) > 1 {
				return nil, closer, fmt.Errorf("More than one artifact matches pattern %s", artifactPattern)
			}
			opener := gcsReadOpener{bucket.Object(globs[0])}
			artifacts = append(artifacts, artifact{
				readOpener: opener,
				repo:       repo,
				commit:     hash,
				generation: gen,
				platform:   platform,
				filename:   path.Base(globs[0]),
			})
		}
	}

	return artifacts, closer, nil
}

