/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package appliancedb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"bg/base_def"

	"github.com/satori/uuid"
)

type certManager interface {
	AllServerCerts(context.Context) ([]ServerCert, error)
	ServerCertByFingerprint(context.Context, []byte) (*ServerCert, error)
	ServerCertByUUID(context.Context, uuid.UUID) (*ServerCert, error)
	InsertServerCert(context.Context, *ServerCert) error
	DeleteExpiredServerCerts(context.Context) (int64, error)
	UnclaimedDomainCount(context.Context) (int64, error)
	DomainsMissingCerts(context.Context) ([]DecomposedDomain, error)
	RegisterDomain(context.Context, uuid.UUID, string) (string, error)
	NextDomain(context.Context, string) (DecomposedDomain, error)
	ResetMaxUnclaimed(context.Context, map[string]DecomposedDomain) error
	GetMaxUnclaimed(context.Context) (map[string]DecomposedDomain, error)
	GetSiteUUIDByDomain(context.Context, DecomposedDomain) (uuid.UUID, error)
	GetCertConfigInfoByDomain(context.Context, []DecomposedDomain) (map[string]CertConfigInfo, error)
	CertsExpiringWithin(context.Context, time.Duration) ([]ServerCert, error)
	FailDomains(context.Context, []DecomposedDomain) error
	FailedDomains(context.Context, bool) ([]DecomposedDomain, error)
	ComputeDomain(context.Context, int32, string) (string, error)
}

// SiteDomain represents the Brightgate domain used at a particular site.
type SiteDomain struct {
	UUID         uuid.UUID `json:"site_uuid"`
	SiteID       int32     `json:"siteid"`
	Jurisdiction string    `json:"jurisdiction"`
}

// DecomposedDomain represents a domain decomposed into its raw siteid and
// jurisdiction.  The Domain field is for convenience.
type DecomposedDomain struct {
	Domain       string `json:"domain"`
	SiteID       int32  `json:"siteid"`
	Jurisdiction string `json:"jurisdiction"`
}

// ServerCert represents the TLS certificate used by an appliance for EAP
// authentication and its web server.  The Domain field is for convenience.
type ServerCert struct {
	Domain       string    `json:"domain"`
	SiteID       int32     `json:"siteid"`
	Jurisdiction string    `json:"jurisdiction"`
	Fingerprint  []byte    `json:"fingerprint"`
	Expiration   time.Time `json:"expiration"`
	Cert         []byte    `json:"certificate"`
	IssuerCert   []byte    `json:"issuer_cert"`
	Key          []byte    `json:"key"`
}

// CertConfigInfo is used by GetCertConfigInfoByDomain to return information
// needed to post a certificate's availability to the config tree.
type CertConfigInfo struct {
	UUID        uuid.UUID
	Fingerprint []byte
	Expiration  time.Time
}

var (
	computeDomain     = make(map[string]func(int32, string) string)
	computeDomainLock sync.Mutex
)

// ComputeDomain returns the DNS domain corresponding to the given siteid and
// jurisdiction.
func (db *ApplianceDB) ComputeDomain(ctx context.Context, siteid int32, jurisdiction string) (string, error) {
	// We cache the constants in siteid_sequences to avoid hitting the
	// database with the same query over and over.  Rows in that table
	// should never be modified, though we might see rows added from time to
	// time.
	computeDomainLock.Lock()
	defer computeDomainLock.Unlock()
	if f, ok := computeDomain[jurisdiction]; ok {
		return f(siteid, jurisdiction), nil
	}

	row := db.QueryRowContext(ctx,
		`SELECT factor, constant, range_min, range_max
		 FROM siteid_sequences
		 WHERE jurisdiction = $1`,
		jurisdiction)

	var factor, constant, min, max int32
	err := row.Scan(&factor, &constant, &min, &max)
	switch err {
	case sql.ErrNoRows:
		return "", NotFoundError{"jurisdiction not present"}
	case nil:
	default:
		panic(err)
	}

	computeDomain[jurisdiction] = func(siteid int32, jurisdiction string) string {
		obfuscated := (factor*siteid+constant)%(max-min) + min
		if jurisdiction == "" {
			return fmt.Sprintf("%d.%s",
				obfuscated, base_def.GATEWAY_CLIENT_DOMAIN)
		}
		return fmt.Sprintf("%d.%s.%s",
			obfuscated, jurisdiction, base_def.GATEWAY_CLIENT_DOMAIN)
	}

	return computeDomain[jurisdiction](siteid, jurisdiction), nil
}

// AllServerCerts returns a slice of all the certificates.
func (db *ApplianceDB) AllServerCerts(ctx context.Context) ([]ServerCert, error) {
	var certs []ServerCert

	err := db.SelectContext(ctx, &certs,
		`SELECT siteid, jurisdiction, fingerprint, expiration, cert, issuercert, key
                 FROM site_certs
		 ORDER BY jurisdiction, siteid, expiration`)
	if err != nil {
		return nil, err
	}

	for i, cert := range certs {
		domstr, err := db.ComputeDomain(ctx, cert.SiteID, cert.Jurisdiction)
		if err != nil {
			panic(err)
		}
		certs[i].Domain = domstr
	}
	return certs, nil
}

// CertsExpiringWithin returns the certs which are within `grace` of their
// expiration date.
func (db *ApplianceDB) CertsExpiringWithin(ctx context.Context, grace time.Duration) ([]ServerCert, error) {
	var certs []ServerCert

	// Go SQL drivers cannot automatically convert time.Duration to
	// interval, so we do that manually via the string representation.
	// We are careful to ignore domains which have recently-renewed
	// certificates even if they have others which are already expired
	// or are nearing expiration.
	err := db.SelectContext(ctx, &certs,
		`SELECT
		     siteid, jurisdiction, fingerprint, expiration, cert, issuercert, key
		 FROM (
		     SELECT DISTINCT ON (siteid, jurisdiction)
		         siteid, jurisdiction, fingerprint, expiration, cert, issuercert, key
		     FROM site_certs
		     ORDER BY siteid, jurisdiction, expiration DESC
		 ) AS junk
		 WHERE expiration - $1::interval < now()`,
		grace.String())
	if err != nil {
		return nil, err
	}
	for i, cert := range certs {
		domstr, err := db.ComputeDomain(ctx, cert.SiteID, cert.Jurisdiction)
		if err != nil {
			panic(err)
		}
		certs[i].Domain = domstr
	}
	return certs, nil
}

// ServerCertByFingerprint returns the certificate for the given fingerprint.
func (db *ApplianceDB) ServerCertByFingerprint(ctx context.Context, fingerprint []byte) (*ServerCert, error) {
	var cert ServerCert

	err := db.GetContext(ctx, &cert,
		`SELECT siteid, jurisdiction, fingerprint, expiration, cert, issuercert, key
		 FROM site_certs
		 WHERE fingerprint = $1`,
		fingerprint)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{"certificate not found"}
	case nil:
	default:
		panic(err)
	}
	domain, err := db.ComputeDomain(ctx, cert.SiteID, cert.Jurisdiction)
	if err != nil {
		return nil, err
	}
	cert.Domain = domain
	return &cert, nil
}

// ServerCertByUUID returns the newest certificate for the given site UUID.
func (db *ApplianceDB) ServerCertByUUID(ctx context.Context, u uuid.UUID) (*ServerCert, error) {
	var cert ServerCert

	err := db.GetContext(ctx, &cert,
		`SELECT c.siteid, c.jurisdiction, c.fingerprint, c.expiration, c.cert, c.issuercert, c.key
		 FROM site_certs c, site_domains d
		 WHERE d.site_uuid = $1 AND (c.siteid, c.jurisdiction) = (d.siteid, d.jurisdiction)
		 ORDER BY c.expiration DESC
		 LIMIT 1`,
		u)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{"no certificate found"}
	case nil:
	default:
		panic(err)
	}
	domain, err := db.ComputeDomain(ctx, cert.SiteID, cert.Jurisdiction)
	if err != nil {
		return nil, err
	}
	cert.Domain = domain
	return &cert, nil
}

// InsertServerCert inserts a server certificate into the database.
func (db *ApplianceDB) InsertServerCert(ctx context.Context, ci *ServerCert) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO site_certs
		 (siteid, jurisdiction, fingerprint, expiration, cert, issuercert, key)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		ci.SiteID, ci.Jurisdiction, ci.Fingerprint, ci.Expiration, ci.Cert, ci.IssuerCert, ci.Key)
	return err
}

// DeleteExpiredServerCerts removes any expired certificates from the database.
func (db *ApplianceDB) DeleteExpiredServerCerts(ctx context.Context) (int64, error) {
	result, err := db.ExecContext(ctx,
		`DELETE
		 FROM site_certs
		 WHERE expiration < CURRENT_TIMESTAMP`)
	if err != nil {
		return -1, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return -1, nil
	}
	return rows, nil
}

// UnclaimedDomainCount returns the number of domains whose certificates haven't
// been assigned to a site.
func (db *ApplianceDB) UnclaimedDomainCount(ctx context.Context) (int64, error) {
	row := db.QueryRowContext(ctx,
		`SELECT count(*) FROM (
		     SELECT DISTINCT c.siteid, c.jurisdiction
		     FROM site_certs c
		     WHERE NOT EXISTS (
		         SELECT 1
		         FROM site_domains d
		         WHERE (c.siteid, c.jurisdiction) = (d.siteid, d.jurisdiction)
		 )) AS junk`)
	var count int64
	err := row.Scan(&count)
	return count, err
}

// DomainsMissingCerts returns a list of domains which are missing entries in
// site_certs.
func (db *ApplianceDB) DomainsMissingCerts(ctx context.Context) ([]DecomposedDomain, error) {
	var domains []DecomposedDomain

	err := db.SelectContext(ctx, &domains,
		`SELECT d.siteid, d.jurisdiction
		 FROM site_domains d
		 WHERE NOT EXISTS (
		     SELECT 1
		     FROM site_certs c
		     WHERE (d.siteid, d.jurisdiction) = (c.siteid, c.jurisdiction)
		 )`)
	if err != nil {
		return nil, err
	}
	for i, dom := range domains {
		domstr, err := db.ComputeDomain(ctx, dom.SiteID, dom.Jurisdiction)
		if err != nil {
			panic(err)
		}
		domains[i].Domain = domstr
	}
	return domains, nil
}

// RegisterDomain assigns a siteid to a site and returns the domain.
func (db *ApplianceDB) RegisterDomain(ctx context.Context, u uuid.UUID, jurisdiction string) (string, error) {
	var siteid int32
	err := db.GetContext(ctx, &siteid,
		`INSERT INTO site_domains
		 (site_uuid, jurisdiction)
		 VALUES ($1, $2)
		 ON CONFLICT DO NOTHING
		 RETURNING siteid`,
		u, jurisdiction)

	// If we hit the conflict clause, then just return the domain we already
	// had.
	if err == sql.ErrNoRows {
		var dom DecomposedDomain
		err = db.GetContext(ctx, &dom,
			`SELECT siteid, jurisdiction
			 FROM site_domains
			 WHERE site_uuid = $1`,
			u)
		jurisdiction = dom.Jurisdiction
	}
	domain, err := db.ComputeDomain(ctx, siteid, jurisdiction)
	if err != nil {
		return "", err
	}
	return domain, err
}

// NextDomain returns the next unregistered domain for the given jurisdiction.
func (db *ApplianceDB) NextDomain(ctx context.Context, jurisdiction string) (DecomposedDomain, error) {
	// In a non-production situation, siteid_sequences.max_unclaimed may not
	// be greater than a siteid represented in site_domains, in which case
	// we'll return a domain which is actually "missing" (i.e., in
	// site_domains, but not in site_certs).
	row := db.QueryRowContext(ctx, `SELECT * FROM next_siteid_unclaimed($1)`, jurisdiction)
	next := DecomposedDomain{Jurisdiction: jurisdiction}
	err := row.Scan(&next.SiteID)
	if err != nil {
		return DecomposedDomain{}, err
	}
	domain, err := db.ComputeDomain(ctx, next.SiteID, next.Jurisdiction)
	next.Domain = domain
	return next, err
}

// GetMaxUnclaimed returns a map from the jurisdiction to a DecomposedDomain
// representing the maximum claimed siteid for that jurisdiction.
func (db *ApplianceDB) GetMaxUnclaimed(ctx context.Context) (map[string]DecomposedDomain, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT jurisdiction, max_unclaimed
		 FROM siteid_sequences
		 WHERE max_unclaimed IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	domains := make(map[string]DecomposedDomain)
	for rows.Next() {
		var domain DecomposedDomain
		err = rows.Scan(&domain.Jurisdiction, &domain.SiteID)
		if err != nil {
			panic(err)
		}
		domains[domain.Jurisdiction] = domain
	}

	return domains, nil
}

// ResetMaxUnclaimed resets the max_unclaimed column to the siteid represented
// in the domains parameter and removes all rows from failed_domains that
// correspond to IDs greater than the new max_unclaimed.
func (db *ApplianceDB) ResetMaxUnclaimed(ctx context.Context, domains map[string]DecomposedDomain) error {
	if len(domains) == 0 {
		tx, err := db.BeginTxx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		_, err = db.ExecContext(ctx,
			`UPDATE siteid_sequences
			 SET max_claimed = NULL, max_unclaimed = NULL`)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `DELETE from failed_domains`)
		if err != nil {
			return err
		}
		return tx.Commit()
	}

	var values []interface{}
	var placeholders []string
	for _, dom := range domains {
		placeholders = append(placeholders, "(?, ?::integer)")
		values = append(values, dom.Jurisdiction, dom.SiteID)
	}

	valuesStr := strings.Join(placeholders, ",")
	seqQuery := `UPDATE siteid_sequences ss
		  SET max_unclaimed = t.max_unclaimed
		  FROM (
		      VALUES ` + valuesStr + `
		  ) t(jurisdiction, max_unclaimed)
		  WHERE ss.jurisdiction = t.jurisdiction`
	seqQuery = db.Rebind(seqQuery)

	fdQuery := `WITH t(jurisdiction, siteid) AS (
		      VALUES ` + valuesStr + `
		  )
		  DELETE FROM failed_domains
		  USING t
		  WHERE
		      failed_domains.jurisdiction = t.jurisdiction AND
		      failed_domains.siteid > t.siteid`
	fdQuery = db.Rebind(fdQuery)

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = db.ExecContext(ctx, seqQuery, values...)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, fdQuery, values...)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// GetSiteUUIDByDomain returns the site UUID corresponding to the given domain.
func (db *ApplianceDB) GetSiteUUIDByDomain(ctx context.Context, domain DecomposedDomain) (uuid.UUID, error) {
	var u uuid.UUID
	err := db.GetContext(ctx, &u,
		`SELECT site_uuid
		 FROM site_domains
		 WHERE siteid = $1 AND jurisdiction = $2`,
		domain.SiteID, domain.Jurisdiction)
	if err == sql.ErrNoRows {
		// If we were passed a DecomposedDomain without a domain string,
		// try to find it, using something unique if we can't.
		if domain.Domain == "" {
			domStr, err := db.ComputeDomain(
				ctx, domain.SiteID, domain.Jurisdiction)
			if err != nil {
				domStr = fmt.Sprintf("(%q,%d)",
					domain.Jurisdiction, domain.SiteID)
			}
			domain.Domain = domStr
		}
		return u, NotFoundError{
			fmt.Sprintf("domain %q has not been claimed", domain.Domain),
		}
	}
	return u, err
}

// GetCertConfigInfoByDomain returns the site UUID, fingerprint, and expiration
// corresponding to each given domain.
func (db *ApplianceDB) GetCertConfigInfoByDomain(ctx context.Context, domains []DecomposedDomain) (map[string]CertConfigInfo, error) {
	if len(domains) == 0 {
		return nil, nil
	}

	// sqlx.In() only works for arrays of core types; here we expand the
	// position of the question mark into pairs of question marks, with as
	// many pairs as the length of domains, and explode the domains array
	// into its constituents to pass as arguments to the query.
	q1 := "(?,?),"
	q := make([]byte, 0, len(q1)*len(domains))
	args := make([]interface{}, 0, len(domains)*2)
	for _, dom := range domains {
		q = append(q, q1...)
		args = append(args, dom.SiteID, dom.Jurisdiction)
	}
	q = q[:len(q)-1]
	query := db.Rebind(`
		 SELECT d.siteid, d.jurisdiction, d.site_uuid, c.fingerprint, c.expiration
		 FROM site_domains d, site_certs c
		 WHERE (d.siteid, d.jurisdiction) IN (` + string(q) + `)
		     AND (d.siteid, d.jurisdiction) = (c.siteid, c.jurisdiction)`)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	retmap := make(map[string]CertConfigInfo)
	for rows.Next() {
		var i int32
		var j string
		var u uuid.UUID
		var f []byte
		var e time.Time
		err = rows.Scan(&i, &j, &u, &f, &e)
		if err != nil {
			panic(err)
		}
		domain, err := db.ComputeDomain(ctx, i, j)
		if err != nil {
			panic(err)
		}
		retmap[domain] = CertConfigInfo{
			UUID:        u,
			Fingerprint: f,
			Expiration:  e,
		}
	}

	return retmap, err
}

// FailDomains records the given domains as having failed ACME validation (for
// whatever reason).
func (db *ApplianceDB) FailDomains(ctx context.Context, domains []DecomposedDomain) error {
	if len(domains) == 0 {
		return nil
	}

	// What we really want is batch insert: https://github.com/jmoiron/sqlx/pull/285
	placeholders := make([]string, len(domains))
	values := []interface{}{}
	for i, dom := range domains {
		placeholders[i] = "(?, ?)"
		values = append(values, dom.SiteID, dom.Jurisdiction)
	}

	valuesStr := strings.Join(placeholders, ",")
	query := `INSERT INTO failed_domains
		  (siteid, jurisdiction)
		  VALUES ` + valuesStr + `
		  ON CONFLICT DO NOTHING`
	query = db.Rebind(query)
	_, err := db.ExecContext(ctx, query, values...)
	return err
}

// FailedDomains returns the domains in the table recording ACME validation
// failures, optionally simultaneously clearing it.
func (db *ApplianceDB) FailedDomains(ctx context.Context, keep bool) ([]DecomposedDomain, error) {
	var domains []DecomposedDomain

	var query string
	if keep {
		query = `SELECT siteid, jurisdiction
		    FROM failed_domains
		    ORDER BY siteid, jurisdiction`
	} else {
		query = `WITH deleted AS (
		    DELETE FROM failed_domains
		    RETURNING siteid, jurisdiction
		 )
		 SELECT siteid, jurisdiction FROM deleted
		 ORDER BY siteid, jurisdiction`
	}
	err := db.SelectContext(ctx, &domains, query)
	if err != nil {
		return nil, err
	}
	for i, dom := range domains {
		domstr, err := db.ComputeDomain(ctx, dom.SiteID, dom.Jurisdiction)
		if err != nil {
			panic(err)
		}
		domains[i].Domain = domstr
	}
	return domains, nil
}
