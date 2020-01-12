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
	"testing"

	"github.com/satori/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"bg/cloud_models/appliancedb"
	"bg/cloud_models/appliancedb/mocks"
)

func aseSiteUUIDs(e AmbiguousSiteError) map[uuid.UUID]bool {
	m := make(map[uuid.UUID]bool, len(e.sites))
	for _, site := range e.sites {
		m[site.SiteUUID] = true
	}
	return m
}

func TestFuzzySiteMatch(t *testing.T) {
	assert := require.New(t)

	mockOrgs := []appliancedb.Organization{
		appliancedb.Organization{
			UUID: uuid.Must(uuid.FromString("10000000-0000-0000-0000-000000000001")),
			Name: "org 1",
		},
		appliancedb.Organization{
			UUID: uuid.Must(uuid.FromString("10000000-0000-0000-0000-000000000002")),
			Name: "org 2",
		},
	}

	mockSites := []appliancedb.CustomerSite{
		appliancedb.CustomerSite{
			UUID:             uuid.Must(uuid.FromString("00000000-0000-0000-0001-000000000001")),
			OrganizationUUID: mockOrgs[0].UUID,
			Name:             "duvall-office",
		},
		appliancedb.CustomerSite{
			UUID:             uuid.Must(uuid.FromString("00000000-0000-0000-0002-000000000002")),
			OrganizationUUID: mockOrgs[1].UUID,
			Name:             "duvall-office",
		},
		appliancedb.CustomerSite{
			UUID:             uuid.Must(uuid.FromString("00000000-0000-0000-0002-000000000003")),
			OrganizationUUID: mockOrgs[1].UUID,
			Name:             "dp-office",
		},
		// We need to have one that's sufficiently different from the
		// others so that a search for "office" doesn't find it, but
		// does find all the others.
		appliancedb.CustomerSite{
			UUID:             uuid.Must(uuid.FromString("00000000-0000-0000-0002-000000000004")),
			OrganizationUUID: mockOrgs[1].UUID,
			Name:             "dogfooding",
		},
	}

	dMock := &mocks.DataStore{}
	dMock.On("AllCustomerSites", mock.Anything).Return(mockSites, nil)
	dMock.On("OrganizationByUUID", mock.Anything, mockOrgs[0].UUID).Return(&mockOrgs[0], nil)
	dMock.On("OrganizationByUUID", mock.Anything, mockOrgs[1].UUID).Return(&mockOrgs[1], nil)
	defer dMock.AssertExpectations(t)

	ctx := context.Background()

	// Empty input should succeed, with essentially empty output.
	fm, err := SiteUUIDByNameFuzzy(ctx, dMock, "")
	assert.NoError(err)
	assert.Equal(uuid.Nil, fm.SiteUUID)
	assert.Equal("", fm.SiteName)

	// A valid UUID string passed in should succeed, with that same UUID
	// coming back.
	fm, err = SiteUUIDByNameFuzzy(ctx, dMock, "00000000-0000-0000-0001-000000000001")
	assert.NoError(err)
	assert.Equal(mockSites[0].UUID, fm.SiteUUID)
	assert.Equal("", fm.SiteName)

	// The same, except using a UUID not in the database.
	fm, err = SiteUUIDByNameFuzzy(ctx, dMock, "feedface-0000-0000-0000-000000000000")
	assert.NoError(err)
	assert.Equal(uuid.Must(uuid.FromString("feedface-0000-0000-0000-000000000000")), fm.SiteUUID)
	assert.Equal("", fm.SiteName)

	// There are two sites with the same name; make sure when requesting it,
	// we get an error with those two sites.
	fm, err = SiteUUIDByNameFuzzy(ctx, dMock, "duvall-office")
	assert.Nil(fm)
	assert.Error(err)
	assert.IsType(AmbiguousSiteError{}, err)
	ase := err.(AmbiguousSiteError)
	assert.Equal(2, len(ase.sites))
	aseSites := aseSiteUUIDs(ase)
	assert.Contains(aseSites, mockSites[0].UUID)
	assert.Contains(aseSites, mockSites[1].UUID)

	// The same should happen when we misspell the name a little bit.
	fm, err = SiteUUIDByNameFuzzy(ctx, dMock, "duvall-ofice")
	assert.Nil(fm)
	assert.Error(err)
	assert.IsType(AmbiguousSiteError{}, err)
	ase = err.(AmbiguousSiteError)
	assert.Equal(2, len(ase.sites))
	aseSites = aseSiteUUIDs(ase)
	assert.Contains(aseSites, mockSites[0].UUID)
	assert.Contains(aseSites, mockSites[1].UUID)

	// There are three sites with "office" in the name, make sure we get all
	// three back in the error.
	fm, err = SiteUUIDByNameFuzzy(ctx, dMock, "office")
	assert.Nil(fm)
	assert.Error(err)
	assert.IsType(AmbiguousSiteError{}, err)
	ase = err.(AmbiguousSiteError)
	assert.Equal(3, len(ase.sites))
	aseSites = aseSiteUUIDs(ase)
	assert.Contains(aseSites, mockSites[0].UUID)
	assert.Contains(aseSites, mockSites[1].UUID)
	assert.Contains(aseSites, mockSites[2].UUID)

	// An exact match with exactly one site should return success.  No name
	// in the return value indicates the input was an exact match.
	fm, err = SiteUUIDByNameFuzzy(ctx, dMock, "dp-office")
	assert.NoError(err)
	assert.Equal(mockSites[2].UUID, fm.SiteUUID)
	assert.Equal("", fm.SiteName)

	// An almost-exact match with exactly one site should return success.
	// This time, the (correct) name should be in the return value.
	fm, err = SiteUUIDByNameFuzzy(ctx, dMock, "dogfoodbowl")
	assert.NoError(err)
	assert.Equal(mockSites[3].UUID, fm.SiteUUID)
	assert.Equal("dogfooding", fm.SiteName)

	// An input that doesn't come close to matching anything will return an
	// unstructured string error.
	fm, err = SiteUUIDByNameFuzzy(ctx, dMock, "zzzzzzzzzz")
	assert.Error(err)
	assert.IsType(fmt.Errorf(""), err)
}
