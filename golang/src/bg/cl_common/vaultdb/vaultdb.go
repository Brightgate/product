/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Package vaultdb contains routines to help a Vault client authenticate to a
// database.
package vaultdb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"bg/cl_common/daemonutils"
	"bg/cl_common/pgutils"

	vault "github.com/hashicorp/vault/api"
	"github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/tevino/abool"
)

// Logger is a basic logging interface.
type Logger interface {
	Debugf(string, ...interface{})
	Infof(string, ...interface{})
	Warnf(string, ...interface{})
	Errorf(string, ...interface{})
	Fatalf(string, ...interface{})
	Panicf(string, ...interface{})
}

func getDBRoleTTL(vaultClient *vault.Client, path, role string, log Logger) (time.Duration, error) {
	zero := time.Duration(0)
	if path == "" || role == "" {
		return zero, errors.New("either path or role are empty")
	}

	vcl := vaultClient.Logical()
	rolePath := fmt.Sprintf("%s/roles/%s", path, role)
	roles, err := vcl.Read(rolePath)
	if err != nil {
		return zero, errors.Wrapf(err, "unable to read DB role data from '%s'",
			rolePath)
	}
	if roles == nil {
		return zero, errors.Errorf("no database role found at '%s'", rolePath)
	}
	if roles.Warnings != nil {
		log.Warnf("vault returned warnings: %v", roles.Warnings)
	}
	if roles.Data == nil {
		return zero, errors.Errorf("no data in role at '%s'", rolePath)
	}

	maxTTL, err := roles.Data["max_ttl"].(json.Number).Int64()
	if err != nil {
		return zero, errors.Wrapf(err, "unable to convert max_ttl JSON value")
	}
	return time.Duration(int(maxTTL)) * time.Second, nil
}

func getDBCreds(vaultClient *vault.Client, path, role string, log Logger) (*vault.Secret, error) {
	if path == "" || role == "" {
		return nil, errors.New("either path or role are empty")
	}

	vcl := vaultClient.Logical()
	credPath := fmt.Sprintf("%s/creds/%s", path, role)
	creds, err := vcl.Read(credPath)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read DB credentials from '%s'",
			credPath)
	}
	if creds == nil {
		return nil, errors.Errorf("no database credentials found at '%s'", credPath)
	}
	if creds.Warnings != nil {
		log.Warnf("vault returned warnings: %v", creds.Warnings)
	}
	if creds.Data == nil {
		return nil, errors.Errorf("no data in credentials at '%s'", credPath)
	}

	return creds, nil
}

func addDBCreds(dbURI string, creds *vault.Secret) string {
	username := creds.Data["username"].(string)
	password := creds.Data["password"].(string)
	if username != "" && password != "" {
		dbURI = pgutils.AddUsername(dbURI, username)
		dbURI = pgutils.AddPassword(dbURI, password)
	}
	return dbURI
}

// A Connector is a database/sql/driver.Connector, representing a way for
// a database client to connect to a database using Vault, and handle rotating
// credentials.
type Connector struct {
	connectURI  string
	vaultClient *vault.Client
	path        string
	role        string // Vault role
	log         Logger
	watcher     *vault.LifetimeWatcher
	creds       *vault.Secret
	l           sync.Mutex
	notifier    chan struct{} // tells us to reauthenticate to Vault
	needCreds   *abool.AtomicBool
	watchHUP    chan struct{}
}

// NewConnector returns a Connector object giving the caller access to the
// database specified in `uri`, using credentials acquired from Vault via
// `vaultClient`.  It handles both database credential rotation, as well as
// Vault authentication token renewal (via `notifier`, acquired from a custom
// Vault authentication package).  The database secrets engine is mounted at
// `path`, and the Vault role for the database used by this connector is named
// by `role`.
//
// If the vault client is nil, or either `path` or `role` are empty, the
// function will panic.
func NewConnector(uri string, vaultClient *vault.Client, notifier *daemonutils.FanOut, path, role string, log Logger) *Connector {
	// Handle some basic, will-never-work issues.
	if vaultClient == nil {
		log.Panicf("vault.Client parameter is nil")
	}
	if path == "" || role == "" {
		log.Panicf("either path or role parameter is empty")
	}

	var creds string
	if pgutils.HasUsername(uri) {
		creds = "username"
	}

	if pgutils.HasPassword(uri) {
		if creds == "" {
			creds = "password"
		} else {
			creds = creds + " and password"
		}
	}

	if creds != "" {
		log.Warnf("Database URI accessed through Vault specifies "+
			"%s, which will be overridden by values retrieved from Vault: "+
			"'%s'", creds, pgutils.CensorPassword(uri))
	}

	var notifierChannel chan struct{}
	if notifier != nil {
		notifierChannel = notifier.AddReceiver()
	}

	c := &Connector{
		connectURI:  uri,
		vaultClient: vaultClient,
		path:        path,
		role:        role,
		log:         log,
		notifier:    notifierChannel,
		needCreds:   abool.NewBool(true),
		watchHUP:    make(chan struct{}),
	}
	go c.waitWatcher()
	return c
}

func (c *Connector) getWatcher() (*vault.LifetimeWatcher, error) {
	return c.vaultClient.NewLifetimeWatcher(
		&vault.LifetimeWatcherInput{Secret: c.creds})
}

func (c *Connector) waitWatcher() {
	for {
		var doneCh <-chan error
		var renewCh <-chan *vault.RenewOutput
		c.l.Lock()
		if c.watcher != nil {
			doneCh = c.watcher.DoneCh()
			renewCh = c.watcher.RenewCh()
		}
		c.l.Unlock()

		select {
		case <-c.notifier:
			c.log.Infof("Couldn't renew lease '%s' any longer (auth token replaced)",
				c.creds.LeaseID)
			c.watcher.Stop()
			c.needCreds.Set()

		case err := <-doneCh:
			base := "Couldn't renew lease '%s' any longer"
			if err != nil {
				c.log.Errorf(base+": %v", c.creds.LeaseID, err)
			} else {
				c.log.Infof(base, c.creds.LeaseID)
			}
			c.watcher.Stop()
			c.needCreds.Set()

		case renewal := <-renewCh:
			warnStr := ""
			if len(renewal.Secret.Warnings) > 0 {
				warnStr = fmt.Sprintf("; warnings: %s",
					renewal.Secret.Warnings)
			}
			c.log.Infof("Got a renewal for '%s' for %s%s",
				renewal.Secret.LeaseID,
				time.Duration(renewal.Secret.LeaseDuration)*time.Second,
				warnStr)

		case <-c.watchHUP:
			// This is just a signal to restart the loop, because
			// c.watcher has changed.
		}
	}
}

func (c *Connector) getCreds(dbURI string) error {
	c.l.Lock()
	// We can't defer the unlock because we need to unlock explicitly before
	// sending to watchHUP, and then we'd panic when unlocking an unlocked
	// mutex.

	if !c.needCreds.IsSet() {
		c.l.Unlock()
		return nil
	}

	var err error
	creds, err := getDBCreds(c.vaultClient, c.path, c.role, c.log)
	if err != nil {
		c.l.Unlock()
		return err
	}
	safeURI := pgutils.CensorPassword(dbURI)
	c.log.Infof("Got DB Credentials for '%s' (username=%s)",
		safeURI, creds.Data["username"].(string))
	c.log.Debugf("Credentials are under lease '%s' for %s",
		creds.LeaseID,
		time.Duration(creds.LeaseDuration)*time.Second)
	c.creds = creds

	watcher, err := c.getWatcher()
	if err != nil {
		c.log.Panicf("Unable to watch for secret rotation: %v", err)
	}
	c.watcher = watcher
	go watcher.Start()
	c.needCreds.UnSet()

	// Restart the loop so that c.watcher can be re-evaluated, since
	// it will have changed.  We have to drop the lock first, or we could
	// end up in a deadlock.
	c.l.Unlock()
	c.watchHUP <- struct{}{}

	return nil
}

func (c *Connector) username() string {
	return c.creds.Data["username"].(string)
}

func (c *Connector) password() string {
	return c.creds.Data["password"].(string)
}

// Connect implements the Connect() method in the Connector interface.  It
// fetches credentials from Vault, as needed, and uses them to log in over a new
// connection.
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	// Force all sessions to operate in UTC, so we don't rely on whatever
	// weird timezone is configured on the server, like GMT.
	dbURI := pgutils.AddTimezone(c.connectURI, "UTC")

	if err := c.getCreds(dbURI); err != nil {
		return nil, err
	}

	dbURI = addDBCreds(dbURI, c.creds)
	pqConnector, err := pq.NewConnector(dbURI)
	if err != nil {
		return nil, err
	}
	return pqConnector.Connect(ctx)
}

// Driver implements the Driver() method in the Connector interface, returning
// the underlying driver.
func (c *Connector) Driver() driver.Driver {
	return &pq.Driver{}
}

// SetConnMaxLifetime sets the maximum amount of time a connection may be
// reused, based on the maximum TTL of the credentials in Vault.  This can
// return error if there's a failure retrieving the data from Vault.
func (c *Connector) SetConnMaxLifetime(db *sql.DB) error {
	var maxTTL time.Duration
	var err error
	if maxTTL, err = getDBRoleTTL(c.vaultClient, c.path, c.role, c.log); err != nil {
		return err
	}
	db.SetConnMaxLifetime(maxTTL)
	return nil
}
