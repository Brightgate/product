/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package release

import (
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/satori/uuid"
)

// Artifact is the data about an artifact necessary for an appliance to
// retrieve, verify, and install it.
type Artifact struct {
	URL      string `json:"url"`
	Hash     string `json:"hash"`
	HashType string `json:"hash_type"`
}

// UUName objects are tuples of UUID and name.
type UUName struct {
	UUID uuid.UUID `json:"uuid"`
	Name string    `json:"name"`
}

// Release objects represent the release descriptor object as consumed (in JSON
// format) by ap-factory.  We could do this as a map instead, but a struct gives
// us control over the field ordering.
type Release struct {
	Release   UUName            `json:"release"`
	Platform  string            `json:"platform"`
	Artifacts []Artifact        `json:"artifacts"`
	Metadata  map[string]string `json:"metadata"`
}

// FilenameByPattern returns a slice of strings representing the artifacts for a
// particular release whose filenames match a pattern.
func (r Release) FilenameByPattern(pattern string) []string {
	paths := make([]string, 0)
	for _, artifact := range r.Artifacts {
		u, err := url.Parse(artifact.URL)
		if err != nil {
			panic(err)
		}
		matches, err := filepath.Match(pattern, path.Base(u.Path))
		if err != nil {
			// Errors only if the pattern is malformed
			panic(err)
		}
		if matches {
			paths = append(paths, path.Base(u.Path))
		}
	}
	return paths
}

// SortArtifacts sorts the Artifacts slice in a Release object in topological
// order (as necessary) suitable for installation.
func (r Release) SortArtifacts() {
	sort.Slice(r.Artifacts, func(i, j int) bool {
		iURL, err := url.Parse(r.Artifacts[i].URL)
		if err != nil {
			return false
		}
		jURL, err := url.Parse(r.Artifacts[j].URL)
		if err != nil {
			return false
		}
		// We assume that there are no inter-dependencies between our
		// packages, other than bg-appliance depends on all the others.
		if strings.HasPrefix(path.Base(iURL.Path), "bg-appliance_") &&
			!strings.HasPrefix(path.Base(jURL.Path), "bg-appliance_") {
			return false
		} else if strings.HasPrefix(path.Base(jURL.Path), "bg-appliance_") &&
			!strings.HasPrefix(path.Base(iURL.Path), "bg-appliance_") {
			return true
		}
		return false
	})
}

