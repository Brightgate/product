/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/satori/uuid"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

const (
	app3Str  = "10000000-1000-1000-1000-000000000003"
	site3Str = "20000000-2000-2000-2000-000000000003"
	org3Str  = "30000000-3000-3000-3000-000000000003"
)

var (
	testOrg3 = Organization{
		UUID: uuid.Must(uuid.FromString(org3Str)),
		Name: "org3",
	}
	testSite3 = CustomerSite{
		UUID:             uuid.Must(uuid.FromString(site3Str)),
		OrganizationUUID: testOrg3.UUID,
		Name:             "site3",
	}
	testID3 = ApplianceID{
		ApplianceUUID:  uuid.Must(uuid.FromString(app3Str)),
		SiteUUID:       uuid.Must(uuid.FromString(site3Str)),
		GCPProject:     testProject,
		GCPRegion:      testRegion,
		ApplianceReg:   testReg,
		ApplianceRegID: testRegID + "-3",
	}
)

func testServerCerts(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	// Make sure there are a handful of appliances to operate on
	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)
	mkOrgSiteApp(t, ds, &testOrg2, &testSite2, &testID2)
	mkOrgSiteApp(t, ds, &testOrg3, &testSite3, &testID3)

	exp1 := time.Date(2018, 12, 7, 16, 37, 44, 0, time.UTC)
	exp2 := exp1.Add(5 * time.Second)

	domain, err := ds.NextDomain(ctx, "")
	assert.NoError(err)
	cert1 := &ServerCert{
		Domain:       domain.Domain,
		SiteID:       domain.SiteID,
		Jurisdiction: domain.Jurisdiction,
		Fingerprint:  hexDecode("cafebeef"),
		Expiration:   exp1,
		Cert:         []byte{0x01},
		IssuerCert:   []byte{0x01},
		Key:          []byte{0x01},
	}
	cert2 := &ServerCert{
		Domain:       domain.Domain,
		SiteID:       domain.SiteID,
		Jurisdiction: domain.Jurisdiction,
		Fingerprint:  hexDecode("feedface"),
		Expiration:   exp2,
		Cert:         []byte{0x01},
		IssuerCert:   []byte{0x01},
		Key:          []byte{0x01},
	}
	cert3 := &ServerCert{
		Domain:       domain.Domain,
		SiteID:       domain.SiteID,
		Jurisdiction: domain.Jurisdiction,
		Fingerprint:  hexDecode("feedface"),
		Expiration:   exp2,
		Cert:         []byte{0x02},
		IssuerCert:   []byte{0x02},
		Key:          []byte{0x02},
	}
	domain, err = ds.NextDomain(ctx, "uk")
	assert.NoError(err)
	cert4 := &ServerCert{
		Domain:       domain.Domain,
		SiteID:       domain.SiteID,
		Jurisdiction: domain.Jurisdiction,
		Fingerprint:  hexDecode("ecafdeef"),
		Expiration:   exp2,
		Cert:         []byte{0x04},
		IssuerCert:   []byte{0x04},
		Key:          []byte{0x04},
	}
	domain, err = ds.NextDomain(ctx, "uk")
	assert.NoError(err)
	cert5 := &ServerCert{
		Domain:       domain.Domain,
		SiteID:       domain.SiteID,
		Jurisdiction: domain.Jurisdiction,
		Fingerprint:  hexDecode("ecafdeef"),
		Expiration:   exp2,
		Cert:         []byte{0x05},
		IssuerCert:   []byte{0x05},
		Key:          []byte{0x05},
	}

	// Make sure we can insert multiple certs for the same domain, but not
	// if the fingerprints match.
	err = ds.InsertServerCert(ctx, cert1)
	assert.NoError(err)
	err = ds.InsertServerCert(ctx, cert2)
	assert.NoError(err)
	err = ds.InsertServerCert(ctx, cert3)
	assert.Error(err)

	// Make sure we retrieve cert2, which is the latest.
	certResp, err := ds.ServerCertByFingerprint(ctx, hexDecode("feedface"))
	assert.NoError(err)
	assert.Equal(cert2, certResp)

	// If we ask for a specific fingerprint, make sure we get it.
	certResp, err = ds.ServerCertByFingerprint(ctx, cert1.Fingerprint)
	assert.NoError(err)
	assert.Equal(cert1, certResp)

	// Make sure we get both if we ask for all
	certArr, _, err := ds.AllServerCerts(ctx)
	assert.NoError(err)
	assert.Len(certArr, 2)

	// Add a cert for a new site.
	err = ds.InsertServerCert(ctx, cert4)
	assert.NoError(err)

	// Make sure that no domains are claimed
	unclaimed, err := ds.UnclaimedDomainCount(ctx)
	assert.NoError(err)
	assert.Equal(int64(2), unclaimed)

	// Register site 12777 (claim the domain)
	domainStr, isNew, err := ds.RegisterDomain(ctx, testID1.SiteUUID, "")
	assert.NoError(err)
	assert.True(isNew)
	assert.Equal("12777.brightgate.net", domainStr)

	// The domain for 12777 should be claimed
	unclaimed, err = ds.UnclaimedDomainCount(ctx)
	assert.NoError(err)
	assert.Equal(int64(1), unclaimed)

	// Make sure that AllServerCerts returns the right site UUIDs, too.
	certArr, uuArr, err := ds.AllServerCerts(ctx)
	assert.NoError(err)
	assert.Len(certArr, 3)
	assert.Len(uuArr, 3)
	for i := 0; i < len(certArr); i++ {
		if certArr[i].Domain == "12777.brightgate.net" {
			assert.Equal(testID1.SiteUUID, uuArr[i].UUID)
		} else {
			assert.False(uuArr[i].Valid)
		}
	}

	// Claim 12777.uk
	domainStr, isNew, err = ds.RegisterDomain(ctx, testID2.SiteUUID, "uk")
	assert.NoError(err)
	assert.True(isNew)
	assert.Equal("12777.uk.brightgate.net", domainStr)

	// No domain left unclaimed
	unclaimed, err = ds.UnclaimedDomainCount(ctx)
	assert.NoError(err)
	assert.Equal(int64(0), unclaimed)

	// Make sure that siteid auto-incrementing works for a second
	// jurisdiction
	err = ds.InsertServerCert(ctx, cert5)
	assert.NoError(err)
	domainStr, isNew, err = ds.RegisterDomain(ctx, testID3.SiteUUID, "uk")
	assert.NoError(err)
	assert.True(isNew)
	assert.Equal("62984.uk.brightgate.net", domainStr)

	// Make sure that max_claimed is what we expect
	adb := ds.(*ApplianceDB)
	var maxClaimedUK int
	err = adb.GetContext(ctx, &maxClaimedUK,
		`SELECT max_claimed FROM siteid_sequences WHERE jurisdiction = 'uk'`)
	assert.NoError(err)
	assert.Equal(1, maxClaimedUK) // 0 and 1 claimed for uk

	// Make sure that registering a site again just returns the original
	// domain, even if the jurisdiction is different.
	domainStr, isNew, err = ds.RegisterDomain(ctx, testID2.SiteUUID, "de")
	assert.NoError(err)
	assert.False(isNew)
	assert.Equal("12777.uk.brightgate.net", domainStr)

	// Make sure that the above didn't actually insert "de" into the
	// siteid_sequences table.
	var maxClaimedDE sql.NullInt64
	err = adb.GetContext(ctx, &maxClaimedDE,
		`SELECT max_claimed FROM siteid_sequences WHERE jurisdiction = 'de'`)
	assert.EqualError(err, sql.ErrNoRows.Error())

	// Also, the max_claimed for "uk" shouldn't have gotten incremented.
	err = adb.GetContext(ctx, &maxClaimedUK,
		`SELECT max_claimed FROM siteid_sequences WHERE jurisdiction = 'uk'`)
	assert.NoError(err)
	assert.Equal(1, maxClaimedUK)

	// Check that it doesn't happen even when the jurisdiction is the same
	domainStr, isNew, err = ds.RegisterDomain(ctx, testID2.SiteUUID, "uk")
	assert.NoError(err)
	assert.False(isNew)
	assert.Equal("12777.uk.brightgate.net", domainStr)
	err = adb.GetContext(ctx, &maxClaimedUK,
		`SELECT max_claimed FROM siteid_sequences WHERE jurisdiction = 'uk'`)
	assert.NoError(err)
	assert.Equal(1, maxClaimedUK)

	// If we re-register, make sure we get the same domain, even when it's
	// not siteid 0.
	domainStr, isNew, err = ds.RegisterDomain(ctx, testID3.SiteUUID, "uk")
	assert.NoError(err)
	assert.False(isNew)
	assert.Equal("62984.uk.brightgate.net", domainStr)

	// When getting a cert by the site UUID, make sure we get the latest
	// one.
	certResp, err = ds.ServerCertByUUID(ctx, testID1.SiteUUID)
	assert.NoError(err)
	assert.Equal(cert2, certResp)

	// Remove a cert belonging to a site and see that we can discover that.
	// We can use DeleteExpiredServerCerts() because all certs are expired.
	count, err := ds.DeleteExpiredServerCerts(ctx, testID1.SiteUUID)
	assert.NoError(err)
	assert.EqualValues(2, count) // cert1 & cert2
	domains, err := ds.DomainsMissingCerts(ctx)
	assert.NoError(err)
	assert.Len(domains, 1)
	assert.Equal("12777.brightgate.net", domains[0].Domain)

	// Some simple failed domain testing: insert some failed domains and
	// make sure that we get them back out again.
	fd0 := DecomposedDomain{"12777.brightgate.net", 0, ""}
	fd1 := DecomposedDomain{"62984.brightgate.net", 1, ""}
	err = ds.FailDomains(ctx, []DecomposedDomain{fd0, fd1})
	assert.NoError(err)
	fds, err := ds.FailedDomains(ctx, false)
	assert.Len(fds, 2)
	assert.Equal(fds, []DecomposedDomain{fd0, fd1})

	// There shouldn't be any domains left in the failed table
	fds, err = ds.FailedDomains(ctx, false)
	assert.Len(fds, 0)

	// Failed domains should come back ordered by siteid.
	err = ds.FailDomains(ctx, []DecomposedDomain{fd1, fd0})
	assert.NoError(err)
	fds, err = ds.FailedDomains(ctx, false)
	assert.Len(fds, 2)
	assert.Equal(fds, []DecomposedDomain{fd0, fd1})

	// An empty set of failed domains returns no error.
	err = ds.FailDomains(ctx, []DecomposedDomain{})
	assert.NoError(err)

	// Make sure GetSiteUUIDByDomain returns the expected UUID for a
	// registered domain.
	u, err := ds.GetSiteUUIDByDomain(ctx, DecomposedDomain{"", 0, "uk"})
	assert.NoError(err)
	assert.Equal(testID2.SiteUUID, u)

	// Make sure GetSiteUUIDByDomain returns NotFoundError for an unknown
	// domain.
	_, err = ds.GetSiteUUIDByDomain(ctx, DecomposedDomain{SiteID: 9999})
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	// Getting the config info for a non-existent/non-registered domain
	// should come back empty, not with an error.  Also if the requested
	// domains list is empty.
	cci, err := ds.GetCertConfigInfoByDomain(ctx,
		[]DecomposedDomain{DecomposedDomain{SiteID: 9999}})
	assert.NoError(err)
	assert.Empty(cci)
	cci, err = ds.GetCertConfigInfoByDomain(ctx, []DecomposedDomain{})
	assert.NoError(err)
	assert.Empty(cci)

	// Retrieve the config-tree related information for a couple of the
	// domains.  Use more than one to test the argument expansion.
	cci, err = ds.GetCertConfigInfoByDomain(ctx,
		[]DecomposedDomain{
			DecomposedDomain{
				SiteID:       cert4.SiteID,
				Jurisdiction: cert4.Jurisdiction,
			},
			DecomposedDomain{
				SiteID:       cert5.SiteID,
				Jurisdiction: cert5.Jurisdiction,
			}})
	assert.NoError(err)
	assert.Len(cci, 2)
	assert.Equal(testID2.SiteUUID, cci["12777.uk.brightgate.net"].UUID)
	assert.Equal(cert4.Fingerprint, cci["12777.uk.brightgate.net"].Fingerprint)
	assert.Equal(cert4.Expiration, cci["12777.uk.brightgate.net"].Expiration)
	assert.Equal(testID3.SiteUUID, cci["62984.uk.brightgate.net"].UUID)
	assert.Equal(cert5.Fingerprint, cci["62984.uk.brightgate.net"].Fingerprint)
	assert.Equal(cert5.Expiration, cci["62984.uk.brightgate.net"].Expiration)

	// Make sure GetMaxUnclaimed returns expected values before and after a
	// call to NextDomain; that is, incremented by one.
	maxUnclaimed, err := ds.GetMaxUnclaimed(ctx)
	assert.NoError(err)
	maxUK := maxUnclaimed["uk"].SiteID
	maxNone := maxUnclaimed[""].SiteID
	_, err = ds.NextDomain(ctx, "uk")
	assert.NoError(err)
	maxUnclaimed, err = ds.GetMaxUnclaimed(ctx)
	assert.NoError(err)
	assert.Equal(maxUK+1, maxUnclaimed["uk"].SiteID)

	// Make sure that ResetMaxUnclaimed resets max_unclaimed to the value
	// specified in its arguments, and doesn't change the value for a
	// jurisdiction that isn't represented.
	newMax := map[string]DecomposedDomain{"uk": DecomposedDomain{"", maxUK, "uk"}}
	err = ds.ResetMaxUnclaimed(ctx, newMax)
	assert.NoError(err)
	maxUnclaimed, err = ds.GetMaxUnclaimed(ctx)
	assert.NoError(err)
	assert.Equal(maxUK, maxUnclaimed["uk"].SiteID)
	assert.Equal(maxNone, maxUnclaimed[""].SiteID)
}

func testServerCertsDelete(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	numSites := 10
	certsPerSite := 4

	// Just one org.
	mkOrgSiteApp(t, ds, &testOrg1, nil, nil)

	// Make a handful of sites, each one with one appliance
	sites := make([]CustomerSite, numSites)
	apps := make([]ApplianceID, numSites)
	for i := 0; i < numSites; i++ {
		siteUU := uuid.Must(uuid.FromString(fmt.Sprintf("1%07d-%04d-%04d-%04d-%012d", i, i, i, i, i)))
		sites[i] = CustomerSite{
			UUID:             siteUU,
			OrganizationUUID: testOrg1.UUID,
			Name:             fmt.Sprintf("site%d", i),
		}
		appUU := uuid.Must(uuid.FromString(fmt.Sprintf("0%07d-%04d-%04d-%04d-%012d", i, i, i, i, i)))
		apps[i] = ApplianceID{
			ApplianceUUID:  appUU,
			SiteUUID:       siteUU,
			GCPProject:     testProject,
			GCPRegion:      testRegion,
			ApplianceReg:   testReg,
			ApplianceRegID: fmt.Sprintf("%s-%d", testRegID, i),
		}
		mkOrgSiteApp(t, ds, nil, &sites[i], &apps[i])
	}

	// One domain per site
	var domains []DecomposedDomain
	for i := 0; i < numSites; i++ {
		domain, err := ds.NextDomain(ctx, "")
		assert.NoError(err)
		domains = append(domains, domain)
	}

	expirePast := time.Now().Add(-time.Hour).Round(time.Millisecond).UTC()
	expireFuture := time.Now().Add(time.Hour).Round(time.Millisecond).UTC()

	var certs []*ServerCert
	for i := 0; i < numSites*certsPerSite; i++ {
		// Certs are allocated in contiguous blocks of certsPerSite.
		domain := domains[i/certsPerSite]

		expire := expirePast
		// Keep one cert per site expiring in the future.
		if i%certsPerSite == certsPerSite-1 {
			expire = expireFuture
		}

		certs = append(certs, &ServerCert{
			Domain:       domain.Domain,
			SiteID:       domain.SiteID,
			Jurisdiction: domain.Jurisdiction,
			Fingerprint:  []byte{byte(i), byte(i), byte(i), byte(i)},
			Expiration:   expire,
			Cert:         []byte{byte(i)},
			IssuerCert:   []byte{byte(i)},
			Key:          []byte{byte(i)},
		})
		// t.Logf("cert %d: domain %d, cert %v", i, i/certsPerSite, certs[i])
		err := ds.InsertServerCert(ctx, certs[i])
		assert.NoError(err)
	}

	for i := 0; i < numSites; i++ {
		_, _, err := ds.RegisterDomain(ctx, sites[i].UUID, "")
		assert.NoError(err)
	}

	getCertMap := func() map[int32][]ServerCert {
		allCerts, _, _ := ds.AllServerCerts(ctx)
		allCertMap := make(map[int32][]ServerCert, numSites)
		for i := range allCerts {
			siteID := allCerts[i].SiteID
			allCertMap[siteID] = append(allCertMap[siteID], allCerts[i])
		}
		return allCertMap
	}

	// Delete the expired certs from one site
	count, err := ds.DeleteExpiredServerCerts(ctx, sites[0].UUID)
	assert.NoError(err)
	assert.EqualValues(certsPerSite-1, count)

	// Make sure exactly one cert is left from site 0, and that it's the one
	// that expires in the future.
	allCertMap := getCertMap()
	assert.Len(allCertMap[0], 1)
	assert.Equal(expireFuture, allCertMap[0][0].Expiration)

	// Delete the expired certs from two more sites
	count, err = ds.DeleteExpiredServerCerts(ctx, sites[1].UUID, sites[2].UUID)
	assert.NoError(err)
	assert.EqualValues(2*(certsPerSite-1), count)
	allCertMap = getCertMap()
	assert.Len(allCertMap[0], 1)
	assert.Len(allCertMap[1], 1)
	assert.Len(allCertMap[2], 1)
	assert.Equal(expireFuture, allCertMap[0][0].Expiration)
	assert.Equal(expireFuture, allCertMap[1][0].Expiration)
	assert.Equal(expireFuture, allCertMap[2][0].Expiration)

	// Delete the expired certs from one more site, making sure that
	// specifying sites with no longer any expired certs doesn't confuse
	// anything.
	count, err = ds.DeleteExpiredServerCerts(ctx, sites[1].UUID, sites[2].UUID, sites[3].UUID)
	assert.NoError(err)
	assert.EqualValues(certsPerSite-1, count)
	allCertMap = getCertMap()
	assert.Len(allCertMap[0], 1)
	assert.Len(allCertMap[1], 1)
	assert.Len(allCertMap[2], 1)
	assert.Len(allCertMap[3], 1)
	assert.Equal(expireFuture, allCertMap[0][0].Expiration)
	assert.Equal(expireFuture, allCertMap[1][0].Expiration)
	assert.Equal(expireFuture, allCertMap[2][0].Expiration)
	assert.Equal(expireFuture, allCertMap[3][0].Expiration)

	// Delete all the remainder of the expired certs.
	count, err = ds.DeleteExpiredServerCerts(ctx)
	assert.NoError(err)
	assert.EqualValues((numSites-4)*(certsPerSite-1), count)

	// Delete a non-expired cert
	count, err = ds.DeleteServerCertByFingerprint(ctx,
		[][]byte{certs[certsPerSite-1].Fingerprint})
	assert.NoError(err)
	assert.EqualValues(1, count)

	// Try again; just get a count of 0
	count, err = ds.DeleteServerCertByFingerprint(ctx,
		[][]byte{certs[certsPerSite-1].Fingerprint})
	assert.NoError(err)
	assert.EqualValues(0, count)

	// Try one that had already been expired
	count, err = ds.DeleteServerCertByFingerprint(ctx,
		[][]byte{certs[0].Fingerprint})
	assert.NoError(err)
	assert.EqualValues(0, count)

	// Delete a couple of certs
	count, err = ds.DeleteServerCertByFingerprint(ctx, [][]byte{
		certs[certsPerSite-1].Fingerprint,
		certs[2*certsPerSite-1].Fingerprint,
		certs[3*certsPerSite-1].Fingerprint,
	})
	assert.NoError(err)
	assert.EqualValues(2, count)
}
