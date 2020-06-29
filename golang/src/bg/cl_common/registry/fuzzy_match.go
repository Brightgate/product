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
	"os"
	"sort"
	"strconv"
	"strings"

	"bg/cl_common/pgutils"
	"bg/cloud_models/appliancedb"

	"github.com/dhduvall/closestmatch"
	uuidmod "github.com/satori/uuid"
	"github.com/tatsushid/go-prettytable"
)

// entity is an interface that lets us treat appliancedb.Organizations and
// appliancedb.CustomerSites interchangeably.
type entity interface {
	name() string
	uuid() uuidmod.UUID
}

// entityOrg is a new type copying Organization, allowing us to use it where an
// entity is called for.
type entityOrg appliancedb.Organization

func (e entityOrg) name() string {
	return e.Name
}

func (e entityOrg) uuid() uuidmod.UUID {
	return e.UUID
}

// entitySite is a new type copying CustomerSite, allowing us to use it where an
// entity is called for.
type entitySite appliancedb.CustomerSite

func (e entitySite) name() string {
	return e.Name
}

func (e entitySite) uuid() uuidmod.UUID {
	return e.UUID
}

// entityUUID is a generic structure mapping a name to a UUID, possibly with the
// name of a "parent" entity.
type entityUUID struct {
	Name       string
	UUID       uuidmod.UUID
	ParentName string
}
type entityUUIDSlice []entityUUID

func (s entityUUIDSlice) Len() int {
	return len(s)
}

func (s entityUUIDSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s entityUUIDSlice) Less(i, j int) bool {
	if s[i].ParentName == s[j].ParentName {
		return s[i].Name < s[j].Name
	}
	return s[i].Name < s[j].Name
}

// AmbiguousMatchError encapsulates the entities that might be referred to by an
// input string.
type AmbiguousMatchError struct {
	typ      string
	input    string
	entities entityUUIDSlice
	oneName  bool
}

func (e AmbiguousMatchError) Error() string {
	return fmt.Sprintf("There are %d %s name matches for %q",
		len(e.entities), e.typ, e.input)
}

// Pretty returns a string which is the normal error output plus a table of the
// possible matches matches.
func (e AmbiguousMatchError) Pretty() string {
	b := new(strings.Builder)
	fmt.Fprintf(b, "%s:\n", e.Error())
	var columns []prettytable.Column
	if e.typ == "site" {
		columns = append(columns, prettytable.Column{Header: "Site"})
	}
	columns = append(columns, prettytable.Column{Header: "Organization"})
	columns = append(columns, prettytable.Column{Header: "UUID"})
	table, _ := prettytable.NewTable(columns...)
	table.Separator = "  "

	sort.Sort(e.entities)
	for _, site := range e.entities {
		var cols []interface{}
		cols = append(cols, site.Name)
		if e.typ == "site" {
			cols = append(cols, site.ParentName)
		}
		cols = append(cols, site.UUID)
		table.AddRow(cols...)
	}
	table.WriteTo(b)
	return b.String()
}

// AmbiguousOrgError is a legacy type alias for AmbiguousMatchError.
type AmbiguousOrgError = AmbiguousMatchError

// AmbiguousSiteError is a legacy type alias for AmbiguousMatchError.
type AmbiguousSiteError = AmbiguousMatchError

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

// Create an AmbiguousMatchError from a list of org UUIDs and a mapping of those
// UUIDs to Organization objects.
func makeAMEFromOrgs(ctx context.Context, db appliancedb.DataStore, input string, oneName bool,
	orgUUIDs []uuidmod.UUID, orgMap map[uuidmod.UUID]entity) AmbiguousMatchError {
	ame := AmbiguousMatchError{input: input, oneName: oneName, typ: "org"}
	ame.entities = make([]entityUUID, len(orgUUIDs))

	for i, uuid := range orgUUIDs {
		ame.entities[i] = entityUUID{
			Name: orgMap[uuid].(entityOrg).Name,
			UUID: uuid,
		}
	}

	return ame
}

// Create an AmbiguousMatchError from a list of site UUIDs and a mapping of site
// UUIDs to CustomerSite objects (which have the names and organization UUIDs).
func makeAMEFromSites(ctx context.Context, db appliancedb.DataStore, input string, oneName bool,
	siteUUIDs []uuidmod.UUID, siteMap map[uuidmod.UUID]entity) AmbiguousMatchError {
	ame := AmbiguousMatchError{input: input, oneName: oneName, typ: "site"}
	ame.entities = make([]entityUUID, len(siteUUIDs))

	// Create an array of the organization UUIDs matching siteUUIDs.
	orgUUIDs := make([]uuidmod.UUID, len(siteUUIDs))
	for i, uuid := range siteUUIDs {
		orgUUIDs[i] = siteMap[uuid].(entitySite).OrganizationUUID
	}

	// Now go to the DB and grab the names for those UUIDs.  If that fails,
	// just substitute the UUID strings instead.
	orgNames, err := orgNamesByUUIDs(ctx, db, orgUUIDs)
	if err != nil {
		for _, uuid := range siteUUIDs {
			ou := siteMap[uuid].(entitySite).OrganizationUUID
			orgNames[ou] = ou.String()
		}
	}

	// Fill in the error struct.
	for i, uuid := range siteUUIDs {
		ame.entities[i] = entityUUID{
			ParentName: orgNames[siteMap[uuid].(entitySite).OrganizationUUID],
			Name:       siteMap[uuid].(entitySite).Name,
			UUID:       uuid,
		}
	}

	return ame
}

// FuzzyMatch is the return type of {Site,Org}UUIDByNameFuzzy(), and
// encapsulates the UUID of a site or org and its corresponding name.
type FuzzyMatch struct {
	UUID uuidmod.UUID
	Name string
}

// Should be limited to a connection string or a DataStore.
type dataStoreOrString interface{}

type makeAMEFunc func(context.Context, appliancedb.DataStore, string, bool,
	[]uuidmod.UUID, map[uuidmod.UUID]entity) AmbiguousMatchError

type getEntitiesFunc func(ctx context.Context, db appliancedb.DataStore) ([]entity, error)

// OrgUUIDByNameFuzzy takes an input string and tries to figure out what that
// might mean as an organization.  If it's in the form of a valid UUID, it'll
// return the UUID.  If it's not, it'll look in the database and do a fuzzy
// match of the input against the org names.
//
// The dbURI parameter can be a database connection string, or it can be an
// existing database handle (appliancedb.DataStore).
func OrgUUIDByNameFuzzy(ctx context.Context, dbURI dataStoreOrString, input string) (*FuzzyMatch, error) {
	getEntities := func(ctx context.Context, db appliancedb.DataStore) ([]entity, error) {
		orgs, err := db.AllOrganizations(ctx)
		var ret []entity
		for _, org := range orgs {
			ret = append(ret, entityOrg(org))
		}
		return ret, err
	}
	return uuidByNameFuzzy(ctx, dbURI, input, getEntities, makeAMEFromOrgs)
}

// SiteUUIDByNameFuzzy takes an input string and tries to figure out what that
// might mean as a site.  If it's in the form of a valid UUID, it'll return the
// UUID.  If it's not, it'll look in the database and do a fuzzy match of the
// input against the site names.
//
// The dbURI parameter can be a database connection string, or it can be an
// existing database handle (appliancedb.DataStore).
func SiteUUIDByNameFuzzy(ctx context.Context, dbURI dataStoreOrString, input string) (*FuzzyMatch, error) {
	getEntities := func(ctx context.Context, db appliancedb.DataStore) ([]entity, error) {
		sites, err := db.AllCustomerSites(ctx)
		var ret []entity
		for _, site := range sites {
			ret = append(ret, entitySite(site))
		}
		return ret, err
	}
	return uuidByNameFuzzy(ctx, dbURI, input, getEntities, makeAMEFromSites)
}

// uuidByNameFuzzy is the generic code behind OrgUUIDByNameFuzzy and
// SiteUUIDByNameFuzzy.
func uuidByNameFuzzy(ctx context.Context, dbURI dataStoreOrString, input string,
	getEntities getEntitiesFunc, makeAME makeAMEFunc) (*FuzzyMatch, error) {
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
		panic(fmt.Sprintf("Unexpected type '%T' for dbURI in uuidByNameFuzzy()", dbURI))
	}
	entities, err := getEntities(ctx, db)
	if err != nil {
		return nil, err
	}

	// Build some new data structures from this data.  entityNames is the
	// corpus for the fuzzy matching routines; entityUUIDs lets us do quick
	// lookups of the entity UUID from a name; and entityMap is used when
	// building ambiguous match errors, to fill in those data structures.
	entityNames := make([]string, len(entities))
	entityUUIDs := make(map[string][]uuidmod.UUID, len(entities))
	entityMap := make(map[uuidmod.UUID]entity, len(entities))
	for i, entity := range entities {
		// XXX Do we want to split them up by dashes, remove other
		// non-word characters?
		name := entity.name()
		uu := entity.uuid()
		entityNames[i] = name
		entityUUIDs[name] = append(entityUUIDs[name], uu)
		entityMap[uu] = entity
	}

	// If input matches a name exactly, and there is only one site with that
	// name, return it.  If there are more sites, return an error indicating
	// the ambiguity.
	if uuids, ok := entityUUIDs[input]; ok {
		if len(uuids) == 1 {
			// No name in the return value indicates the input was
			// an exact match.
			return &FuzzyMatch{uuids[0], ""}, nil
		}
		return nil, makeAME(ctx, db, input, true, uuids, entityMap)
	}

	// Do some fuzzy matching
	bagSizes := []int{2, 3, 4}
	cm := closestmatch.New(entityNames, bagSizes)
	// When we have a very small corpus, such as in test cases, we want to
	// make sure we return pretty much everything possible.
	n := len(entityNames) / 2
	if n < 10 {
		n = len(entityNames)
	}
	words := cm.ClosestNRank(input, n)
	if len(words) == 0 {
		return nil, fmt.Errorf("Couldn't find any site name matches for %q",
			input)
	}

	// Run through the matches.  If there appears to be a cliff (one ranking
	// is at least 3x of the next), then offer the ones before the cliff.
	// Mark a steeper cliff if there's a 10x dropoff prior to that, which we
	// can use to eliminate warnings.
	val := words[0].Value
	matches := []string{}
	strongMatches := 0
	bigCliff := 10
	if bc, err := strconv.Atoi(os.Getenv("BG_FUZZYMATCH_BIGCLIFF")); err == nil {
		bigCliff = bc
	}
	debug := os.Getenv("BG_FUZZYMATCH_DEBUG")
	if debug != "" {
		fmt.Println(words)
	}
	for i, pair := range words {
		if val > bigCliff*pair.Value && strongMatches == 0 {
			if debug != "" {
				fmt.Printf("Found %dx cutoff; previous value "+
					"(%d) is %.1fx %d\n", bigCliff, val,
					float32(val)/float32(pair.Value), pair.Value)
			}
			strongMatches = i
		}
		if val > 3*pair.Value {
			if debug != "" {
				fmt.Printf("Found 3x cutoff; previous value "+
					"(%d) is %.1fx %d\n", val,
					float32(val)/float32(pair.Value), pair.Value)
			}
			break
		}
		matches = append(matches, pair.Key)
		val = pair.Value
	}

	// If there's just one reasonable match (and there's only one site with
	// that name), then return it, allowing the caller to warn the user that
	// it might still not be what the user is looking for.
	if len(matches) == 1 {
		if uuids, ok := entityUUIDs[matches[0]]; ok {
			if len(uuids) > 1 {
				return nil, makeAME(ctx, db, input, true, uuids, entityMap)
			}
			fm := &FuzzyMatch{UUID: uuids[0]}
			// If we never got any strong matches, or got too many,
			// the user should see a warning.
			if strongMatches != 1 {
				fm.Name = entityMap[uuids[0]].name()
			}
			return fm, nil
		}
	}

	// If there are multiple possible matches, punt and make the caller pass
	// in something more specific.
	uuids := make([]uuidmod.UUID, 0)
	for _, name := range matches {
		for _, uuid := range entityUUIDs[name] {
			uuids = append(uuids, uuid)
		}
	}
	return nil, makeAME(ctx, db, input, false, uuids, entityMap)
}
