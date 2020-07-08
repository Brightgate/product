/*
 * COPYRIGHT 2020 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package appliancedb

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/guregu/null"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/satori/uuid"
)

type releaseManager interface {
	InsertArtifact(context.Context, ReleaseArtifact) (*ReleaseArtifact, error)
	InsertRelease(context.Context, []*ReleaseArtifact, map[string]string) (uuid.UUID, error)
	GetRelease(context.Context, uuid.UUID) (*Release, error)
	GetCurrentRelease(context.Context, uuid.UUID) (uuid.UUID, error)
	SetCurrentRelease(context.Context, uuid.UUID, uuid.UUID, time.Time, map[string]string) error
	GetTargetRelease(context.Context, uuid.UUID) (uuid.UUID, error)
	SetTargetRelease(context.Context, uuid.UUID, uuid.UUID) error
	ListReleases(context.Context) ([]*Release, error)
	GetReleaseStatusByAppliances(context.Context, []uuid.UUID) (map[uuid.UUID]ApplianceReleaseStatus, error)
	SetUpgradeResults(context.Context, time.Time, uuid.UUID, uuid.UUID, bool, sql.NullString, string) error
	SetUpgradeStage(context.Context, uuid.UUID, uuid.UUID, time.Time, string, bool, string) error
}

// ReleaseArtifact objects represent rows in the artifacts table.
type ReleaseArtifact struct {
	UUID       uuid.UUID // Artifact UUID
	Platform   string
	Repo       string `db:"repo_name"`
	Commit     []byte `db:"commit_hash"`
	Generation int
	Filename   string
	HashType   string `db:"hash_type"`
	Hash       []byte
}

// Scan implements the sql.Scanner interface.  The queries whose results
// populate this struct each have a column which is an array of composite (row)
// type, and it is a single member of those arrays which is being scanned here.
// The queries return different types here--there is some overlap, but they're
// used in different contexts, so the contents are different--but they are
// distinguishable by the number of elements, so we check that to determine
// which "mode" to use.  Note that no column can have a comma in the field, or
// this parsing will break.
func (ra *ReleaseArtifact) Scan(src interface{}) error {
	srcBytes, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("Type assertion from %T to []byte failed", src)
	}

	// Rows come back looking like: (col1,col2,col3,...), so first split on
	// commas, excluding the parentheses at either end.
	srcBytes = srcBytes[1 : len(srcBytes)-1]
	elems := bytes.Split(srcBytes, []byte(","))

	decodeBytea := func(in []byte) ([]byte, error) {
		// A bytea comes back like "\\x02468ace" (including the double
		// quotes); thus the actual decodable bytes start after four
		// characters ("\\x) and end one before the end (").
		hexdigits := in[4 : len(in)-1]
		ret := make([]byte, hex.DecodedLen(len(hexdigits)))
		_, err := hex.Decode(ret, hexdigits)
		return ret, err
	}

	// The two queries share repo, commit, and generation, but they're in
	// different positions; decide where they are and don't duplicate the
	// extraction code.
	var err error
	var repoIdx, commitIdx, genIdx int
	if len(elems) == 3 {
		repoIdx, commitIdx, genIdx = 0, 1, 2
	} else if len(elems) == 6 {
		ra.Filename = string(elems[0])
		if ra.Hash, err = decodeBytea(elems[1]); err != nil {
			return err
		}
		ra.HashType = string(elems[2])
		repoIdx, commitIdx, genIdx = 3, 4, 5
	} else {
		return fmt.Errorf("artifact/commit column has %d columns, not 3 or 6",
			len(elems))
	}
	ra.Repo = string(elems[repoIdx])
	if ra.Commit, err = decodeBytea(elems[commitIdx]); err != nil {
		return err
	}
	if ra.Generation, err = strconv.Atoi(string(elems[genIdx])); err != nil {
		return err
	}

	return nil
}

// ReleaseArtifactArray is an alias for a slice of ReleaseArtifact to allow us
// to use pq's array parsing to split the response into individual encoded blobs
// representing ReleaseArtifact objects.
type ReleaseArtifactArray []ReleaseArtifact

// Scan implements the sql.Scanner interface.
func (raa *ReleaseArtifactArray) Scan(src interface{}) error {
	ga := pq.GenericArray{A: raa}
	return ga.Scan(src)
}

// KVMap is an alias for a string->string map, used to represent release
// metadata.
type KVMap map[string]string

// Value implements the driver.Valuer interface.
func (p KVMap) Value() (driver.Value, error) {
	j, err := json.Marshal(p)
	return j, err
}

// Scan implements the sql.Scanner interface.
func (p *KVMap) Scan(src interface{}) error {
	if src == nil {
		return nil
	}

	source, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("Type assertion from %T to []byte failed", src)
	}

	var i interface{}
	err := json.Unmarshal(source, &i)
	if err != nil {
		return err
	}

	// We'll get here with a `null` JSON object.
	if i == nil {
		return nil
	}

	// We can't type-assert directly into a map[string]string, ...
	var j map[string]interface{}
	j, ok = i.(map[string]interface{})
	if !ok {
		return fmt.Errorf("Type assertion from %T to map[string]interface{} failed", i)
	}

	// ... so we copy, type-asserting each value.
	badkeys := []string{}
	for k, v := range j {
		s, ok := v.(string)
		if !ok {
			badkeys = append(badkeys, k)
			continue
		}
		(*p)[k] = s
	}

	if len(badkeys) > 0 {
		if len(badkeys) == 1 {
			return fmt.Errorf("Value for key %q is not a string", badkeys[0])
		} else if len(badkeys) == 2 {
			return fmt.Errorf("Values for keys %q and %q are not strings",
				badkeys[0], badkeys[1])
		} else {
			return fmt.Errorf("Values for keys \"%s\", and %q are not strings",
				strings.Join(badkeys[0:len(badkeys)-1], "\", \""), badkeys[len(badkeys)-1])
		}
	}

	return nil
}

// Release objects represent rows in the releases table, joined with data from
// the artifacts and platforms tables.
type Release struct {
	UUID     uuid.UUID            `db:"release_uuid"`
	Creation time.Time            `db:"create_ts"`
	Platform string               `db:"platform"`
	Commits  ReleaseArtifactArray `db:"commits"`
	Metadata KVMap

	// OnePlatform is a synthetic column created by the query in
	// ListReleases() to indicate whether all the artifacts for a
	// release belong to the same platform.  It shouldn't be used
	// outside that method.
	OnePlatform bool `db:"one_platform"`
}

// ReleaseExistsError is returned when a release with exactly the given
// artifacts already exists.
type ReleaseExistsError struct{}

func (e ReleaseExistsError) Error() string {
	return "a release already exists with these artifacts"
}

// BadReleaseError is returned when a release is found to be self-inconsistent.
type BadReleaseError struct {
	Releases []*Release
}

func (e BadReleaseError) Error() string {
	s := "releases have"
	if len(e.Releases) == 1 {
		s = "release has"
	}
	var uuids []string
	for _, release := range e.Releases {
		uuids = append(uuids, release.UUID.String())
	}
	return fmt.Sprintf("%d %s inconsistent platforms: %s", len(e.Releases), s,
		strings.Join(uuids, ", "))
}

// InsertArtifact adds an artifact to the artifacts table.
func (db *ApplianceDB) InsertArtifact(ctx context.Context, artifact ReleaseArtifact) (*ReleaseArtifact, error) {
	// The four colons in ":commit_hash ::::bytea" allows the query to pass
	// through sqlx's NameMapper and end up with the correct two colons.
	// (See https://github.com/jmoiron/sqlx/issues/91.)  The space allows it
	// to pass through the mapper at all.
	nstmt, err := db.PrepareNamedContext(ctx, `
		INSERT INTO artifacts (
			artifact_uuid, platform_name, repo_name, commit_hash,
			generation, filename, hash, hash_type
		)
		VALUES (
			uuid_generate_v4(), :platform, :repo_name,
			:commit_hash ::::bytea, :generation ::::int, :filename,
			:hash, :hash_type
		)
		RETURNING artifact_uuid`)
	if err != nil {
		return nil, err
	}
	var artifactUUID uuid.UUID
	err = nstmt.GetContext(ctx, &artifactUUID, artifact)

	// If we add an existing artifact, we return an error, even though it's
	// not really an issue, so that the UI has the opportunity to report on
	// it, as desired.  We fetch the artifact's UUID, because the caller
	// will want that, too.
	if pqErr, ok := err.(*pq.Error); ok && pqErr.Code.Name() == "unique_violation" {
		uve := UniqueViolationError{
			Message:    pqErr.Message,
			Detail:     pqErr.Detail,
			Schema:     pqErr.Schema,
			Table:      pqErr.Table,
			Constraint: pqErr.Constraint,
		}
		nstmt, err = db.PrepareNamedContext(ctx, `
			SELECT artifact_uuid
			FROM artifacts
			WHERE
				platform_name = :platform AND
				repo_name = :repo_name AND
				commit_hash = :commit_hash AND
				generation = :generation AND
				filename = :filename AND
				hash = :hash AND
				hash_type = :hash_type`)
		if err != nil {
			return nil, err
		}
		err = nstmt.GetContext(ctx, &artifactUUID, artifact)
		if err != nil {
			return nil, err
		}
		newArtifact := artifact
		newArtifact.UUID = artifactUUID
		return &newArtifact, uve
	}
	if err != nil {
		return nil, err
	}

	// Add the artifact UUID into a copy of the struct and return it.
	newArtifact := artifact
	newArtifact.UUID = artifactUUID
	return &newArtifact, nil
}

// InsertRelease creates a release out of the given artifacts, adding the
// release to the release table and adding the release/artifact mappings to the
// release_artifacts table.  The UUID of the release is returned.  If the same
// set of artifacts is already mapped to a release, no table changes will be
// made, and sql.ErrNoRows will be returned.
func (db *ApplianceDB) InsertRelease(ctx context.Context, artifacts []*ReleaseArtifact, metadata map[string]string) (uuid.UUID, error) {
	if len(artifacts) == 0 {
		return uuid.Nil, fmt.Errorf("Cannot create a release with no artifacts")
	}

	artifactUUIDs := make([]uuid.UUID, len(artifacts))
	for i, artifact := range artifacts {
		artifactUUIDs[i] = artifact.UUID
	}

	// Set up JOIN and WHERE clauses that allow us to check for existing
	// specific combinations of artifacts in the release_artifacts table.
	// Assuming that there's an index on (artifact_uuid, release_uuid), the
	// query should be constant time even with many rows in the table.
	var join bytes.Buffer
	tabs4 := "\n\t\t\t\t"
	tabs5 := tabs4 + "\t"
	tabs6 := tabs5 + "\t"
	tabs7 := tabs6 + "\t"
	for i := range artifacts[1:] {
		fmt.Fprintf(&join, "%sJOIN release_artifacts ra%d USING (release_uuid)", tabs4, i+2)
	}
	fmt.Fprintf(&join, tabs4+"WHERE ")
	for i := range artifacts {
		fmt.Fprintf(&join, "ra%d.artifact_uuid = $1[%d] AND%s", i+1, i+1, tabs5)
	}
	fmt.Fprintf(&join, "NOT EXISTS (%sSELECT FROM release_artifacts rax", tabs6)
	fmt.Fprintf(&join, "%sWHERE rax.release_uuid = ra1.release_uuid AND", tabs6)
	fmt.Fprintf(&join, "%srax.artifact_uuid <> ALL ($1)%s)", tabs7, tabs5)

	// We have three CTEs to set up the insertion into the release_artifacts
	// table.  The first just sets up the incoming artifact UUIDs as a
	// table.
	//
	// The second tests to see if the particular combination of artifacts
	// already exists as a release (for the details on how it works, see
	// https://stackoverflow.com/questions/56534152).  It sets a column in a
	// one-row table to the boolean representing that fact.  (It's possible
	// we could use a recursive CTE to avoid all the query building above,
	// but that might be more complex than necessary.)
	//
	// The third CTE generates the release UUID and inserts that into the
	// releases table if and only if the boolean from the first CTE is true.
	// If the insertion happens, the new UUID is stored in the temporary
	// table.
	//
	// The insertion itself is straightforward, joining the three tables
	// from the three CTEs to get the release and artifact UUIDs, plus
	// whether or not to do it at all.  And finally returning the release
	// UUID.
	q := `
		WITH art_val (artifact_uuid) AS (
			SELECT unnest($1::uuid[])
		), existing (tf) AS (
			SELECT EXISTS (
				SELECT 1
				FROM release_artifacts ra1` + join.String() + `
			)
		), rel_uuid (new_uuid) AS (
			INSERT INTO releases (release_uuid, metadata) (
				SELECT uuid, meta FROM (
					VALUES (uuid_generate_v4(), $2::jsonb)
				) AS junk(uuid, meta), existing
				WHERE NOT existing.tf
			)
			RETURNING release_uuid
		)
		INSERT INTO release_artifacts (release_uuid, artifact_uuid) (
			SELECT rel_uuid.new_uuid, art_val.artifact_uuid
			FROM rel_uuid, art_val, existing
			WHERE NOT existing.tf
		)
		RETURNING release_uuid
	`

	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return uuid.Nil, err
	}
	var releaseUUID uuid.UUID
	err = db.GetContext(ctx, &releaseUUID, q, pq.Array(artifactUUIDs), metaJSON)
	err = mkSyntaxError(err, q)

	if err != nil {
		if err == sql.ErrNoRows {
			err = ReleaseExistsError{}
		}
		return uuid.Nil, err
	}

	return releaseUUID, err
}

func filterSlice(slice *[]*Release, filter func(int) bool) []*Release {
	var out []*Release

	for i := 0; i < len(*slice); i++ {
		if filter(i) {
			out = append(out, (*slice)[i])
			(*slice)[i] = (*slice)[0]
			*slice = (*slice)[1:]
			i--
		}
	}

	return out
}

// ListReleases returns the releases in the database, along with some of their
// core metadata.
func (db *ApplianceDB) ListReleases(ctx context.Context) ([]*Release, error) {
	// min(p.name)=max(p.name) asserts that all the platforms are identical,
	// and min(p.name) gives us that name. I might be able to create a
	// custom aggregation function to emit the unique name or null, but that
	// seems like overkill.
	//
	// The commits come back as an array of "tuples" (rows).  There's no
	// supported way to scan this directly; see the Scan() methods of the
	// ReleaseArtifact and ReleaseArtifactArray types to see how we do this.
	q := `
		SELECT r.release_uuid,
			r.create_ts,
			r.metadata,
			min(p.name)=max(p.name) AS one_platform,
			min(p.name) AS platform,
			array_agg(DISTINCT (a.repo_name, a.commit_hash, a.generation)) AS commits
		FROM releases r
			JOIN release_artifacts ra ON r.release_uuid = ra.release_uuid
			JOIN artifacts a ON ra.artifact_uuid = a.artifact_uuid
			JOIN platforms p ON a.platform_name = p.name
		GROUP BY r.release_uuid
		ORDER BY r.create_ts
	`
	var releases []*Release
	err := db.SelectContext(ctx, &releases, q)
	if err != nil {
		return nil, err
	}

	// If any release is inconsistent in its platform (this should never
	// happen, but the database won't prevent it), pull those releases out
	// of the response.  Return both the (set of good) releases as well as
	// an error that tells the caller that something's up.
	platCheck := func(i int) bool { return !releases[i].OnePlatform }
	badReleases := filterSlice(&releases, platCheck)
	if len(badReleases) > 0 {
		err = BadReleaseError{Releases: badReleases}
	}

	return releases, err
}

// GetRelease gets the details of a release, sufficient to build an upgrade
// descriptor that ap-factory can use.  The placeholder nil release is special,
// and has no associated artifacts; the return for that release is nil, but also
// a nil error.
func (db *ApplianceDB) GetRelease(ctx context.Context, relUU uuid.UUID) (*Release, error) {
	if relUU == uuid.Nil {
		return nil, nil
	}

	q := `
		SELECT r.release_uuid,
			r.create_ts,
			r.metadata,
			min(p.name)=max(p.name) AS one_platform,
			min(p.name) AS platform,
			unnest(array_agg(p.name)) AS platform,
			array_agg(DISTINCT (
				a.filename, a.hash, a.hash_type,
				a.repo_name, a.commit_hash, a.generation
			)) AS commits
		FROM releases r
			JOIN release_artifacts ra ON r.release_uuid = ra.release_uuid
			JOIN artifacts a ON ra.artifact_uuid = a.artifact_uuid
			JOIN platforms p ON a.platform_name = p.name
		WHERE r.release_uuid = $1
		GROUP BY r.release_uuid
	`

	var release Release
	err := db.GetContext(ctx, &release, q, relUU)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{"GetRelease: Couldn't find release"}
	case nil:
		return &release, nil
	default:
		panic(err)
	}
}

// GetCurrentRelease gets the release which we think an appliance is currently
// running.
func (db *ApplianceDB) GetCurrentRelease(ctx context.Context, appUU uuid.UUID) (uuid.UUID, error) {
	var relUU uuid.UUID
	err := db.GetContext(ctx, &relUU, `
		SELECT release_uuid
		FROM appliance_release_history
		WHERE appliance_uuid = $1
		ORDER BY updated_ts DESC
		LIMIT 1`,
		appUU)
	switch err {
	case sql.ErrNoRows:
		return uuid.Nil, NotFoundError{fmt.Sprintf(
			"GetCurrentRelease: Couldn't find appliance for %v", appUU)}
	case nil:
		return relUU, nil
	default:
		panic(err)
	}
}

// SetCurrentRelease sets the release which we think an appliance is currently
// running.
func (db *ApplianceDB) SetCurrentRelease(ctx context.Context, appUU, relUU uuid.UUID,
	ts time.Time, commits map[string]string) error {
	// XXX If the commits match a release and relUU is uuid.Nil, we could
	// set relUU to the real value.  This has diminished value until
	// appliances start reporting VUB versions.
	//
	// XXX If the commits don't match a release and relUU isn't uuid.Nil, we
	// could set relUU to uuid.Nil.  Note that this would be actively
	// counterproductive until appliances start reporting VUB versions.
	//
	// XXX If an appliance reboots into an older release, we won't ever
	// record the fact that it's fallen back.
	commitJSON, err := json.Marshal(commits)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, `
		WITH c AS (
			SELECT release_uuid = $2 AS success
			FROM appliance_release_targets
			WHERE appliance_uuid = $1
		)
		INSERT INTO appliance_release_history (
			appliance_uuid, release_uuid, updated_ts, stage, success, repo_commits
		)
		VALUES ($1, $2, $3, 'complete', (SELECT success FROM c), $4::jsonb)
		ON CONFLICT (appliance_uuid, release_uuid, stage) DO
			UPDATE SET (updated_ts, success, repo_commits) = (
				EXCLUDED.updated_ts, EXCLUDED.success, EXCLUDED.repo_commits
			) WHERE appliance_release_history.success IS DISTINCT FROM EXCLUDED.success OR
				appliance_release_history.repo_commits IS DISTINCT FROM EXCLUDED.repo_commits`,
		appUU, relUU, ts, commitJSON)
	if pqErr, ok := err.(*pq.Error); ok && pqErr.Code.Name() == "foreign_key_violation" {
		var m string
		switch pqErr.Constraint {
		case "appliance_release_history_appliance_uuid_fkey":
			m = fmt.Sprintf("Unknown appliance UUID %s", appUU)
		case "appliance_release_history_release_uuid_fkey":
			m = fmt.Sprintf("Unknown release UUID %s", relUU)
		default:
			m = fmt.Sprintf("Unexpected constraint %s violated in table %s: %s",
				pqErr.Constraint, pqErr.Table, pqErr.Detail)
		}
		return ForeignKeyError{
			simpleMessage: m,
			Message:       pqErr.Message,
			Detail:        pqErr.Detail,
			Schema:        pqErr.Schema,
			Table:         pqErr.Table,
			Constraint:    pqErr.Constraint,
		}
	}
	return err
}

// SetUpgradeStage records what stage the upgrade has completed, its success or
// failure, and any message associated with it.  For the "complete" stage, use
// SetCurrentRelease, and for the "installed" stage, use SetUpgradeResults.
func (db *ApplianceDB) SetUpgradeStage(ctx context.Context, appUU, relUU uuid.UUID, ts time.Time, stage string, success bool, msg string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO appliance_release_history (
			appliance_uuid, release_uuid, updated_ts, stage, success
		)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (appliance_uuid, release_uuid, stage) DO
			UPDATE SET (updated_ts, success, message) = (EXCLUDED.updated_ts, EXCLUDED.success, EXCLUDED.message)`,
		appUU, relUU, ts, stage, success)
	if pqErr, ok := err.(*pq.Error); ok && pqErr.Code.Name() == "foreign_key_violation" {
		var m string
		switch pqErr.Constraint {
		case "appliance_release_history_appliance_uuid_fkey":
			m = fmt.Sprintf("Unknown appliance UUID %s", appUU)
		case "appliance_release_history_release_uuid_fkey":
			m = fmt.Sprintf("Unknown release UUID %s", relUU)
		default:
			m = fmt.Sprintf("Unexpected constraint %s violated in table %s: %s",
				pqErr.Constraint, pqErr.Table, pqErr.Detail)
		}
		return ForeignKeyError{
			simpleMessage: m,
			Message:       pqErr.Message,
			Detail:        pqErr.Detail,
			Schema:        pqErr.Schema,
			Table:         pqErr.Table,
			Constraint:    pqErr.Constraint,
		}
	}
	return err
}

// GetTargetRelease gets the release to which an appliance is expected to
// upgrade.
func (db *ApplianceDB) GetTargetRelease(ctx context.Context, appUU uuid.UUID) (uuid.UUID, error) {
	var relUU uuid.UUID
	err := db.GetContext(ctx, &relUU, `
		SELECT release_uuid
		FROM appliance_release_targets
		WHERE appliance_uuid = $1`,
		appUU)
	switch err {
	case sql.ErrNoRows:
		return uuid.Nil, NotFoundError{fmt.Sprintf(
			"GetTargetRelease: Couldn't find appliance for %v", appUU)}
	case nil:
		return relUU, nil
	default:
		panic(err)
	}
}

// SetTargetRelease sets the release to which an appliance is expected to
// upgrade.
func (db *ApplianceDB) SetTargetRelease(ctx context.Context, appUU, relUU uuid.UUID) error {
	// XXX It would be nice to error out if the given release wasn't the
	// right platform for the appliance, but we don't keep track of the
	// latter information.
	_, err := db.ExecContext(ctx, `
		INSERT INTO appliance_release_targets (appliance_uuid, release_uuid)
			VALUES ($1, $2)
		ON CONFLICT (appliance_uuid) DO UPDATE
		SET (release_uuid) = (EXCLUDED.release_uuid)`,
		appUU, relUU)
	if pqErr, ok := err.(*pq.Error); ok && pqErr.Code.Name() == "foreign_key_violation" {
		var m string
		switch pqErr.Constraint {
		case "appliance_release_targets_appliance_uuid_fkey":
			m = fmt.Sprintf("Unknown appliance UUID %s", appUU)
		case "appliance_release_targets_release_uuid_fkey":
			m = fmt.Sprintf("Unknown release UUID %s", relUU)
		default:
			m = fmt.Sprintf("Unexpected constraint %s violated in table %s: %s",
				pqErr.Constraint, pqErr.Table, pqErr.Detail)
		}
		return ForeignKeyError{
			simpleMessage: m,
			Message:       pqErr.Message,
			Detail:        pqErr.Detail,
			Schema:        pqErr.Schema,
			Table:         pqErr.Table,
			Constraint:    pqErr.Constraint,
		}
	}
	return err
}

// SetUpgradeResults stores a short error message, if any, and a pointer to the
// log for an appliance's upgrade procedure (the part before the reboot).
func (db *ApplianceDB) SetUpgradeResults(ctx context.Context, ts time.Time,
	appUU, relUU uuid.UUID, success bool, upgradeErr sql.NullString, logURL string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO appliance_release_history (
			appliance_uuid, release_uuid, updated_ts, stage, success, message, log_url
		)
		VALUES ($1, $2, $3, 'installed', $4, $5, $6)
		ON CONFLICT (appliance_uuid, release_uuid, stage) DO UPDATE
		SET (updated_ts, success, message, log_url) = (
			EXCLUDED.updated_ts, EXCLUDED.success, EXCLUDED.message, EXCLUDED.log_url
		)`,
		appUU, relUU, ts, success, upgradeErr, logURL)
	if pqErr, ok := err.(*pq.Error); ok && pqErr.Code.Name() == "foreign_key_violation" {
		var m string
		switch pqErr.Constraint {
		case "appliance_release_history_appliance_uuid_fkey":
			m = fmt.Sprintf("Unknown appliance UUID %s", appUU)
		case "appliance_release_history_release_uuid_fkey":
			m = fmt.Sprintf("Unknown release UUID %s", relUU)
		default:
			m = fmt.Sprintf("Unexpected constraint %s violated in table %s: %s",
				pqErr.Constraint, pqErr.Table, pqErr.Detail)
		}
		return ForeignKeyError{
			simpleMessage: m,
			Message:       pqErr.Message,
			Detail:        pqErr.Detail,
			Schema:        pqErr.Schema,
			Table:         pqErr.Table,
			Constraint:    pqErr.Constraint,
		}
	}
	return err
}

// ApplianceReleaseStatus represents the join of the appliance_release_targets
// and appliance_release_history for an individual appliance.
type ApplianceReleaseStatus struct {
	CurrentReleaseUUID uuid.NullUUID
	CurrentReleaseName sql.NullString
	RunningSince       null.Time
	TargetReleaseUUID  uuid.NullUUID
	TargetReleaseName  sql.NullString
	Commits            KVMap
	Stage              sql.NullString
	Success            sql.NullBool
	Message            sql.NullString
	LogURL             sql.NullString
}

// GetReleaseStatusByAppliances returns information about what appliances are
// running or targeted to run what release.
func (db *ApplianceDB) GetReleaseStatusByAppliances(ctx context.Context, appUUs []uuid.UUID) (
	map[uuid.UUID]ApplianceReleaseStatus, error) {
	q := `
		SELECT DISTINCT ON (appliance_uuid)
			m.appliance_uuid,
			c.release_uuid, rc.metadata->>'name', c.updated_ts,
			t.release_uuid, rt.metadata->>'name', c.repo_commits,
			c.stage, c.success, c.message, c.log_url
		FROM (
			appliance_release_targets t
				INNER JOIN releases rt USING (release_uuid)
		) FULL OUTER JOIN (
			appliance_release_history c
				INNER JOIN releases rc USING (release_uuid)
		) USING (appliance_uuid)
			INNER JOIN appliance_id_map m
				ON m.appliance_uuid = c.appliance_uuid OR
				   m.appliance_uuid = t.appliance_uuid
		%s
		ORDER BY appliance_uuid, updated_ts desc
	`

	// The usual $1::uuid IS NULL OR m.appliance_uuid IN (?) trick doesn't
	// work because sqlx.In doesn't like it if we pass in an empty array.
	var args []interface{}
	if len(appUUs) > 0 {
		q = fmt.Sprintf(q, `WHERE m.appliance_uuid IN (?)`)
		var err error
		q, args, err = sqlx.In(q, appUUs)
		if err != nil {
			return nil, err
		}
		q = db.Rebind(q)
	} else {
		q = fmt.Sprintf(q, "")
	}

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ret := make(map[uuid.UUID]ApplianceReleaseStatus)
	for rows.Next() {
		var appUU uuid.UUID
		var curUU, targUU uuid.NullUUID
		var curName, targName, stage, message, logurl sql.NullString
		var success sql.NullBool
		var curTime null.Time
		commits := make(KVMap)
		err = rows.Scan(&appUU, &curUU, &curName, &curTime,
			&targUU, &targName, &commits, &stage, &success,
			&message, &logurl)
		if err != nil {
			return nil, err
		}
		ret[appUU] = ApplianceReleaseStatus{
			CurrentReleaseUUID: curUU,
			CurrentReleaseName: curName,
			RunningSince:       curTime,
			TargetReleaseUUID:  targUU,
			TargetReleaseName:  targName,
			Commits:            commits,
			Stage:              stage,
			Success:            success,
			Message:            message,
			LogURL:             logurl,
		}
	}

	return ret, nil
}
