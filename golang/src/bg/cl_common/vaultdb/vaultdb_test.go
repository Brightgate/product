/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package vaultdb

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"bg/common/briefpg"

	vaultapi "github.com/hashicorp/vault/api"
	logicalDb "github.com/hashicorp/vault/builtin/logical/database"
	vaulthttp "github.com/hashicorp/vault/http"
	"github.com/hashicorp/vault/sdk/logical"
	"github.com/hashicorp/vault/vault"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// testVaultServer is based largely on testVaultServerCoreConfig from
// command/command_test.go in the vault repo.
func testVaultServer(t *testing.T) (*vaultapi.Client, func()) {
	coreConfig := &vault.CoreConfig{
		DisableMlock: true,
		DisableCache: true,
		LogicalBackends: map[string]logical.Factory{
			"database": logicalDb.Factory,
		},
	}

	cluster := vault.NewTestCluster(t, coreConfig, &vault.TestClusterOptions{
		HandlerFunc: vaulthttp.Handler,
		NumCores:    1,
	})
	cluster.Start()

	core := cluster.Cores[0].Core
	vault.TestWaitActive(t, core)

	client := cluster.Cores[0].Client
	client.SetToken(cluster.RootToken)

	return client, func() { defer cluster.Cleanup() }
}

func TestDBSecrets(t *testing.T) {
	var ctx = context.Background()
	assert := require.New(t)

	vc, vStop := testVaultServer(t)
	defer vStop()

	// Set up the database
	bpg := briefpg.New(nil)
	if err := bpg.Start(ctx); err != nil {
		t.Fatalf("Failed to start Postgres: %v", err)
	}
	defer bpg.Fini(ctx)

	dbName := "junkydb"
	dbURI, err := bpg.CreateDB(ctx, dbName, "")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	mi := &vaultapi.MountInput{
		Type: "database",
	}
	path := "database/" + dbName
	vc.Sys().Mount(path, mi)

	// Configure the database plugin
	vcl := vc.Logical()
	role := "myrole"
	_, err = vcl.Write(path+"/config/db", map[string]interface{}{
		"plugin_name":    "postgresql-database-plugin",
		"allowed_roles":  role,
		"connection_url": dbURI,
	})
	if err != nil {
		t.Fatalf("Failed to configure DB engine in Vault: %v", err)
	}

	// Use the database via Vault
	vdbc := NewConnector(dbURI, vc, path, role, zaptest.NewLogger(t).Sugar())
	if err != nil {
		t.Fatalf("Failed to create VaultDB connector: %v", err)
	}
	db := sql.OpenDB(vdbc)
	// This combination is intended to indicate that each statement uses a
	// brand new connection, and that connections won't be reused.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	// This requires the role to be configured, so will return an error.
	err = vdbc.SetConnMaxLifetime(db)
	assert.Error(err)

	// This will attempt to open a connection, thus read creds from vault,
	// thus fail because the role isn't configured.
	err = db.Ping()
	assert.Error(err)

	// Create a role in Vault that is configured to create a Postgres role
	// with all privileges.
	createSQL := `
		CREATE ROLE "{{name}}" WITH
			LOGIN
			PASSWORD '{{password}}'
			VALID UNTIL '{{expiration}}';
		GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO "{{name}}";
	`
	_, err = vcl.Write(path+"/roles/"+role, map[string]interface{}{
		"db_name":             "db",
		"default_ttl":         2,
		"max_ttl":             5,
		"creation_statements": createSQL,
	})
	if err != nil {
		t.Fatalf("Failed to create DB role in Vault: %v", err)
	}

	// These should succeed now.
	err = vdbc.SetConnMaxLifetime(db)
	assert.NoError(err)
	err = db.Ping()
	assert.NoError(err)

	watcher, err := vdbc.getWatcher()
	assert.NoError(err)
	go watcher.Start()

	// Make sure we got credentials.
	ephemeralRoleName := vdbc.rolename
	assert.NotEmpty(vdbc.rolename)
	assert.NotEmpty(vdbc.rolepass)

	// We can create an object with the credentials
	_, err = db.Exec("CREATE TABLE test();")
	assert.NoError(err)

	// Verify that the user postgres thinks we are is the same as what Vault
	// told us.
	row := db.QueryRow(`SELECT session_user`)
	assert.NoError(err)
	var sessionUser string
	err = row.Scan(&sessionUser)
	assert.NoError(err)
	assert.Equal(ephemeralRoleName, sessionUser)

	// Wait for a renewal, and drop the table (showing the dropping user is
	// the same as the creating one).
	renewEvent := <-watcher.RenewCh()
	assert.IsType(&vaultapi.RenewOutput{}, renewEvent)
	_, err = db.Exec("DROP TABLE test;")
	assert.NoError(err)

	// Re-create the table; then, wait for the old credentials to expire.
	_, err = db.Exec("CREATE TABLE test();")
	assert.NoError(err)
	doneErr := <-watcher.DoneCh()
	assert.NoError(doneErr)

	// Demonstrate that the new credentials are in use by looking at the
	// session user.  Because the credential rotation isn't happening in a
	// separate goroutine, it will happen in one of the queries in the loop,
	// but we don't know which, in advance.  This is because the "done"
	// notification we got above is not synchronized with the one received
	// in getCreds, so we don't have a guarantee that it will have been
	// delivered by the time we next call it.
	for start := time.Now(); err == nil &&
		sessionUser == ephemeralRoleName &&
		time.Now().Before(start.Add(time.Second)); time.Sleep(50 * time.Millisecond) {
		err = db.QueryRow(`SELECT session_user`).Scan(&sessionUser)
	}
	assert.NoError(err)
	assert.NotEqual(ephemeralRoleName, sessionUser)

	// Also, we can create new objects, but are unable to modify objects in
	// use by the old user.
	_, err = db.Exec("CREATE TABLE test2();")
	assert.NoError(err)
	_, err = db.Exec("DROP TABLE test;")
	assert.Error(err)

	// Run a query that creates objects at the beginning and the end, and is
	// long enough that it would have to straddle credential rotation.
	ephemeralRoleName = vdbc.rolename
	_, err = db.Exec("CREATE TABLE test3(); SELECT pg_sleep(5); CREATE TABLE test4();")
	assert.NoError(err)
	_, err = db.Exec("SELECT 1")
	assert.NoError(err)
	assert.NotEmpty(vdbc.rolename)
	assert.NotEmpty(vdbc.rolepass)
	assert.NotEqual(vdbc.rolename, ephemeralRoleName)

	// Make sure that table ownership is as expected; both tables created in
	// the previous statement, despite crossing a credential rotation, are
	// owned by the same user, but they're different from the owner of the
	// previous one.
	rows, err := db.Query(`
		SELECT tablename, tableowner
		FROM pg_tables
		WHERE tablename IN ('test', 'test3', 'test4')`)
	assert.NoError(err)
	owners := make(map[string]string)
	for rows.Next() {
		var owner, table string
		err = rows.Scan(&table, &owner)
		assert.NoError(err)
		owners[table] = owner
	}
	assert.NotEqual(owners["test2"], owners["test3"])
	assert.Equal(owners["test3"], owners["test4"])
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
