/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/cl_common/clcfg"
	"bg/cl_common/daemonutils"
	"bg/cl_common/pgutils"
	"bg/cloud_models/appliancedb"
	"bg/common/cfgapi"
	"bg/common/zaperr"

	"github.com/satori/uuid"
	"github.com/spf13/cobra"
	"github.com/tatsushid/go-prettytable"
	"github.com/tomazk/envcfg"
	"github.com/xenolf/lego/acme"
	"github.com/xenolf/lego/acme/api"
	"github.com/xenolf/lego/certificate"
	"github.com/xenolf/lego/challenge"
	"github.com/xenolf/lego/challenge/dns01"
	"github.com/xenolf/lego/lego"
	legolog "github.com/xenolf/lego/log"
	dnsexec "github.com/xenolf/lego/providers/dns/exec"
	dnsgoog "github.com/xenolf/lego/providers/dns/gcloud"

	"go.uber.org/zap"
)

// Cfg contains the environment variable-based configuration settings
type Cfg struct {
	PostgresConnection string `envcfg:"B10E_CLCERT_POSTGRES_CONNECTION"`

	AcmeURL     string `envcfg:"B10E_CLCERT_ACME_URL"`
	AcmeConfig  string `envcfg:"B10E_CLCERT_ACME_CONFIG"`
	DNSCredFile string `envcfg:"B10E_CLCERT_GOOGLE_DNS_CREDENTIALS"`
	DNSExec     string `envcfg:"B10E_CLCERT_DNS_CHALLENGE_EXE"`

	// Don't bother checking that DNS changes are in place before telling
	// the ACME server the challenge is ready; used for testing, probably in
	// combination with B10E_CLCERT_DNS_CHALLENGE_EXE=/bin/true.
	DNSSkipPreCheck bool `envcfg:"B10E_DNS_SKIP_PRECHECK"`

	// Wait this many seconds before letting lego run its precheck routines
	DNSDelayPreCheck int `envcfg:"B10E_DNS_DELAY_PRECHECK"`

	// How many unclaimed certs to keep in reserve, and how fast to fill it
	// up.
	PoolSize       int `envcfg:"B10E_CLCERT_POOL_SIZE"`
	PoolFillAmount int `envcfg:"B10E_CLCERT_POOL_FILL_AMOUNT"`

	// How long before expiration can certs be renewed
	GracePeriod duration `envcfg:"B10E_CLCERT_GRACE_PERIOD"`
	// Force the expiration of the certs; this may only affect the database,
	// and not the certs themselves.
	ExpirationOverride duration `envcfg:"B10E_CLCERT_EXPIRATION_OVERRIDE"`

	ConfigdConnection string `envcfg:"B10E_CLCERT_CLCONFIGD_CONNECTION"`
	// Whether to Disable TLS for outbound connections to cl.configd
	ConfigdDisableTLS bool `envcfg:"B10E_CLCERT_CLCONFIGD_DISABLE_TLS"`
}

type requiredUsage struct {
	cmd         *cobra.Command
	msg         string
	explanation string
}

func (e requiredUsage) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return "More information needed"
}

// type-wrap time.Duration so that we can pass values through envcfg
type duration time.Duration

func (d *duration) UnmarshalText(text []byte) error {
	dd, err := time.ParseDuration(string(text))
	*d = duration(dd)
	return err
}

// LegoHandler is an interface that abstracts what we need out of lego.
type LegoHandler interface {
	obtain(certificate.ObtainRequest) (*certificate.Resource, error)
	getPoolSize() int
	getPoolFillAmount() int
	getExpirationOverride() time.Duration
	getGracePeriod() time.Duration
	getLimiter() *time.Ticker
	createMap([]string)
	getToken(string) string
	getDomains(string) []string
}

type legoHandle struct {
	client             *lego.Client
	poolSize           int
	poolFill           int
	expirationOverride time.Duration
	gracePeriod        time.Duration

	// Maps domains to a string that uniquely identifies the order that
	// encompasses those domains.
	solveToken map[string]string
	// The reverse mapping: a map of the order identifier to the domains
	// that belong to that order.
	revSolveToken map[string][]string
	// A lock covering both the above maps.
	sync.Mutex
}

func (h *legoHandle) obtain(request certificate.ObtainRequest) (*certificate.Resource, error) {
	return h.client.Certificate.Obtain(request)
}

func (h *legoHandle) getPoolSize() int {
	return h.poolSize
}

func (h *legoHandle) getPoolFillAmount() int {
	return h.poolFill
}

func (h *legoHandle) getExpirationOverride() time.Duration {
	return h.expirationOverride
}

func (h *legoHandle) getGracePeriod() time.Duration {
	return h.gracePeriod
}

func (h *legoHandle) getLimiter() *time.Ticker {
	// The Let's Encrypt endpoints can only be hit 20 times a second.
	return time.NewTicker(time.Second / 20)
}

func (h *legoHandle) createMap(domains []string) {
	tok := domains[0] // Can be whatever, as long as it's unique

	h.Lock()
	defer h.Unlock()

	// Forward mapping
	for _, domain := range domains {
		h.solveToken[domain] = tok
	}

	// Reverse mapping
	h.revSolveToken[tok] = domains
}

func (h *legoHandle) getToken(domain string) string {
	h.Lock()
	defer h.Unlock()
	return h.solveToken[domain]
}

func (h *legoHandle) getDomains(token string) []string {
	h.Lock()
	defer h.Unlock()
	return h.revSolveToken[token]
}

func newLegoHandle(client *lego.Client) *legoHandle {
	return &legoHandle{
		client:             client,
		poolSize:           environ.PoolSize,
		poolFill:           environ.PoolFillAmount,
		gracePeriod:        time.Duration(environ.GracePeriod),
		expirationOverride: time.Duration(environ.ExpirationOverride),
		solveToken:         make(map[string]string),
		revSolveToken:      make(map[string][]string),
	}
}

const (
	checkMark = `✔︎ `
	pname     = "cl-cert"

	defaultPoolSize    = 500
	defaultPoolFill    = 30
	defaultDNSDelay    = 120
	defaultGracePeriod = 30 * 24 * time.Hour
)

var (
	log  *zap.Logger
	slog *zap.SugaredLogger

	environ Cfg

	// Where we keep track of authorization URLs so we can clean them up
	// later, if necessary.
	authURLs []string

	// Functions that need to be mocked for testing.
	getConfigClientHandle func(string) (*cfgapi.Handle, error)
)

func processEnv(dbOnly bool) {
	if environ.PostgresConnection == "" {
		slog.Fatalf("B10E_CLCERT_POSTGRES_CONNECTION must be set")
	}
	if dbOnly {
		return
	}

	if environ.AcmeURL == "" || environ.AcmeURL == "production" {
		if environ.AcmeURL == "" {
			slog.Warnf("Setting ACME URL to %s", lego.LEDirectoryProduction)
		}
		environ.AcmeURL = lego.LEDirectoryProduction
	} else if environ.AcmeURL == "staging" {
		environ.AcmeURL = lego.LEDirectoryStaging
	}
	if environ.AcmeConfig == "" {
		slog.Fatalf("B10E_CLCERT_ACME_CONFIG must be set")
	}
	if environ.ConfigdConnection == "" {
		slog.Fatalf("B10E_CLCERT_CLCONFIGD_CONNECTION must be set")
	}
	if environ.DNSCredFile == "" && environ.DNSExec == "" {
		slog.Fatalf("B10E_CLCERT_GOOGLE_DNS_CREDENTIALS or " +
			"B10E_CLCERT_DNS_CHALLENGE_EXE must be set")
	}
	if environ.DNSDelayPreCheck == 0 {
		environ.DNSDelayPreCheck = defaultDNSDelay
	}
	if environ.PoolSize == 0 {
		environ.PoolSize = defaultPoolSize
	}
	if environ.PoolFillAmount == 0 {
		environ.PoolFillAmount = defaultPoolFill
	}
	if environ.GracePeriod == 0 {
		environ.GracePeriod = duration(defaultGracePeriod)
	}
	slog.Infof(checkMark + "Environ looks good")
}

// makeApplianceDB handles connection setup to the appliance database
func makeApplianceDB(postgresURI string) appliancedb.DataStore {
	postgresURI, err := pgutils.PasswordPrompt(postgresURI)
	if err != nil {
		slog.Fatalw("failed to get DB password", "error", err)
	}
	applianceDB, err := appliancedb.Connect(postgresURI)
	if err != nil {
		slog.Fatalw("failed to connect to DB", "error", err)
	}
	slog.Infof(checkMark + "Connected to Appliance DB")
	err = applianceDB.Ping()
	if err != nil {
		slog.Fatalw("failed to ping DB", "error", err)
	}
	slog.Infof(checkMark + "Pinged Appliance DB")
	return applianceDB
}

func getCertsForDomains(ctx context.Context, lh LegoHandler, db appliancedb.DataStore, tag string, domains []appliancedb.DecomposedDomain) []appliancedb.DecomposedDomain {
	limiter := lh.getLimiter()
	if limiter != nil {
		defer limiter.Stop()
	}

	errc := make(chan error)
	failedDomainChan := make(chan appliancedb.DecomposedDomain)
	var wg sync.WaitGroup

	for _, domain := range domains {
		slog.Infow("Requesting "+tag+" certificate", "domain", domain.Domain)
		wg.Add(1)
		go obtainAndStoreCertForAsync(ctx, lh, db, domain, errc,
			failedDomainChan, &wg)
		if limiter != nil {
			<-limiter.C
		}
	}

	doneChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneChan)
	}()
	failedDomains := []appliancedb.DecomposedDomain{}
	for done := false; !done; {
		select {
		case domain := <-failedDomainChan:
			failedDomains = append(failedDomains, domain)
		case err := <-errc:
			if err == nil {
				continue
			}
			slog.Errorw("failed to request "+tag+" certificate",
				"error", err)
		case <-doneChan:
			done = true
		}
	}

	if len(failedDomains) > 0 {
		// We want to log only the composed domain strings.
		strdoms := make([]string, len(failedDomains))
		for i, dom := range failedDomains {
			strdoms[i] = dom.Domain
		}

		err := db.FailDomains(ctx, failedDomains)
		if err != nil {
			slog.Errorw("Failed to record failed domains",
				"error", err,
				"domains", strdoms)
		} else {
			slog.Infow("Recorded failed domains for later retry",
				"domains", strdoms)
		}
	}

	fdMap := map[string]bool{}
	for _, domain := range failedDomains {
		fdMap[domain.Domain] = true
	}
	succeededDomains := []appliancedb.DecomposedDomain{}
	for _, domain := range domains {
		if ok := fdMap[domain.Domain]; !ok {
			succeededDomains = append(succeededDomains, domain)
		}
	}

	return succeededDomains
}

// maybePostCerts posts certs to the config tree of the appropriate site, if
// there is a site which is bound to the certificate's domain.
func maybePostCerts(ctx context.Context, db appliancedb.DataStore, succeeded []appliancedb.DecomposedDomain) error {
	m, err := db.GetCertConfigInfoByDomain(ctx, succeeded)
	if err != nil {
		return err
	}

	for domain, cci := range m {
		cert := &appliancedb.ServerCert{
			Fingerprint: cci.Fingerprint,
			Expiration:  cci.Expiration,
		}
		if err = postCert(cert, cci.UUID, domain); err != nil {
			slog.Errorw("Failed to post certificate",
				"domain", domain, "error", err)
		}
	}

	return nil
}

// getMissingCerts fetches or validates certificates which are missing from the
// certificate table, but we think we should have.
func getMissingCerts(ctx context.Context, lh LegoHandler, db appliancedb.DataStore) error {
	domains, err := db.DomainsMissingCerts(ctx)
	if err != nil {
		return err
	}

	succeeded := getCertsForDomains(ctx, lh, db, "missing", domains)
	return maybePostCerts(ctx, db, succeeded)
}

// getFailedCerts retries validation for domains which previously failed.
func getFailedCerts(ctx context.Context, lh LegoHandler, db appliancedb.DataStore) error {
	domains, err := db.FailedDomains(ctx)
	if err != nil {
		return err
	}
	if len(domains) == 0 {
		return nil
	}

	succeeded := getCertsForDomains(ctx, lh, db, "previously failed", domains)
	return maybePostCerts(ctx, db, succeeded)
}

// getNewCerts fills up the pool of unclaimed certificates.  Let's Encrypt's
// new-cert rate limit allows for 50 a week.  We don't manage that limit in any
// way, relying instead on getting rate-limit errors and retrying later.  We do
// limit attempts to $B10E_CLCERT_POOL_FILL_AMOUNT because trying to submit 500
// concurrent DNS zone changes to Google ends up in 100% failure.
func getNewCerts(ctx context.Context, lh LegoHandler, db appliancedb.DataStore) error {
	unclaimed, err := db.UnclaimedDomainCount(ctx)
	if err != nil {
		return err
	}

	var msg string
	if int(unclaimed) < lh.getPoolSize() {
		msg = "Filling up certificate pool"
	} else {
		msg = "Certificate pool full"
	}
	fillAmount := lh.getPoolFillAmount()
	if limit := lh.getPoolSize() - int(unclaimed); fillAmount > limit {
		fillAmount = limit
	}
	slog.Infow(msg, "poolsize", lh.getPoolSize(), "unclaimed", unclaimed,
		"fill-amount", fillAmount)

	watermarks, err := db.GetMaxUnclaimed(ctx)
	if err != nil {
		return err
	}

	var domains []appliancedb.DecomposedDomain
	for i := 0; i < fillAmount; i++ {
		// XXX We'll want to allocate certs for other domains, too, once
		// we know the desired distribution.
		nextDomain, err := db.NextDomain(ctx, "")
		if err != nil {
			return err
		}
		domains = append(domains, nextDomain)
	}

	succeeded := getCertsForDomains(ctx, lh, db, "new", domains)
	// There probably won't be any postings here, but just in case ...
	// We delay returning this error until after we tweak max_unclaimed.
	postErr := maybePostCerts(ctx, db, succeeded)

	// Trawl through the succeeded domains and find the highest siteid for
	// each jurisdiction, and go remark max_unclaimed for that jurisdiction
	// in siteid_sequences.
	for _, domain := range succeeded {
		if domain.SiteID > watermarks[domain.Jurisdiction].SiteID {
			watermarks[domain.Jurisdiction] = domain
		}
	}
	err = db.ResetMaxUnclaimed(ctx, watermarks)
	if err != nil {
		return err
	}
	return postErr
}

func renewOneCert(ctx context.Context, lh LegoHandler, db appliancedb.DataStore, cert appliancedb.ServerCert, errc chan error, wg *sync.WaitGroup) {
	defer wg.Done()
	domain := appliancedb.DecomposedDomain{
		Domain:       cert.Domain,
		SiteID:       cert.SiteID,
		Jurisdiction: cert.Jurisdiction,
	}
	slog.Infow("Renewing certificate",
		"domain", cert.Domain,
		"fingerprint", hex.EncodeToString(cert.Fingerprint))
	newCert, err := obtainAndStoreCert(ctx, lh, db, domain)
	if err != nil {
		errc <- zaperr.Errorw("Couldn't obtain/store cert",
			"domain", cert.Domain, "error", err)
		return
	}
	u, err := db.GetSiteUUIDByDomain(ctx, domain)
	if err != nil {
		// If the domain hasn't been claimed, then there's no
		// appliance to post the renewal to; move on.
		if _, ok := err.(appliancedb.NotFoundError); ok {
			slog.Infow("Renewed unclaimed domain",
				"domain", cert.Domain)
			return
		}
		errc <- zaperr.Errorw("Couldn't get site by domain",
			"domain", cert.Domain, "error", err)
		return
	}
	errc <- postCert(newCert, u, cert.Domain)
}

func renewCerts(ctx context.Context, lh LegoHandler, db appliancedb.DataStore) error {
	certs, err := db.CertsExpiringWithin(ctx, lh.getGracePeriod())
	if err != nil {
		return err
	}

	slog.Infow("Certificates to renew", "renewable", len(certs))

	limiter := lh.getLimiter()
	if limiter != nil {
		defer limiter.Stop()
	}

	errc := make(chan error)
	var wg sync.WaitGroup

	for _, cert := range certs {
		wg.Add(1)
		go renewOneCert(ctx, lh, db, cert, errc, &wg)
		if limiter != nil {
			<-limiter.C
		}
	}

	doneChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneChan)
	}()
	for done := false; !done; {
		select {
		case err = <-errc:
			if err == nil {
				continue
			}
			slog.Errorw("failed to renew certificate", "error", err)
		case <-doneChan:
			done = true
		}
	}

	return nil
}

func deleteExpiredCerts(ctx context.Context, db appliancedb.DataStore) error {
	ndel, err := db.DeleteExpiredServerCerts(ctx)
	if err == nil {
		slog.Infow("Deleted expired certificates", "deleted", ndel)
	}
	return err
}

func obtainAndStoreCertForAsync(ctx context.Context, lh LegoHandler, db appliancedb.DataStore,
	domain appliancedb.DecomposedDomain, errc chan error,
	failedDomainChan chan appliancedb.DecomposedDomain, wg *sync.WaitGroup) {
	defer func() {
		wg.Done()
	}()
	_, err := obtainAndStoreCert(ctx, lh, db, domain)
	if err != nil {
		failedDomainChan <- domain
	}
	errc <- err
}

func tryObtainCert(lh LegoHandler, db appliancedb.DataStore, domains []string) (*certificate.Resource, bool, error) {
	lh.createMap(domains)

	request := certificate.ObtainRequest{
		Domains: domains,
		// Don't request a bundle, so that we can keep the cert and the issuer
		// cert separate for clients that can't use the bundle.
		Bundle: false,
		// XXX Not sure about this
		MustStaple: false,
	}
	// RenewCertificate() just calls ObtainCertificate() after cracking open
	// the provided CertificateRequest object and using its domains and
	// private key, so we might as well call ObtainCertificate() directly.
	certResp, err := lh.obtain(request)

	retryable := false
	switch typedErr := err.(type) {
	case acme.ProblemDetails:
		var zerrs zaperr.ZapErrorArray
		for _, serr := range typedErr.SubProblems {
			var msg string
			switch serr.Type {
			case acme.BadNonceErr:
				msg = "ACME Nonce error"
				retryable = true
			default:
				msg = "ACME error"
			}
			nerr := zaperr.Errorw(msg,
				"type", serr.Type,
				"detail", serr.Detail,
				"domain", serr.Identifier.Value)
			zerrs = append(zerrs, nerr)
		}
		err = zaperr.Errorw("Error obtaining certificate",
			"error", zerrs, "domains", domains)
	case acme.NonceError:
		err = zaperr.Errorw("Error obtaining certificate",
			"code", typedErr.HTTPStatus,
			"type", typedErr.Type,
			"detail", typedErr.Detail,
			"domains", domains)
		retryable = true
	case nil:
	default:
		typ := fmt.Sprintf("%T", err)
		err = zaperr.Errorw("Error obtaining certificate",
			"error", err, "error-type", typ, "domains", domains)
	}

	return certResp, retryable, err
}

func obtainAndStoreCert(ctx context.Context, lh LegoHandler, db appliancedb.DataStore,
	domain appliancedb.DecomposedDomain) (*appliancedb.ServerCert, error) {

	domains := []string{
		domain.Domain,
		fmt.Sprintf("*.%s", domain.Domain),
	}

	var err error
	var certResp *certificate.Resource
	retryable := true
	for retryable {
		certResp, retryable, err = tryObtainCert(lh, db, domains)
		if retryable {
			slog.Debugw("Retryable error obtaining certificates",
				"error", err)
		}
	}
	if err != nil {
		return nil, err
	}

	slog.Debugw("New raw certificates", "certResponse", certResp)

	// What we get back is PEM encoded, so we have to decode it in order to
	// get out the expiration date, as well as to put it into the database.
	certBlock, _ := pem.Decode([]byte(certResp.Certificate))
	if certBlock == nil {
		return nil, fmt.Errorf("Certificate not PEM encoded")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		err = zaperr.Errorw("Unable to parse certificate", "error", err)
		return nil, err
	}
	if lh.getExpirationOverride() != 0 {
		cert.NotAfter = time.Now().Add(lh.getExpirationOverride())
	}
	rawFingerprint := sha1.Sum(cert.Raw)

	issuerBlock, _ := pem.Decode([]byte(certResp.IssuerCertificate))
	if issuerBlock == nil {
		return nil, fmt.Errorf("Issuer cert not PEM encoded")
	}

	keyBlock, _ := pem.Decode([]byte(certResp.PrivateKey))
	if keyBlock == nil {
		return nil, fmt.Errorf("Key not PEM encoded")
	}

	dbCert := &appliancedb.ServerCert{
		SiteID:       domain.SiteID,
		Jurisdiction: domain.Jurisdiction,
		Fingerprint:  rawFingerprint[:],
		Expiration:   cert.NotAfter,
		Cert:         certBlock.Bytes,
		IssuerCert:   issuerBlock.Bytes,
		Key:          keyBlock.Bytes,
	}

	slog.Infow("New certificate",
		"fingerprint", hex.EncodeToString(rawFingerprint[:]),
		"expiration", cert.NotAfter, "domain", domains[0],
		"stableURL", certResp.CertStableURL)

	// Put the new one into the database
	err = db.InsertServerCert(ctx, dbCert)
	if err != nil {
		err = zaperr.Errorw("Database insertion failed", "error", err)
		return nil, err
	}

	return dbCert, nil
}

// Post the fingerprint to configd to alert the appliance it's ready to be
// downloaded.
func postCert(cert *appliancedb.ServerCert, u uuid.UUID, domain string) error {
	fingerprint := hex.EncodeToString(cert.Fingerprint)

	hdl, err := getConfigClientHandle(u.String())
	if err != nil {
		return zaperr.Errorw("Unable to contact cl.configd",
			"site-uuid", u, "domain", domain, "error", err)
	}
	defer hdl.Close()
	prop := fmt.Sprintf("@/certs/%s/state", fingerprint)
	// We don't create the origin node here too because a) only the cloud
	// sets the state to available, and b) it would make the code on the
	// client side more complicated, dealing with add vs set.
	if err = hdl.CreateProp(prop, "available", &cert.Expiration); err != nil {
		if err == cfgapi.ErrTimeout {
			slog.Warnw("Certificate posting to config tree timed out",
				"site-uuid", u, "domain", domain,
				"fingerprint", fingerprint)
			return nil
		}
		return zaperr.Errorw("Unable to set config property",
			"site-uuid", u, "domain", domain,
			"fingerprint", fingerprint, "error", err)
	}
	slog.Debugw("Certificate posted to config tree",
		"site-uuid", u, "domain", domain, "fingerprint", fingerprint)
	return nil
}

func deactivateAuthorizations(config *lego.Config) error {
	if len(authURLs) == 0 {
		return nil
	}

	core, err := api.New(config.HTTPClient, config.UserAgent,
		config.CADirURL, config.User.GetRegistration().URI,
		config.User.GetPrivateKey())
	if err != nil {
		return fmt.Errorf("Couldn't build ACME Core: %v", err)
	}

	for _, url := range authURLs {
		auth, err := core.Authorizations.Get(url)
		if err != nil {
			slog.Errorw("Couldn't get authorization",
				"url", url, "error", err)
			continue
		}
		if auth.Status != "pending" {
			continue
		}
		err = core.Authorizations.Deactivate(url)
		if err != nil {
			slog.Errorw("Couldn't deactivate authorization",
				"url", url, "error", err)
		}
		slog.Infow("Deactivated pending authorization", "url", url)
	}

	return nil
}

// LegoLog wraps a zap.SugaredLogger to provide the interface that Lego's logger
// wants, so that we can replace it, and have all the logs come through a single
// stream.
type LegoLog struct {
	slog *zap.SugaredLogger
}

// Fatal implements lego/log.StdLogger
func (ll LegoLog) Fatal(args ...interface{}) {
	ll.slog.Fatal(args...)
}

// Fatalf implements lego/log.StdLogger
func (ll LegoLog) Fatalf(format string, args ...interface{}) {
	ll.slog.Fatalf(format, args...)
}

// Fatalln implements lego/log.StdLogger
func (ll LegoLog) Fatalln(args ...interface{}) {
	args = append(args, "\n")
	ll.slog.Fatal(args...)
}

// Print implements lego/log.StdLogger
func (ll LegoLog) Print(args ...interface{}) {
	ll.slog.Info(args...)
}

// Printf implements lego/log.StdLogger.  Since lego's default logger prepends
// the log level to the message itself, we extract that and use it to determine
// the right zap logging level.
func (ll LegoLog) Printf(format string, args ...interface{}) {
	if strings.HasPrefix(format, "[INFO] ") {
		ll.slog.Infof(format[7:], args...)

		// We have no other way of getting at this information.
		// See https://github.com/xenolf/lego/issues/771
		if strings.Contains(format, " AuthURL: ") {
			url, ok := args[1].(string)
			if ok {
				authURLs = append(authURLs, url)
			}
		}
	} else if strings.HasPrefix(format, "[WARN] ") {
		ll.slog.Warnf(format[7:], args...)
	} else {
		ll.slog.Infof(format, args...)
	}
}

// Println implements lego/log.StdLogger
func (ll LegoLog) Println(args ...interface{}) {
	args = append(args, "\n")
	ll.slog.Info(args...)
}

func realGetConfigClientHandle(cuuid string) (*cfgapi.Handle, error) {
	configd, err := clcfg.NewConfigd(pname, cuuid,
		environ.ConfigdConnection, !environ.ConfigdDisableTLS)
	if err != nil {
		return nil, err
	}
	configHandle := cfgapi.NewHandle(configd)
	return configHandle, nil
}

// Create a "lockfile" to prevent a cooperating process from running
// concurrently.
func lock(lockPath string) error {
	locked := false

	err := os.Symlink(fmt.Sprintf("%d", os.Getpid()), lockPath)
	if lerr, ok := err.(*os.LinkError); ok {
		if serr, ok := lerr.Err.(syscall.Errno); ok {
			if serr == syscall.EEXIST {
				locked = true
			}
		}
	}
	if err != nil && !locked {
		return zaperr.Errorw("unanticipated error", "error", err)
	}

	if locked {
		pidStr, err := os.Readlink(lockPath)
		if err != nil {
			return zaperr.Errorw(
				"cl-cert is already running or just exited",
				"error", err)
		}
		return zaperr.Errorw("cl-cert is already running",
			"pid", pidStr)
	}

	return nil
}

// unlock is the counterpart of lock.  There's no point in returning an error,
// since it's used in a defer statement, and there's no way to grab the return
// value.
func unlock(lockPath string) {
	if err := os.Remove(lockPath); err != nil {
		fmt.Printf("error removing lock; please manually "+
			"remove %s before running cl-cert again\n", lockPath)
	}
}

func run(cmd *cobra.Command, args []string) error {
	log, slog = daemonutils.SetupLogs()
	defer log.Sync()

	lockPath := "/tmp/cl-cert.lock"
	if err := lock(lockPath); err != nil {
		slog.Fatalw("Failed to lock for cl-cert processing",
			"error", err)
	}
	defer unlock(lockPath)

	legolog.Logger = LegoLog{slog}

	err := envcfg.Unmarshal(&environ)
	if err != nil {
		slog.Fatalw("failed environment configuration", "error", err)
	}
	processEnv(false)
	slog.Infow(pname+" starting", "args", os.Args)

	config, client, err := acmeSetup(environ.AcmeConfig, environ.AcmeURL)
	if err != nil {
		slog.Fatalw("Failed to set up ACME connection info", "error", err)
	}
	lh := newLegoHandle(client)

	var provider challenge.Provider
	if environ.DNSExec != "" {
		provider, err = dnsexec.NewDNSProviderConfig(
			&dnsexec.Config{Program: environ.DNSExec})
	} else {
		provider, err = dnsgoog.NewDNSProviderServiceAccount(
			environ.DNSCredFile)
	}
	if err != nil {
		slog.Fatalw("Failed to set DNS challenge provider", "error", err)
	}

	challengeOptions := make([]dns01.ChallengeOption, 0)
	if environ.DNSSkipPreCheck {
		challengeOptions = append(challengeOptions,
			dns01.AddPreCheck(func(fqdn, value string) (bool, error) {
				return true, nil
			}))
	} else {
		solvedMap := make(map[string]bool)
		var solvedMapLock sync.Mutex
		wrapFunc := func(domain, fqdn, value string, orig dns01.PreCheckFunc) (bool, error) {
			token := lh.getToken(domain)
			solvedMapLock.Lock()
			solved := solvedMap[token]
			solvedMapLock.Unlock()
			if !solved {
				domains := lh.getDomains(token)
				delay := time.Duration(environ.DNSDelayPreCheck) * time.Second
				slog.Infof("[%s] Waiting %s before checking DNS propagation",
					strings.Join(domains, ", "), delay.Round(time.Second))
				time.Sleep(delay)
				solvedMapLock.Lock()
				solvedMap[token] = true
				solvedMapLock.Unlock()
			}
			return orig(fqdn, value)
		}
		challengeOptions = append(challengeOptions, dns01.WrapPreCheck(wrapFunc))
	}
	err = client.Challenge.SetDNS01Provider(provider, challengeOptions...)
	if err != nil {
		slog.Fatalw("Failed to set DNS challenge provider", "error", err)
	}
	slog.Info(checkMark + "Set up ACME connection info for " + environ.AcmeURL)

	getConfigClientHandle = realGetConfigClientHandle
	if environ.ConfigdDisableTLS {
		slog.Warn("Disabling TLS for connection to configd")
	}
	hdl, err := getConfigClientHandle(uuid.Nil.String())
	if err != nil {
		slog.Fatalw("failed to make config client", "error", err)
	}
	err = hdl.Ping(context.Background())
	hdl.Close()
	if err != nil {
		slog.Fatalw("failed to ping config client", "error", err)
	}
	slog.Info(checkMark + "Can connect to cl.configd")

	applianceDB := makeApplianceDB(environ.PostgresConnection)
	slog.Info(checkMark + "Connected to database")

	// Get certs for any domains that seem to be missing them.
	err = getMissingCerts(context.Background(), lh, applianceDB)
	if err != nil {
		slog.Errorw("failed to acquire missing certificates",
			"error", err)
	}

	// If we previously failed to validate any domains, try them again.
	err = getFailedCerts(context.Background(), lh, applianceDB)
	if err != nil {
		slog.Errorw("failed to request previously failed certificates",
			"error", err)
	}

	// Accumulate as many certificates as we can until we fill up the pool
	// or run into rate limits.
	err = getNewCerts(context.Background(), lh, applianceDB)
	if err != nil {
		slog.Errorw("failed to request new certificates", "error", err)
	}

	// Once we've retrieved all the new certs we can, we want to run through
	// our existing certificates and attempt to renew any which are within
	// `graceFlag` of expiration (or already expired).
	err = renewCerts(context.Background(), lh, applianceDB)
	if err != nil {
		slog.Errorw("failed to renew certificates", "error", err)
	}

	// Finally, we should delete any expired certificates.
	err = deleteExpiredCerts(context.Background(), applianceDB)
	if err != nil {
		slog.Errorw("failed to delete expired certificates", "error", err)
	}

	err = deactivateAuthorizations(config)
	if err != nil {
		slog.Errorw("failed to deactivate pending authorizations",
			"error", err)
	}

	return nil
}

func listCerts(cmd *cobra.Command, args []string) error {
	log, slog = daemonutils.SetupLogs()
	defer log.Sync()

	err := envcfg.Unmarshal(&environ)
	if err != nil {
		slog.Fatalw("failed environment configuration", "error", err)
	}
	processEnv(true)
	slog.Infow(pname+" starting", "args", os.Args)

	db := makeApplianceDB(environ.PostgresConnection)

	certs, err := db.AllServerCerts(context.Background())
	if err != nil {
		return err
	}

	table, _ := prettytable.NewTable(
		prettytable.Column{Header: "Domain"},
		prettytable.Column{Header: "Jurisdiction"},
		prettytable.Column{Header: "SiteID"},
		prettytable.Column{Header: "Fingerprint"},
		prettytable.Column{Header: "Expiration"},
	)
	table.Separator = " "

	for _, cert := range certs {
		table.AddRow(cert.Domain, cert.Jurisdiction, cert.SiteID,
			hex.EncodeToString(cert.Fingerprint), cert.Expiration.Round(time.Second))
	}
	table.Print()
	return nil
}

func certStatus(cmd *cobra.Command, args []string) error {
	log, slog = daemonutils.SetupLogs()
	defer log.Sync()

	err := envcfg.Unmarshal(&environ)
	if err != nil {
		slog.Fatalw("failed environment configuration", "error", err)
	}
	processEnv(true)
	slog.Infow(pname+" starting", "args", os.Args)

	db := makeApplianceDB(environ.PostgresConnection)

	missing, err := db.DomainsMissingCerts(context.Background())
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		table, _ := prettytable.NewTable(
			prettytable.Column{Header: "Domain"},
			prettytable.Column{Header: "Jurisdiction"},
			prettytable.Column{Header: "SiteID"},
		)
		table.Separator = " "

		for _, domain := range missing {
			table.AddRow(domain.Domain, domain.Jurisdiction,
				domain.SiteID)
		}
		slog.Warnw("Some registered sites are missing certs",
			"number", len(missing))
		table.Print()
	} else {
		slog.Info(checkMark + "No registered sites are missing certs")
	}

	failed, err := db.FailedDomains(context.Background())
	if err != nil {
		return err
	}
	if len(failed) > 0 {
		table, _ := prettytable.NewTable(
			prettytable.Column{Header: "Domain"},
			prettytable.Column{Header: "Jurisdiction"},
			prettytable.Column{Header: "SiteID"},
		)
		table.Separator = " "

		for _, domain := range failed {
			table.AddRow(domain.Domain, domain.Jurisdiction,
				domain.SiteID)
		}
		slog.Warnw("Some certificate requests failed and are awaiting retry",
			"number", len(failed))
		table.Print()
	} else {
		slog.Info(checkMark + "No certificate requests failed")
	}

	certs, err := db.AllServerCerts(context.Background())
	if err != nil {
		return err
	}
	width := len(fmt.Sprintf("%d", len(certs)))
	slog.Infof("  %*d certificates in pool", width, len(certs))
	unclaimed, err := db.UnclaimedDomainCount(context.Background())
	if err != nil {
		return err
	}
	slog.Infof("  %*d certificates unclaimed", width, unclaimed)

	type ld struct {
		mark     string
		label    string
		duration time.Duration
	}
	durations := []ld{
		{"✘", "already expired", 0},
		{"‼", "expiring within one day", 24 * time.Hour},
		{"!", "expiring within one week", 7 * 24 * time.Hour},
		{" ", "expiring within thirty days", 30 * 24 * time.Hour},
	}
	prev := 0
	for _, dur := range durations {
		certs, err := db.CertsExpiringWithin(context.Background(), dur.duration)
		if err != nil {
			return err
		}
		slog.Infof("%s %*d certificates %s", dur.mark, width, len(certs)-prev, dur.label)
		prev = len(certs)
	}

	return nil
}

func main() {
	rootCmd := cobra.Command{
		Use: os.Args[0],
	}

	registerCmd := &cobra.Command{
		Use:   "register [flags]",
		Short: "Register with the ACME service",
		Args:  cobra.NoArgs,
		RunE:  acmeRegister,
	}
	registerCmd.Flags().String("email", "x-appliance-certs@brightgate.com",
		"registration email")
	registerCmd.Flags().String("url", lego.LEDirectoryProduction,
		"registration URL")
	registerCmd.Flags().String("key-type", "rsa2048",
		"key type (rsa2048, rsa4096, rsa8192, ec256, ec384)")
	rootCmd.AddCommand(registerCmd)

	runCmd := &cobra.Command{
		Use: "run",
		Short: "Routine certificate maintenance " +
			"(retry failures, fill pool, renew certs)",
		Args: cobra.NoArgs,
		RunE: run,
	}
	runCmd.Flags().AddFlagSet(daemonutils.GetLogFlagSet())
	rootCmd.AddCommand(runCmd)

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List certificates",
		Args:  cobra.NoArgs,
		RunE:  listCerts,
	}
	rootCmd.AddCommand(listCmd)

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Get certificate pool status",
		Args:  cobra.NoArgs,
		RunE:  certStatus,
	}
	rootCmd.AddCommand(statusCmd)

	// Will likely also want subcommands to request and store certificates
	// for one or more specific domains, retrieve cert material, run fill,
	// renew, and retry separately.

	err := rootCmd.Execute()
	if err, ok := err.(requiredUsage); ok {
		err.cmd.Usage()
		extraUsage := "\n" + err.explanation
		io.WriteString(err.cmd.OutOrStderr(), extraUsage)
	}
	if err != nil {
		slog.Fatal(err)
	}
}
