/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

//
// Package briefpg provides an easy way to start and control temporary
// instances of PostgreSQL servers.  The package is designed primarily as an
// aid to writing test cases.  The package does not select or mandate any
// particular PostgreSQL driver, as it invokes Postgres commands to do its
// work.
//
// Concepts are drawn from EphemeralPG and Python's testing.postgresql.
//
package briefpg

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

const (
	// Cribbed from https://bitbucket.org/eradman/ephemeralpg
	pgConf = `
unix_socket_directories = '%s'
listen_addresses = ''
shared_buffers = 12MB
fsync = off
synchronous_commit = off
full_page_writes = off
log_min_duration_statement = 0
log_connections = on
log_disconnections = on
max_worker_processes = 4
`
)

var (
	prefix string
	pgVer  string
)

type bpState int

const (
	stateNotPresent bpState = iota
	statePresent
	stateUninitialized
	stateInitialized
	stateServerStarted
	stateDefunct
)

// BriefPG represents a managed instance of the Postgres database server; the
// instance and all associated data is disposed when Fini() is called.
type BriefPG struct {
	TmpDir   string      // Can be user-set if desired
	Encoding string      // Defaults to "UNICODE"
	Logger   *log.Logger // Verbose output
	state    bpState
}

var utilities = []string{"psql", "initdb", "pg_ctl", "pg_dump"}

var pgCmds = map[string]string{}

var tryGlobs = []string{
	"/usr/lib/postgresql/*/bin", // Debian
	"/usr/pgsql-*/bin",          // Centos
	"/usr/local/pgsql/bin",
	"/usr/local/pgsql-*/bin",
	"/usr/local/bin",
}

var allPaths = make([]string, 1)

func wrapExecErr(msg string, cmd *exec.Cmd, err error) error {
	args := strings.Join(cmd.Args, " ")
	if xerr, ok := err.(*exec.ExitError); ok {
		return errors.Wrapf(xerr, "%s; command: %s; stderr: %s",
			msg, args, xerr.Stderr)
	}
	return errors.Wrapf(err, "%s", msg)
}

func init() {
	user, err := user.Current()
	if err != nil {
		panic("could not get current user; not implemented?")
	}
	prefix = fmt.Sprintf("briefpg.%s", user.Name)

	pathSplit := strings.Split(os.Getenv("PATH"), ":")
	allPaths = append(allPaths, pathSplit...)

	for _, glob := range tryGlobs {
		if paths, err := filepath.Glob(glob); err == nil {
			allPaths = append(allPaths, paths...)
		}
	}

pathLoop:
	for _, path := range allPaths {
		newCmds := make(map[string]string)
		for _, cName := range utilities {
			p := filepath.Join(path, cName)
			if _, err := os.Stat(p); err != nil {
				continue pathLoop
			}
			newCmds[cName] = p
		}
		pgCmds = newCmds
		break
	}

	if len(pgCmds) == 0 {
		return
	}

	outb, err := exec.Command(pgCmds["psql"], "-V").Output()
	if err != nil {
		pgVer = ""
	}
	out := strings.TrimSpace(string(outb))
	sl := strings.Split(out, " ")
	pgVer = sl[len(sl)-1]
}

// CheckInstall returns an error if the module is unable to operate due to
// a failure to locate postgres
func CheckInstall() error {
	if len(pgCmds) != len(utilities) {
		return errors.Errorf("Failed to find postgres installation (Tried %v)", allPaths)
	}
	return nil
}

// New returns an instance of BriefPG
func New(logger *log.Logger) *BriefPG {
	if logger == nil {
		logger = log.New(ioutil.Discard, "", 0)
	}
	return &BriefPG{
		state:    stateUninitialized,
		Encoding: "UNICODE",
		Logger:   logger,
	}
}

func (bp *BriefPG) mkTemp() error {
	var err error
	bp.TmpDir, err = ioutil.TempDir("", prefix)
	if err != nil {
		return errors.Wrap(err, "Failed to make tmpdir")
	}
	return nil
}

func (bp *BriefPG) dbDir() string {
	return filepath.Join(bp.TmpDir, pgVer)
}

func (bp *BriefPG) initDB(ctx context.Context) error {
	if err := CheckInstall(); err != nil {
		return err
	}

	if bp.TmpDir == "" {
		if err := bp.mkTemp(); err != nil {
			return err
		}
		bp.state = statePresent
	} else if _, err := os.Stat(bp.TmpDir); err != nil {
		bp.state = stateNotPresent
		return errors.Wrapf(err, "Tmpdir %s not present or not readable", bp.TmpDir)
	}

	if _, err := os.Stat(bp.dbDir()); err != nil {
		cmd := exec.Command(pgCmds["initdb"], "--nosync", "-D", bp.dbDir(), "-E", bp.Encoding, "-A", "trust")
		bp.Logger.Println("briefpg: " + strings.Join(cmd.Args, " "))
		cmdOut, err := cmd.CombinedOutput()
		bp.Logger.Println("briefpg: " + string(cmdOut))
		if err != nil {
			return wrapExecErr("initDB failed", cmd, err)
		}
	}
	confFile := filepath.Join(bp.TmpDir, pgVer, "postgresql.conf")
	// Unix domain sockets appear in the TmpDir
	confContents := fmt.Sprintf(pgConf, bp.TmpDir)
	if err := ioutil.WriteFile(confFile, []byte(confContents), 0600); err != nil {
		return errors.Wrap(err, "initDB failed to write config")
	}
	bp.state = stateInitialized
	return nil
}

// Start the postgres server, performing necessary initialization along the way
func (bp *BriefPG) Start(ctx context.Context) error {
	var err error
	if bp.state == stateDefunct {
		return errors.Errorf("briefpg instance is defunct")
	}

	if bp.state < stateInitialized {
		err = bp.initDB(ctx)
		if err != nil {
			return err
		}
	}

	userOpts := "" // XXX
	postgresOpts := fmt.Sprintf("-c listen_addresses='' %s", userOpts)
	logFile := filepath.Join(bp.dbDir(), "postgres.log")
	cmd := exec.Command(pgCmds["pg_ctl"], "-w", "-o", postgresOpts, "-s", "-D", bp.dbDir(), "-l", logFile, "start")
	bp.Logger.Println("briefpg: " + strings.Join(cmd.Args, " "))
	cmdOut, err := cmd.CombinedOutput()
	bp.Logger.Println("briefpg: " + string(cmdOut))
	if err != nil {
		return wrapExecErr("Start failed", cmd, err)
	}
	bp.state = stateServerStarted
	return nil
}

// CreateDB is a convenience function to create a named database; you can do this
// using your database driver instead, at lower cost.  This routine uses 'psql' to
// do the job.  The primary use case is to rapidly set up an empty database for
// test purposes.
func (bp *BriefPG) CreateDB(ctx context.Context, dbName, createArgs string) (string, error) {
	if bp.state < stateServerStarted {
		return "", errors.Errorf("Server not started; cannot create database")
	}
	scmd := fmt.Sprintf("CREATE DATABASE \"%s\" %s", dbName, createArgs)
	cmd := exec.Command(pgCmds["psql"], "-c", scmd, bp.DBUri("postgres"))
	bp.Logger.Println("briefpg: " + strings.Join(cmd.Args, " "))
	cmdOut, err := cmd.CombinedOutput()
	bp.Logger.Println("briefpg: " + string(cmdOut))
	if err != nil {
		return "", wrapExecErr("CreateDB failed", cmd, err)
	}
	return bp.DBUri(dbName), nil
}

// DumpDB writes the named database contents to w using pg_dump.  In a test
// case, this can be used to dump the database in the event of a failure.
func (bp *BriefPG) DumpDB(ctx context.Context, dbName string, w io.Writer) error {
	if bp.state < stateServerStarted {
		return errors.Errorf("Server not started; cannot dump database")
	}
	cmd := exec.Command(pgCmds["pg_dump"], bp.DBUri(dbName))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	bp.Logger.Println("briefpg: starting dump: " + strings.Join(cmd.Args, " "))
	err = cmd.Start()
	if err != nil {
		return err
	}
	_, err = io.Copy(w, stdout)
	if err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		return wrapExecErr("DumpDB failed", cmd, err)
	}
	return nil
}

// DBUri returns the connection URI for a named database
func (bp *BriefPG) DBUri(dbName string) string {
	return fmt.Sprintf("postgresql:///%s?host=%s", dbName, url.PathEscape(bp.TmpDir))
}

// Fini stops the database server, if running, and
func (bp *BriefPG) Fini(ctx context.Context) error {
	bp.state = stateDefunct
	if bp.state >= stateServerStarted {
		cmd := exec.Command(pgCmds["pg_ctl"], "-m", "immediate", "-w", "-D", bp.dbDir(), "stop")
		bp.Logger.Println("briefpg: " + strings.Join(cmd.Args, " "))
		cmdOut, err := cmd.CombinedOutput()
		bp.Logger.Println("briefpg: " + string(cmdOut))
		if err != nil {
			return wrapExecErr("Fini failed", cmd, err)
		}
	}

	if bp.state >= statePresent {
		os.RemoveAll(bp.TmpDir)
	}
	return nil
}
