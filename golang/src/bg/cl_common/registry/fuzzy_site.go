/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package registry

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"bg/cl_common/pgutils"
	"bg/cloud_models/appliancedb"

	"github.com/dhduvall/closestmatch"
	uuidmod "github.com/satori/uuid"
	"github.com/tatsushid/go-prettytable"
)

type orgSiteUUID struct {
	OrganizationName string
	SiteName         string
	SiteUUID         uuidmod.UUID
}
type orgSiteUUIDSlice []orgSiteUUID

func (s orgSiteUUIDSlice) Len() int {
	return len(s)
}

func (s orgSiteUUIDSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s orgSiteUUIDSlice) Less(i, j int) bool {
	// XXX Sort by organization first, or not at all?
	if s[i].OrganizationName == s[j].OrganizationName {
		return s[i].SiteName < s[j].SiteName
	}
	return s[i].OrganizationName < s[j].OrganizationName
}

// AmbiguousSiteError encapsulates the sites that might be referred to by an
// input string.
type AmbiguousSiteError struct {
	input   string
	sites   orgSiteUUIDSlice
	oneName bool
}

func (e AmbiguousSiteError) Error() string {
	return fmt.Sprintf("There are %d site name matches for %q",
		len(e.sites), e.input)
}

// Pretty returns a string which is the normal error output plus a table of the
// possible site matches.
func (e AmbiguousSiteError) Pretty() string {
	b := new(strings.Builder)
	fmt.Fprintf(b, "%s:\n", e.Error())
	table, _ := prettytable.NewTable(
		prettytable.Column{Header: "Site"},
		prettytable.Column{Header: "Organization"},
		prettytable.Column{Header: "UUID"},
	)
	table.Separator = "  "

	sort.Sort(e.sites)
	for _, site := range e.sites {
		table.AddRow(site.SiteName, site.OrganizationName, site.SiteUUID)
	}
	table.WriteTo(b)
	return b.String()
}

// Find the names of the organizations identified by the UUIDs in orgUUIDs.
// This could be done in a single database call, if it existed.
func orgNamesByUUIDs(ctx context.Context, db appliancedb.DataStore, orgUUIDs []uuidmod.UUID) (map[uuidmod.UUID]string, error) {
	orgNames := make(map[uuidmod.UUID]string, 0)
	for _, uuid := range orgUUIDs {
		if _, ok := orgNames[uuid]; ok {
			continue
		}
		org, err := db.OrganizationByUUID(ctx, uuid)
		if err != nil {
			return nil, err
		}
		orgNames[uuid] = org.Name
	}
	return orgNames, nil
}

// Create an AmbiguousSiteError from a list of site UUIDs and a mapping of site
// UUIDs to CustomerSite objects (which have the names and organization UUIDs).
func makeASEFromSites(ctx context.Context, db appliancedb.DataStore, input string, oneName bool,
	siteUUIDs []uuidmod.UUID, siteMap map[uuidmod.UUID]appliancedb.CustomerSite) AmbiguousSiteError {
	ase := AmbiguousSiteError{input: input, oneName: oneName}
	ase.sites = make([]orgSiteUUID, len(siteUUIDs))

	// Create an array of the organization UUIDs matching siteUUIDs.
	orgUUIDs := make([]uuidmod.UUID, len(siteUUIDs))
	for i, uuid := range siteUUIDs {
		orgUUIDs[i] = siteMap[uuid].OrganizationUUID
	}

	// Now go to the DB and grab the names for those UUIDs.  If that fails,
	// just substitute the UUID strings instead.
	orgNames, err := orgNamesByUUIDs(ctx, db, orgUUIDs)
	if err != nil {
		for _, uuid := range siteUUIDs {
			ou := siteMap[uuid].OrganizationUUID
			orgNames[ou] = ou.String()
		}
	}

	// Fill in the error struct.
	for i, uuid := range siteUUIDs {
		ase.sites[i] = orgSiteUUID{
			OrganizationName: orgNames[siteMap[uuid].OrganizationUUID],
			SiteName:         siteMap[uuid].Name,
			SiteUUID:         uuid,
		}
	}

	return ase
}

// FuzzyMatch is the return type of SiteUUIDByNameFuzzy(), and encapsulates the
// UUID of a site and its corresponding name.
type FuzzyMatch struct {
	SiteUUID uuidmod.UUID
	SiteName string
}

// Should be limited to a connection string or a DataStore.
type dataStoreOrString interface{}

// SiteUUIDByNameFuzzy takes an input string and tries to figure out what that
// might mean as a site.  If it's in the form of a valid UUID, it'll return the
// UUID.  If it's not, it'll look in the database and do a fuzzy match of the
// input against the site names.
//
// The dbURI parameter can be a database connection string, or it can be an
// existing database handle (appliancedb.DataStore).
func SiteUUIDByNameFuzzy(ctx context.Context, dbURI dataStoreOrString, input string) (*FuzzyMatch, error) {
	// If we're given an empty string, return a match indicating that.
	if input == "" {
		return &FuzzyMatch{}, nil
	}

	// If we're just given a UUID, return it.
	if uuid, err := uuidmod.FromString(input); err == nil {
		return &FuzzyMatch{uuid, ""}, nil
	}

	// Otherwise, get site names and do a fuzzy match.  First set up the
	// database connection (if needed) and grab the sites.
	var db appliancedb.DataStore
	switch dbTyped := dbURI.(type) {
	case string:
		var err error
		var dbURI string
		if dbURI, err = pgutils.PasswordPrompt(dbTyped); err != nil {
			return nil, err
		}
		if db, err = appliancedb.Connect(dbURI); err != nil {
			return nil, err
		}
	case appliancedb.DataStore:
		db = dbTyped
	default:
		panic(fmt.Sprintf("Unexpected type '%T' for dbURI in SiteUUIDByNameFuzzy()", dbURI))
	}
	sites, err := db.AllCustomerSites(ctx)
	if err != nil {
		return nil, err
	}

	// Build some new data structures from this data.  siteNames is the
	// corpus for the fuzzy matching routines; siteUUIDs lets us do quick
	// lookups of the site UUID from a name; and siteMap is used when
	// building ambiguous site errors, to fill in those data structures.
	siteNames := make([]string, len(sites))
	siteUUIDs := make(map[string][]uuidmod.UUID, len(sites))
	siteMap := make(map[uuidmod.UUID]appliancedb.CustomerSite, len(sites))
	for i, site := range sites {
		// XXX Do we want to split them up by dashes, remove other
		// non-word characters?
		siteNames[i] = site.Name
		siteUUIDs[site.Name] = append(siteUUIDs[site.Name], site.UUID)
		siteMap[site.UUID] = site
	}

	// If input matches a name exactly, and there is only one site with that
	// name, return it.  If there are more sites, return an error indicating
	// the ambiguity.
	if uuids, ok := siteUUIDs[input]; ok {
		if len(uuids) == 1 {
			// No name in the return value indicates the input was
			// an exact match.
			return &FuzzyMatch{uuids[0], ""}, nil
		}
		return nil, makeASEFromSites(ctx, db, input, true, uuids, siteMap)
	}

	// Do some fuzzy matching
	bagSizes := []int{2, 3, 4}
	cm := closestmatch.New(siteNames, bagSizes)
	words := cm.ClosestNRank(input, len(siteNames)/2)
	if len(words) == 0 {
		return nil, fmt.Errorf("Couldn't find any site name matches for %q",
			input)
	}

	// Run through the matches.  If there appears to be a cliff (one ranking
	// is at least 3x of the next), then offer the ones before the cliff.
	val := words[0].Value
	matches := []string{}
	for _, pair := range words {
		if val > 3*pair.Value {
			break
		}
		matches = append(matches, pair.Key)
	}

	// If there's just one reasonable match (and there's only one site with
	// that name), then return it, allowing the caller to warn the user that
	// it might still not be what the user is looking for.
	if len(matches) == 1 {
		if uuids, ok := siteUUIDs[matches[0]]; ok {
			if len(uuids) == 1 {
				return &FuzzyMatch{uuids[0], siteMap[uuids[0]].Name}, nil
			}
			return nil, makeASEFromSites(ctx, db, input, true, uuids, siteMap)
		}
	}

	// If there are multiple possible matches, punt and make the caller pass
	// in something more specific.
	uuids := make([]uuidmod.UUID, 0)
	for _, name := range matches {
		for _, uuid := range siteUUIDs[name] {
			uuids = append(uuids, uuid)
		}
	}
	return nil, makeASEFromSites(ctx, db, input, false, uuids, siteMap)
}
