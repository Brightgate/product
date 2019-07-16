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

package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"bg/cloud_models/appliancedb"
	"bg/common/briefpg"
	"bg/common/cfgapi"

	"github.com/satori/uuid"
	"github.com/stretchr/testify/require"
	"github.com/xenolf/lego/certificate"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	templateDBName = "appliancedb_clcert_template"
	templateDBArg  = "TEMPLATE=" + templateDBName

	testProject = "test-project"
	testRegion  = "test-region"
	testReg     = "test-registry"
	testRegID   = "test-appliance"
	app1Str     = "00000001-0001-0001-0001-000000000001"
	site1Str    = "10000001-0001-0001-0001-000000000001"
	org1Str     = "20000001-0001-0001-0001-000000000001"
)

var (
	bpg *briefpg.BriefPG

	siteIDMapDefault map[string]int

	testOrg1 = appliancedb.Organization{
		UUID: uuid.Must(uuid.FromString(org1Str)),
		Name: "org1",
	}

	testSite1 = appliancedb.CustomerSite{
		UUID:             uuid.Must(uuid.FromString(site1Str)),
		OrganizationUUID: testOrg1.UUID,
		Name:             "site1",
	}

	testID1 = appliancedb.ApplianceID{
		ApplianceUUID:  uuid.Must(uuid.FromString(app1Str)),
		SiteUUID:       uuid.Must(uuid.FromString(site1Str)),
		GCPProject:     testProject,
		GCPRegion:      testRegion,
		ApplianceReg:   testReg,
		ApplianceRegID: testRegID + "-1",
	}
)

type dbTestFunc func(*testing.T, appliancedb.DataStore, *zap.Logger, *zap.SugaredLogger)

func setupLogging(t *testing.T) (*zap.Logger, *zap.SugaredLogger) {
	logger := zaptest.NewLogger(t)
	slogger := logger.Sugar()
	return logger, slogger
}

// mkOrgSiteApp is a help function to prep the database: if not nil, add
// org, site, and/or appliance to the DB
func mkOrgSiteApp(t *testing.T, ds appliancedb.DataStore, org *appliancedb.Organization, site *appliancedb.CustomerSite, app *appliancedb.ApplianceID) {
	ctx := context.Background()
	assert := require.New(t)

	if org != nil {
		err := ds.InsertOrganization(ctx, org)
		assert.NoError(err, "expected Insert Organization to succeed")
	}
	if site != nil {
		err := ds.InsertCustomerSite(ctx, site)
		assert.NoError(err, "expected Insert Site to succeed")
	}
	if app != nil {
		err := ds.InsertApplianceID(ctx, app)
		assert.NoError(err, "expected Insert App to succeed")
	}
}

// make a template database, loaded with the schema.  Subsequently
// we can knock out copies.
func mkTemplate(ctx context.Context) error {
	templateURI, err := bpg.CreateDB(ctx, templateDBName, "")
	if err != nil {
		return fmt.Errorf("failed to make templatedb: %+v", err)
	}
	templateDB, err := appliancedb.Connect(templateURI)
	if err != nil {
		return fmt.Errorf("failed to connect to templatedb: %+v", err)
	}
	defer templateDB.Close()
	err = templateDB.LoadSchema(ctx, "../cloud_models/appliancedb/schema")
	if err != nil {
		return fmt.Errorf("failed to load schema: %+v", err)
	}
	return nil
}

// TestCmdHdl Implements a mocked CmdHdl; this handle always succeeds.
type TestCmdHdl struct{}

func (h *TestCmdHdl) Status(_ context.Context) (string, error) {
	return "", nil
}

func (h *TestCmdHdl) Wait(_ context.Context) (string, error) {
	return "", nil
}

// TestConfigExec implements ConfigExec; it does nothing except return
// TestCmdHdl.
type TestConfigExec struct{}

func (t *TestConfigExec) Ping(_ context.Context) error {
	return nil
}

func (t *TestConfigExec) Execute(_ context.Context, _ []cfgapi.PropertyOp) cfgapi.CmdHdl {
	return &TestCmdHdl{}
}

func (t *TestConfigExec) HandleChange(_ string, _ func([]string, string, *time.Time)) error {
	return nil
}

func (t *TestConfigExec) HandleDelete(_ string, _ func([]string)) error {
	return nil
}

func (t *TestConfigExec) HandleExpire(_ string, _ func([]string)) error {
	return nil
}

func (t *TestConfigExec) Close() {
}

func testGetConfigClientHandle(_ string) (*cfgapi.Handle, error) {
	return cfgapi.NewHandle(&TestConfigExec{}), nil
}

type legoCert = certificate.Resource

type testLegoHandle struct {
	obtainer           func(certificate.ObtainRequest) (*legoCert, error)
	poolsize           int
	poolfill           int
	expirationOverride time.Duration
	gracePeriod        time.Duration
	limit              time.Duration
}

func (h testLegoHandle) obtain(request certificate.ObtainRequest) (*legoCert, error) {
	return h.obtainer(request)
}

func (h testLegoHandle) getPoolSize() int {
	return h.poolsize
}

func (h testLegoHandle) getPoolFillAmount() int {
	// Default to 50, which is plenty big enough to work for all of the tests.
	if h.poolfill == 0 {
		return 50
	}
	return h.poolfill
}

func (h testLegoHandle) getExpirationOverride() time.Duration {
	return h.expirationOverride
}

func (h testLegoHandle) getGracePeriod() time.Duration {
	return h.gracePeriod
}

func (h testLegoHandle) getLimiter() *time.Ticker {
	if h.limit == 0 {
		return nil
	}
	return time.NewTicker(h.limit)
}

func (h testLegoHandle) createMap(_ []string)         {}
func (h testLegoHandle) getToken(_ string) string     { return "" }
func (h testLegoHandle) getDomains(_ string) []string { return []string{} }

func createSSKeyCert(domains []string) ([]byte, []byte, []byte) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	var serialMax big.Int
	serialMax.SetInt64(math.MaxInt64)

	notBefore := time.Now()
	notAfter := notBefore.Add(time.Hour)
	serialNumber, err := rand.Int(rand.Reader, &serialMax)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: domains[0],
		},
		DNSNames:  domains,
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	kb := x509.MarshalPKCS1PrivateKey(priv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: kb})
	if keyPEM == nil {
		panic("Couldn't encode private key")
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		panic(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if certPEM == nil {
		panic("Couldn't encode certificate")
	}

	return keyPEM, certPEM, certPEM
}

func testPing(t *testing.T, ds appliancedb.DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	assert := require.New(t)
	err := ds.Ping()
	assert.NoError(err)
}

// errorBasedObtainer returns an obtainer that returns a cert or an error based
// on the input maps of siteids to errors.
func errorBasedObtainer(args ...interface{}) func(certificate.ObtainRequest) (*legoCert, error) {
	errors := make(map[string]map[int]error)
	for i := 0; i < len(args); i += 2 {
		errors[args[i].(string)] = args[i+1].(map[int]error)
	}
	obtainer := func(request certificate.ObtainRequest) (*legoCert, error) {
		base := strings.Split(strings.TrimSuffix(request.Domains[0], "brightgate.net"), ".")
		jurisdiction := ""
		if len(base) == 2 {
			jurisdiction = base[1]
		}
		siteid := siteIDMapDefault[base[0]]
		if errors[jurisdiction][siteid] != nil {
			return nil, errors[jurisdiction][siteid]
		}
		key, cert, issuer := createSSKeyCert(request.Domains)
		return &legoCert{
			Domain:            request.Domains[0],
			PrivateKey:        key,
			Certificate:       cert,
			IssuerCertificate: issuer,
		}, nil
	}

	return obtainer
}

// perfectObtainer returns an obtainer that always succeeds.
func perfectObtainer() func(certificate.ObtainRequest) (*legoCert, error) {
	obtainer := func(request certificate.ObtainRequest) (*legoCert, error) {
		key, cert, issuer := createSSKeyCert(request.Domains)
		return &legoCert{
			Domain:            request.Domains[0],
			PrivateKey:        key,
			Certificate:       cert,
			IssuerCertificate: issuer,
		}, nil
	}

	return obtainer
}

func testFillPool(t *testing.T, ds appliancedb.DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	poolsize := 5

	// Just return valid key material without error.
	obtainer := perfectObtainer()
	lh := testLegoHandle{
		obtainer: obtainer,
		poolsize: poolsize,
	}

	// Fill up the pool.
	err := getNewCerts(ctx, lh, ds)
	assert.NoError(err)

	// Make sure the certs table is the size we expect, and make sure the
	// certificate content is reasonable.
	adb := ds.(*appliancedb.ApplianceDB)
	var certs []appliancedb.ServerCert
	err = adb.SelectContext(ctx, &certs, "SELECT siteid, jurisdiction, cert FROM site_certs")
	assert.NoError(err)
	assert.Len(certs, poolsize)
	for _, cert := range certs {
		domain, err := ds.ComputeDomain(ctx, cert.SiteID, cert.Jurisdiction)
		assert.NoError(err)
		certObj, err := x509.ParseCertificate(cert.Cert)
		assert.NoError(err)
		assert.Equal(domain, certObj.Subject.CommonName)
	}

	// Make sure no domains failed.
	doms, err := ds.FailedDomains(ctx, false)
	assert.NoError(err)
	assert.Empty(doms)

	// Fill up the pool again; this shouldn't do anything
	err = getNewCerts(ctx, lh, ds)
	assert.NoError(err)
	certs = nil
	err = adb.SelectContext(ctx, &certs, "SELECT siteid, jurisdiction FROM site_certs")
	assert.NoError(err)
	assert.Len(certs, poolsize)
}

func testFillPoolPartialError(t *testing.T, ds appliancedb.DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	poolsize := 5

	// Set up a few requests to error out.  Make sure they're not at the end
	// of the siteid run, or they'll not end up in failed_domains.
	errors := make(map[int]error)
	errors[2] = fmt.Errorf("Some crappy error")
	errors[3] = fmt.Errorf("Some crappy error")
	obtainer := errorBasedObtainer("", errors)
	lh := testLegoHandle{
		obtainer: obtainer,
		poolsize: poolsize,
	}

	// Fill up the pool.
	err := getNewCerts(ctx, lh, ds)
	assert.NoError(err)

	// Make sure the certs table is the size we expect.
	adb := ds.(*appliancedb.ApplianceDB)
	var certs []appliancedb.ServerCert
	err = adb.SelectContext(ctx, &certs, "SELECT siteid, jurisdiction FROM site_certs")
	assert.NoError(err)
	assert.Len(certs, poolsize-2)

	// Make sure the failed domains table is also the size we expect.
	doms, err := ds.FailedDomains(ctx, true)
	assert.NoError(err)
	assert.Len(doms, 2)

	// Run getFailedCerts() and fail one of the remaining two slots
	errors[2] = nil
	lh.obtainer = errorBasedObtainer("", errors)
	err = getFailedCerts(ctx, lh, ds)
	slog.Sync()
	assert.NoError(err)
	// The pool is a bit fuller ...
	certs = nil
	err = adb.SelectContext(ctx, &certs, "SELECT siteid, jurisdiction FROM site_certs")
	assert.NoError(err)
	assert.Len(certs, poolsize-1)
	// ... and failed_certs has one left
	doms2, err := ds.FailedDomains(ctx, true)
	assert.NoError(err)
	assert.Len(doms2, 1)
	assert.Contains(doms, doms2[0])

	// Finally, run getFailedCerts() with a perfect obtainer and make sure
	// everything looks good.
	lh.obtainer = perfectObtainer()
	err = getFailedCerts(ctx, lh, ds)
	assert.NoError(err)
	// The pool is full ...
	certs = nil
	err = adb.SelectContext(ctx, &certs, "SELECT siteid, jurisdiction FROM site_certs")
	assert.NoError(err)
	assert.Len(certs, poolsize)
	// ... and failed_certs is empty
	doms = nil
	err = adb.SelectContext(ctx, &doms, "SELECT * FROM failed_domains")
	assert.NoError(err)
	assert.Empty(doms)

	// getFailedCerts() on empty failed_certs should be idempotent.
	err = getFailedCerts(ctx, lh, ds)
	assert.NoError(err)
	certs = nil
	err = adb.SelectContext(ctx, &certs, "SELECT siteid, jurisdiction FROM site_certs")
	assert.NoError(err)
	assert.Len(certs, poolsize)
	doms = nil
	err = adb.SelectContext(ctx, &doms, "SELECT * FROM failed_domains")
	assert.NoError(err)
	assert.Empty(doms)
}

func testFillPoolCompleteError(t *testing.T, ds appliancedb.DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	poolsize := 5

	// Set up all the requests to error out.
	errors := make(map[int]error)
	for i := 0; i < poolsize; i++ {
		errors[i] = fmt.Errorf("Some crappy error")
	}
	obtainer := errorBasedObtainer("", errors)
	lh := testLegoHandle{
		obtainer: obtainer,
		poolsize: poolsize,
	}

	// Fill up the pool.
	err := getNewCerts(ctx, lh, ds)
	assert.NoError(err)

	// Make sure the certs table is empty.
	adb := ds.(*appliancedb.ApplianceDB)
	var certs []appliancedb.ServerCert
	err = adb.SelectContext(ctx, &certs, "SELECT siteid, jurisdiction FROM site_certs")
	assert.NoError(err)
	assert.Empty(certs)

	// Since this is the first time through, max_unclaimed for all
	// jurisdictions should get reset to NULL.
	rows, err := adb.QueryxContext(ctx,
		"SELECT jurisdiction, max_unclaimed FROM siteid_sequences")
	assert.NoError(err)
	for rows.Next() {
		rowmap := make(map[string]interface{})
		err = rows.MapScan(rowmap)
		assert.NoError(err)
		assert.Equalf(nil, rowmap["max_unclaimed"],
			"siteid_sequences: %v", rowmap)
	}
	rows.Close()

	// Make sure the failed domains table is also empty.
	var doms []appliancedb.DecomposedDomain
	err = adb.SelectContext(ctx, &doms, "SELECT * FROM failed_domains")
	assert.NoError(err)
	assert.Empty(doms)

	// Now really fill up the pool.
	lh.obtainer = perfectObtainer()
	err = getNewCerts(ctx, lh, ds)
	assert.NoError(err)

	// Make sure the certs table is full and failed_domains is empty.
	certs = nil
	err = adb.SelectContext(ctx, &certs, "SELECT siteid, jurisdiction FROM site_certs")
	assert.NoError(err)
	assert.Len(certs, poolsize)
	doms = nil
	err = adb.SelectContext(ctx, &doms, "SELECT * FROM failed_domains")
	assert.NoError(err)
	assert.Empty(doms)

	// Increase the pool size and refill with a completely inept obtainer.
	// But grab the current max_unclaimed first.
	rows, err = adb.QueryxContext(ctx,
		"SELECT jurisdiction, max_unclaimed FROM siteid_sequences")
	assert.NoError(err)
	ssm := make(map[string]interface{})
	for rows.Next() {
		rowmap := make(map[string]interface{})
		err = rows.MapScan(rowmap)
		assert.NoError(err)
		ssm[rowmap["jurisdiction"].(string)] = rowmap["max_unclaimed"]
	}
	rows.Close()
	oldPoolsize := poolsize
	poolsize = 10
	for i := oldPoolsize; i < poolsize; i++ {
		errors[i] = fmt.Errorf("Some crappy error")
	}
	lh.obtainer = errorBasedObtainer("", errors)
	err = getNewCerts(ctx, lh, ds)
	assert.NoError(err)

	// Make sure the certs table is still where it was, failed_domains is
	// empty, and siteid_sequences wasn't reset all the way to zero.
	certs = nil
	err = adb.SelectContext(ctx, &certs, "SELECT siteid, jurisdiction FROM site_certs")
	assert.NoError(err)
	assert.Len(certs, oldPoolsize)
	doms = nil
	err = adb.SelectContext(ctx, &doms, "SELECT * FROM failed_domains")
	assert.NoError(err)
	assert.Empty(doms)
	rows, err = adb.QueryxContext(ctx,
		"SELECT jurisdiction, max_unclaimed FROM siteid_sequences")
	assert.NoError(err)
	for rows.Next() {
		rowmap := make(map[string]interface{})
		err = rows.MapScan(rowmap)
		assert.NoError(err)
		assert.Equalf(ssm[rowmap["jurisdiction"].(string)], rowmap["max_unclaimed"],
			"siteid_sequences: %v", rowmap)
	}
	rows.Close()
}

func testCertRenewal(t *testing.T, ds appliancedb.DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	poolsize := 5

	// Just return valid key material without error.  Make sure to force a
	// short expiration so that we don't have to wait long before we can
	// renew.
	obtainer := perfectObtainer()
	lh := testLegoHandle{
		obtainer:           obtainer,
		poolsize:           poolsize,
		expirationOverride: 1 * time.Second,
		gracePeriod:        time.Second / 2,
	}

	// Fill up the pool.
	err := getNewCerts(ctx, lh, ds)
	assert.NoError(err)

	// Get the old expiration dates and fingerprints
	adb := ds.(*appliancedb.ApplianceDB)
	rows, err := adb.QueryxContext(ctx,
		"SELECT siteid, jurisdiction, expiration, fingerprint, cert FROM site_certs")
	assert.NoError(err)
	expmap := make(map[string]time.Time)
	fpmap := make(map[string][]byte)
	namesmap := make(map[string][]string)
	for rows.Next() {
		var siteid int32
		var juris string
		var exp time.Time
		var fp, certBytes []byte
		err = rows.Scan(&siteid, &juris, &exp, &fp, &certBytes)
		assert.NoError(err)
		domain, err := ds.ComputeDomain(ctx, siteid, juris)
		assert.NoError(err)
		expmap[domain] = exp
		fpmap[domain] = fp
		cert, err := x509.ParseCertificate(certBytes)
		assert.NoError(err)
		namesmap[domain] = cert.DNSNames
	}
	rows.Close()

	// Wait for a bit so that we pass the expiration
	time.Sleep(time.Second)

	// Renew the certs and delete the expired ones.  Go back to the normal
	// expiration so that we don't delete any of the renewed ones.
	lh.expirationOverride = 0
	err = renewCerts(ctx, lh, ds)
	assert.NoError(err)
	err = deleteExpiredCerts(ctx, ds)
	assert.NoError(err)

	// We need to make sure the certificates changed, and the expiration
	// dates moved forward, but that the embedded domains didn't.
	rows, err = adb.QueryxContext(ctx,
		"SELECT siteid, jurisdiction, expiration, fingerprint, cert FROM site_certs")
	assert.NoError(err)
	for rows.Next() {
		var siteid int32
		var juris string
		var exp time.Time
		var fp, certBytes []byte
		err = rows.Scan(&siteid, &juris, &exp, &fp, &certBytes)
		assert.NoError(err)
		domain, err := ds.ComputeDomain(ctx, siteid, juris)
		assert.NoError(err)
		assert.Truef(exp.After(expmap[domain]),
			"%s is not later than %s", exp, expmap[domain])
		assert.NotEqual(fpmap[domain], fp)
		cert, err := x509.ParseCertificate(certBytes)
		assert.NoError(err)
		assert.ElementsMatch(namesmap[domain], cert.DNSNames)
	}
	rows.Close()
}

// Make sure that total failure after a certain point (as if we hit the rate
// limit) resets the point where we start again, doesn't fill up failed_certs,
// etc.
func testNewCertRateLimit(t *testing.T, ds appliancedb.DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	poolsize := 10
	errors := make(map[int]error)
	errors[6] = fmt.Errorf("Some crappy error")
	errors[7] = fmt.Errorf("Some crappy error")
	errors[8] = fmt.Errorf("Some crappy error")
	errors[9] = fmt.Errorf("Some crappy error")
	obtainer := errorBasedObtainer("", errors)
	lh := testLegoHandle{
		obtainer: obtainer,
		poolsize: poolsize,
	}

	// Fill up the pool.
	err := getNewCerts(ctx, lh, ds)
	assert.NoError(err)
	count, err := ds.UnclaimedDomainCount(ctx)
	assert.NoError(err)
	assert.EqualValues(6, count)

	// Because the failures are all grouped at the end, we shouldn't record
	// them as failures; we should just let the next round of pool filling
	// take care of them.
	adb := ds.(*appliancedb.ApplianceDB)
	var doms []appliancedb.DecomposedDomain
	err = adb.SelectContext(ctx, &doms, "SELECT * FROM failed_domains")
	assert.NoError(err)
	assert.Empty(doms)

	// Do another round.  This time we leave a hole in the map; site 7
	// should count as failed, and site 9 should be the next pickup point.
	errors[6] = nil
	errors[8] = nil
	obtainer = errorBasedObtainer("", errors)
	lh.obtainer = obtainer
	err = getNewCerts(ctx, lh, ds)
	assert.NoError(err)
	count, err = ds.UnclaimedDomainCount(ctx)
	assert.NoError(err)
	assert.EqualValues(8, count)

	doms = nil
	err = adb.SelectContext(ctx, &doms, "SELECT * FROM failed_domains")
	assert.NoError(err)
	assert.Len(doms, 1)
}

// Make sure the we refill the pool to the right point after claiming a cert.
func testRefillPool(t *testing.T, ds appliancedb.DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	poolsize := 5
	obtainer := perfectObtainer()
	lh := testLegoHandle{
		obtainer: obtainer,
		poolsize: poolsize,
	}

	// Fill up the pool.
	err := getNewCerts(ctx, lh, ds)
	assert.NoError(err)
	count, err := ds.UnclaimedDomainCount(ctx)
	assert.NoError(err)
	assert.EqualValues(poolsize, count)

	// Create a site
	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)

	// Claim a cert for the site and make sure the unclaimed count goes
	// down.
	_, isNew, err := ds.RegisterDomain(ctx, testSite1.UUID, "")
	assert.NoError(err)
	assert.True(isNew)
	count, err = ds.UnclaimedDomainCount(ctx)
	assert.NoError(err)
	assert.EqualValues(poolsize-1, count)

	// Refill the pool and make sure the unclaimed count represents that.
	err = getNewCerts(ctx, lh, ds)
	assert.NoError(err)
	count, err = ds.UnclaimedDomainCount(ctx)
	assert.NoError(err)
	assert.EqualValues(poolsize, count)
}

func TestCertificateProcessing(t *testing.T) {
	var ctx = context.Background()
	bpg = briefpg.New(nil)
	defer bpg.Fini(ctx)
	err := bpg.Start(ctx)
	if err != nil {
		t.Fatalf("failed to setup: %+v", err)
	}
	err = mkTemplate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name  string
		tFunc dbTestFunc
	}{
		{"testPing", testPing},
		{"testFillPool", testFillPool},
		{"testFillPoolPartialError", testFillPoolPartialError},
		{"testFillPoolCompleteError", testFillPoolCompleteError},
		{"testRefillPool", testRefillPool},
		{"testNewCertRateLimit", testNewCertRateLimit},
		{"testCertRenewal", testCertRenewal},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logger, slogger := setupLogging(t)
			bpg.Logger = zap.NewStdLog(logger)
			// Ensure uniqueness so that things work if count > 1
			dbName := fmt.Sprintf("%s_%d", t.Name(), time.Now().Unix())

			testdb, err := bpg.CreateDB(ctx, dbName, templateDBArg)
			if err != nil {
				t.Fatalf("CreateDB Failed: %v", err)
			}
			ds, err := appliancedb.Connect(testdb)
			if err != nil {
				t.Fatalf("Connect failed: %v", err)
			}
			defer ds.Close()

			log, slog = logger, slogger

			tc.tFunc(t, ds, logger, slogger)
		})
	}
}

func TestMain(m *testing.M) {
	// Configure mocked functions
	getConfigClientHandle = testGetConfigClientHandle

	// Set up the reverse mapping from domains to siteids.
	siteIDMapDefault = make(map[string]int, 100)
	for i := 0; i < 100; i++ {
		siteid := (50207*i+2777)%(100000-10000) + 10000
		siteIDMapDefault[strconv.Itoa(siteid)] = i
	}

	os.Exit(m.Run())
}
