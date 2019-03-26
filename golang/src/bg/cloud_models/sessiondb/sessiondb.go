/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package sessiondb

import (
	"context"
	"database/sql"
	"io/ioutil"
	"path/filepath"

	"github.com/pkg/errors"

	// As per pq directions; causes it to register properly
	_ "github.com/lib/pq"
)

// DataStore facilitates mocking the database
// See http://www.alexedwards.net/blog/organising-database-access
type DataStore interface {
	LoadSchema(context.Context, string) error
	GetPG() *sql.DB
	Ping() error
	PingContext(context.Context) error
	Close() error
}

// SessionDB implements DataStore with the actual DB backend
// sql.DB takes care of Ping() and Close().
type SessionDB struct {
	*sql.DB
}

// Connect opens a new connection to the DataStore
func Connect(dataSource string, allowCreate bool) (DataStore, error) {
	sqldb, err := sql.Open("postgres", dataSource)
	if err != nil {
		return nil, err
	}
	// We found that not limiting this can cause problems as Go attempts to
	// open many many connections to the database.  (presumably the cloud
	// sql proxy can't handle massive numbers of connections)
	sqldb.SetMaxOpenConns(16)

	if !allowCreate {
		var exists string
		row := sqldb.QueryRow("SELECT to_regclass('public.http_sessions');")
		err = row.Scan(&exists)
		if err != nil {
			return nil, errors.Wrap(err, "error testing if table exists")
		}
		if exists != "http_sessions" {
			return nil, errors.Errorf("http_sessions table does not exist: '%s'", exists)
		}
	}

	var ds DataStore = &SessionDB{sqldb}
	return ds, nil
}

// GetPG returns the underlying *sql.DB
func (db *SessionDB) GetPG() *sql.DB {
	return db.DB
}

// LoadSchema loads the SQL schema files from a directory.  ioutil.ReadDir sorts
// the input, ensuring the schema is loaded in the right sequence.
// XXX: Not sure this is the right interface in the right place.  Possibly an
// array of io.Readers would be better?
func (db *SessionDB) LoadSchema(ctx context.Context, schemaDir string) error {
	files, err := ioutil.ReadDir(schemaDir)
	if err != nil {
		return errors.Wrap(err, "could not scan schema dir")
	}

	for _, file := range files {
		bytes, err := ioutil.ReadFile(filepath.Join(schemaDir, file.Name()))
		if err != nil {
			return errors.Wrap(err, "failed to read sql")
		}
		_, err = db.ExecContext(ctx, string(bytes))
		if err != nil {
			return errors.Wrap(err, "failed to exec sql")
		}
	}
	return nil
}
