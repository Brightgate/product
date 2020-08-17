/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package modeldb

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	// sql driver
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
	"golang.org/x/crypto/sha3"
)

// DataStore represents the modeldb database.  This interface also facilitates
// mocking the database See
// http://www.alexedwards.net/blog/organising-database-access
type DataStore interface {
	CheckDB() error
	GetModels() ([]RecordedClassifier, error)
	UpsertModel(RecordedClassifier) error
	Close() error
}

// SQLiteModelDB satisifies the DataStore interface, and represents a SQLite
// database containing models.
type SQLiteModelDB struct {
	*sqlx.DB
}

func getShake256(schema string) string {
	buf := []byte(schema)
	h := make([]byte, 64)
	sha3.ShakeSum256(h, buf)
	return fmt.Sprintf("%x", h)
}

func checkTableSchema(db *sqlx.DB, tname string, tschema string, verb string) error {
	tschemaHash := getShake256(tschema)

	_, err := db.Exec(tschema)
	if err != nil {
		return errors.Wrapf(err, "could not create '%s' table", tname)
	}

	// Check that schema matches what we expect.  If not, we
	// complain.
	row := db.QueryRow("SELECT table_name, schema_hash, create_date FROM version WHERE table_name = $1;", tname)

	var name, schemaHash string
	var creationDate time.Time

	err = row.Scan(&name, &schemaHash, &creationDate)

	if err == sql.ErrNoRows {
		// Not present case.  Insert.
		_, err := db.Exec("INSERT INTO version (table_name, schema_hash, create_date) VALUES ($1, $2, $3)", tname, tschemaHash, time.Now().UTC())
		if err != nil {
			return errors.Wrap(err, "insert version failed")
		}
		return nil
	}

	if err != nil {
		return errors.Wrap(err, "scan error")
	}

	// Mismatch.
	if tschemaHash != schemaHash {
		return errors.Errorf("tname %s tschema %s; name %s, schema %s, create %v\nschema hash mismatch for '%s'; delete and re-%s",
			tname, tschemaHash, name, schemaHash, creationDate, tname, verb)
	}
	return nil
}

// CheckDB tests whether modeldb database looks as we expect.
// Classifier levels are ordered so that we can train new classifiers
// without impacting the production output.
// XXX: maybe fold this into the open?
func (db *SQLiteModelDB) CheckDB() error {
	const versionSchema = `
    CREATE TABLE IF NOT EXISTS version (
	table_name TEXT PRIMARY KEY,
	schema_hash TEXT,
	create_date TIMESTAMP
    );`

	_, err := db.Exec(versionSchema)
	if err != nil {
		return errors.Wrap(err, "could not create version table")
	}

	const modelSchema = `
    CREATE TABLE IF NOT EXISTS model (
	generation_date TIMESTAMP,
	name TEXT PRIMARY KEY,
	classifier_type TEXT,
	classifier_level INTEGER,
	multibayes_min INTEGER,
	certain_above FLOAT,
	uncertain_below FLOAT,
	model_json TEXT
    );`
	return checkTableSchema(db.DB, "model", modelSchema, "train")
}

// RecordedClassifier represents an entry in model table. Each entry
// represents an active classifier and its trained implementation, where
// appropriate.
type RecordedClassifier struct {
	GenerationTS    time.Time `db:"generation_date"`
	ModelName       string    `db:"name"`
	ClassifierType  string    `db:"classifier_type"`
	ClassifierLevel int       `db:"classifier_level"`
	MultibayesMin   int       `db:"multibayes_min"`
	CertainAbove    float64   `db:"certain_above"`
	UncertainBelow  float64   `db:"uncertain_below"`
	ModelJSON       string    `db:"model_json"`
}

// GetModels returns an array of RecordedClassifier structures extracted
// from the database.
func (db *SQLiteModelDB) GetModels() ([]RecordedClassifier, error) {
	models := make([]RecordedClassifier, 0)

	// For reporting, we restrict based on the readiness level.
	err := db.Select(&models, "SELECT * FROM model ORDER BY name ASC")
	if err != nil {
		return nil, errors.Wrap(err, "model select failed")
	}

	return models, nil
}

// UpsertModel inserts or updates a model in the database.
func (db *SQLiteModelDB) UpsertModel(r RecordedClassifier) error {
	_, err := db.Exec(`
		INSERT OR REPLACE INTO model
			(generation_date, name, classifier_type, classifier_level, multibayes_min, certain_above, uncertain_below, model_json)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8);`,
		r.GenerationTS,
		r.ModelName,
		r.ClassifierType,
		r.ClassifierLevel,
		r.MultibayesMin,
		r.CertainAbove,
		r.UncertainBelow,
		r.ModelJSON)
	if err != nil {
		return errors.Wrapf(err, "could not update '%s' model", r.ModelName)
	}
	return nil
}

// OpenSQLite opens the SQLite database named by modelPath
func OpenSQLite(modelPath string) (DataStore, error) {
	db, err := sqlx.Connect("sqlite3", modelPath)
	if err != nil {
		return nil, errors.Wrapf(err, "model database %s open", modelPath)
	}
	m := &SQLiteModelDB{
		DB: db,
	}
	return m, nil
}

