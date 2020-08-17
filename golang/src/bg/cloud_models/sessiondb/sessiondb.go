/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package sessiondb

import (
	"context"
	"database/sql"
	"io/ioutil"
	"path/filepath"

	"bg/cl_common/vaultdb"

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

// SessionDB implements DataStore with the actual DB backend.
type SessionDB struct {
	*sql.DB
}

func connectPost(sqldb *sql.DB, allowCreate bool) (DataStore, error) {
	// We found that not limiting this can cause problems as Go attempts to
	// open many many connections to the database.  (presumably the cloud
	// sql proxy can't handle massive numbers of connections)
	sqldb.SetMaxOpenConns(16)

	if !allowCreate {
		var exists sql.NullString
		row := sqldb.QueryRow("SELECT to_regclass('public.http_sessions');")
		err := row.Scan(&exists)
		if err != nil {
			return nil, errors.Wrap(err, "error testing if table exists")
		}
		if !exists.Valid {
			return nil, errors.Errorf("http_sessions table does "+
				"not exist: '%s'", exists.String)
		}
	}

	var ds DataStore = &SessionDB{sqldb}
	return ds, nil
}

// Connect opens a new connection to the DataStore
func Connect(dataSource string, allowCreate bool) (DataStore, error) {
	sqldb, err := sql.Open("postgres", dataSource)
	if err != nil {
		return nil, err
	}
	return connectPost(sqldb, allowCreate)
}

// VaultConnect takes an existing VaultDB object, opens the connection, and
// creates a DataStore from it.
func VaultConnect(vdbc *vaultdb.Connector, allowCreate bool) (DataStore, error) {
	sqldb := sql.OpenDB(vdbc)
	if err := vdbc.SetConnMaxLifetime(sqldb); err != nil {
		return nil, err
	}
	return connectPost(sqldb, allowCreate)
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

