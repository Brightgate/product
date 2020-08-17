/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// cl-obs combines two related capabilities, based on access to a pool
// of observed device information objects:
//
// 1.  "Training."  We have built a training set by assigning values for
//     known devices.  Each classifier, once trained, can classify values
//     for one attribute of the device.  For now, this set is defined by
//     the `facts.sqlite3` file; each row of the devices table has the
//     assigned values for that device, each row of the training table
//     associates a device information object with that device.
//
//     The set of subcommands in support of the human trainer are
//     typically used to inspect the input and output objects, and their
//     organization and distribution.  Training is viewed as an offline
//     process.
//
// 2.  "Classification."  Classify attributes for a given MAC address or
//     set of MAC addresses or sites.  Classification is viewed as an
//     online process.  The index of known devices is updated via the
//     "ingest" subcommand.  The index of classifications is updated via
//     the "classify" subcommand, with some list of devices or sites
//     given to scope the classification operation.
//
// Classifiers are considered either "experimental" or "production",
// depending on how sufficient the trainer(s) assess the classifier's
// training data and classification accuracy to be.  Experimental
// classifiers may be active, but do not record their classifications in
// the "classification" table, which is the effective output interface
// to other cloud components.
//
// The pool of device information objects are accessed via the (site,
// device, timestamp) tuple.  This tuple can access files, where the
// tuple is expanded into a path based on the convention used in
// cl.eventd.

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	"bg/cl-obs/classifier"
	"bg/cl-obs/defs"
	"bg/cl-obs/extract"
	"bg/cl-obs/modeldb"
	"bg/cl_common/daemonutils"
	"bg/cl_common/deviceinfo"

	"go.uber.org/zap"
	"golang.org/x/crypto/sha3"
	"google.golang.org/api/option"

	"github.com/jmoiron/sqlx"
	"github.com/klauspost/oui"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"github.com/spf13/cobra"

	"cloud.google.com/go/storage"
)

const (
	ouiDefaultFile = "etc/oui.txt"

	unknownSite = "-unknown-site-"

	googleCredentialsEnvVar = "GOOGLE_APPLICATION_CREDENTIALS"

	experimentalClassifier = 0
	productionClassifier   = 10
)

// RecordedSite represents a row of the site table.  The site table is a local
// dictionary of nicknames for the site UUIDs.
type RecordedSite struct {
	SiteUUID string `db:"site_uuid"`
	SiteName string `db:"site_name"`
}

// RecordedInventory represents a row of the inventory table.  There is an
// entry in the inventory table for each received DeviceInfo file.
type RecordedInventory struct {
	Storage              string    `db:"storage"`
	InventoryDate        time.Time `db:"inventory_date"`
	UnixTimestamp        string    `db:"unix_timestamp"`
	SiteUUID             string    `db:"site_uuid"`
	DeviceMAC            string    `db:"device_mac"`
	DHCPVendor           string    `db:"dhcp_vendor"`
	BayesSentenceVersion string    `db:"bayes_sentence_version"`
	BayesSentence        string    `db:"bayes_sentence"`
}

// Tuple returns the deviceinfo.Tuple that names the deviceinfo file
// that this record fronts.
func (r *RecordedInventory) Tuple() deviceinfo.Tuple {
	t, err := deviceinfo.NewTupleFromStrings(r.SiteUUID, r.DeviceMAC, r.UnixTimestamp)
	if err != nil {
		panic(err)
	}
	return t
}

// RecordedDevice represents a row of the device table.  The device table is
// a collection of the devices and value assignments for the training set.
type RecordedDevice struct {
	DGroupID              int    `db:"dgroup_id"`
	DeviceMAC             string `db:"device_mac"`
	AssignedOSGenus       string `db:"assigned_os_genus"`
	AssignedOSSpecies     string `db:"assigned_os_species"`
	AssignedMfg           string `db:"assigned_mfg"`
	AssignedDeviceGenus   string `db:"assigned_device_genus"`
	AssignedDeviceSpecies string `db:"assigned_device_species"`
}

// RecordedTraining represents a row of the training table.  The training table
// is a collection of the DeviceInfo files associated with particular members of
// the training set.
type RecordedTraining struct {
	FactID        int    `db:"fact_id"`
	DGroupID      int    `db:"dgroup_id"`
	UnixTimestamp string `db:"unix_timestamp"`
	SiteUUID      string `db:"site_uuid"`
	DeviceMAC     string `db:"device_mac"`
}

// Tuple returns the deviceinfo.Tuple that names the deviceinfo file
// that this record fronts.
func (r *RecordedTraining) Tuple() deviceinfo.Tuple {
	t, err := deviceinfo.NewTupleFromStrings(r.SiteUUID, r.DeviceMAC, r.UnixTimestamp)
	if err != nil {
		panic(err)
	}
	return t
}

// RecordedIngest represents a row of the ingest table. The ingest table
// contains information about some number of the most recent ingestion
// runs.
type RecordedIngest struct {
	IngestDate     time.Time `db:"ingest_date"`
	SiteUUID       string    `db:"site_uuid"`
	NewInventories int64     `db:"new_inventories"`
	sync.Mutex
}

func (r *RecordedIngest) String() string {
	return fmt.Sprintf("[%s %s New:%d]", r.IngestDate.Format(time.RFC3339Nano),
		r.SiteUUID, r.NewInventories)
}

// RecordedClassification represents a row of the classification table. The
// classification table contains the classifications for each (site, device)
// tuple that have been made by each classifier.
type RecordedClassification struct {
	SiteUUID              string    `db:"site_uuid"`
	DeviceMAC             string    `db:"mac"`
	ModelName             string    `db:"model_name"`
	Classification        string    `db:"classification"`
	Probability           float64   `db:"probability"`
	ClassificationCreated time.Time `db:"classification_created"`
	ClassificationUpdated time.Time `db:"classification_updated"`
}

// Ingester represents a storage backend that contains DeviceInfo object
// stored according to some understood convention.  For example, a
// cloudIngester encodes the convention that, for a given cloud project,
// a bucket pattern combined with a certain prefix for objects can be
// used to calculate the set of currently stored DeviceInfo objects.
type Ingester interface {
	Ingest(*backdrop, map[uuid.UUID]bool) error
}

type backdrop struct {
	ingester            Ingester
	db                  *sqlx.DB
	modeldb             modeldb.DataStore
	modelsLoaded        bool
	ouidb               oui.OuiDB
	store               deviceinfo.Store
	bayesClassifiers    []*classifier.BayesClassifier
	lookupMfgClassifier *classifier.MfgLookupClassifier
}

var (
	_B backdrop

	log  *zap.Logger
	slog *zap.SugaredLogger
)

func getShake256(schema string) string {
	buf := []byte(schema)
	h := make([]byte, 64)
	sha3.ShakeSum256(h, buf)
	return fmt.Sprintf("%x", h)
}

func checkTableSchema(db *sqlx.DB, tname string, tschema string, verb string) {
	tschemaHash := getShake256(tschema)

	_, err := db.Exec(tschema)
	if err != nil {
		slog.Fatalf("could not create '%s' table: %v\n", tname, err)
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
			slog.Errorf("insert version failed: %v\n", err)
		}
		return
	}

	if err != nil {
		slog.Errorf("scan err %v\n", err)
		return
	}

	// Mismatch.
	if tschemaHash != schemaHash {
		slog.Infof("tname %s tschema %s; name %s, schema %s, create %v\n", tname, tschemaHash, name, schemaHash, creationDate)
		slog.Fatalf("schema hash mismatch for '%s'; delete and re-%s", tname, verb)
	}
}

func mustCreateVersionTable(vdb *sqlx.DB) {
	const versionSchema = `
    CREATE TABLE IF NOT EXISTS version (
	table_name TEXT PRIMARY KEY,
	schema_hash TEXT,
	create_date TIMESTAMP
    );`

	_, err := vdb.Exec(versionSchema)
	if err != nil {
		slog.Fatalf("could not create version table: %v\n", err)
	}
}

// The boundary between the primary database and the model database is
// dependency on (site, device, timestamp) tuples and the data
// associated with the same.  If a table references these tuples, it
// should be in the primary database (or in a corresponding table in the
// cloud registry).

// Each device info file gets a row in the inventory table.
// The inventory table has the inventory date and the device and the site.
// Each site gets a row in the sites table, for convenience.  (The cloud
// registry is the authoritative list of sites.)
// The site table has a comment naming the site.
// The device table and the training table together defined the training
// data for the various classfiers.
// Each identified device gets a row in the device table with its
// assigned classes (e.g. operating system genus, device genus).
// The training table combines specific device info records with rows in
// the device table.
// The ingest table records the timestamp of the last ingest operation.
// The classification table records the current set of calculated
// classifications.  (This table will be relocated from the observations
// file to a cloud database.)
func checkDB(idb *sqlx.DB) {
	const inventorySchema = `
    CREATE TABLE IF NOT EXISTS inventory (
	storage text,
	inventory_date timestamp,
	unix_timestamp text,
	site_uuid text,
	device_mac text,
	dhcp_vendor text,
	bayes_sentence_version text,
	bayes_sentence text,
	PRIMARY KEY(site_uuid, device_mac, unix_timestamp)
    );
    `
	const siteSchema = `
    CREATE TABLE IF NOT EXISTS site (
	site_uuid text PRIMARY KEY,
	site_name text
    );`
	const deviceSchema = `
    CREATE TABLE IF NOT EXISTS device (
	dgroup_id int PRIMARY KEY,
	device_mac text,
	assigned_os_genus text,
	assigned_os_species text,
	assigned_mfg text,
	assigned_device_genus text,
	assigned_device_species text
    );`
	const trainingSchema = `
    CREATE TABLE IF NOT EXISTS training (
	fact_id int PRIMARY KEY,
	dgroup_id int REFERENCES device(dgroup_id),
	site_uuid text,
	device_mac text,
	unix_timestamp text
    );`
	const ingestSchema = `
    CREATE TABLE IF NOT EXISTS ingest (
	ingest_date TIMESTAMP,
	site_uuid text REFERENCES site(site_uuid),
	new_inventories int,
	PRIMARY KEY (ingest_date, site_uuid)
    );`
	const classifySchema = `
    CREATE TABLE IF NOT EXISTS classification (
	site_uuid text,
	mac text,
	model_name text,
	classification text,
	probability float,
	classification_created timestamp,
	classification_updated timestamp,
	PRIMARY KEY (site_uuid, mac, model_name)
    );`

	mustCreateVersionTable(idb)

	checkTableSchema(idb, "inventory", inventorySchema, "ingest")
	checkTableSchema(idb, "site", siteSchema, "ingest")
	checkTableSchema(idb, "device", deviceSchema, "ingest")
	checkTableSchema(idb, "training", trainingSchema, "ingest")
	checkTableSchema(idb, "ingest", ingestSchema, "ingest")
	checkTableSchema(idb, "classify", classifySchema, "classify")

	const inventoryIndex = `
    CREATE INDEX IF NOT EXISTS ix_inventory_site_uuid ON inventory ( site_uuid );
    CREATE INDEX IF NOT EXISTS ix_inventory_device_mac ON inventory ( device_mac );
    CREATE INDEX IF NOT EXISTS ix_inventory_inventory_date_desc ON inventory ( inventory_date DESC );
    CREATE INDEX IF NOT EXISTS ix_inventory_inventory_date_asc ON inventory ( inventory_date ASC );`
	if _, err := idb.Exec(inventoryIndex); err != nil {
		slog.Fatalf("could not create indexes: %v", err)
	}
}

func getMfgFromMAC(B *backdrop, mac string) string {
	if strings.HasPrefix(strings.ToLower(mac), "60:90:84:a") {
		return "Brightgate, Inc."
	}

	entry, err := B.ouidb.Query(mac)
	if err == nil {
		return entry.Manufacturer
	}

	return defs.UnknownMfg
}

func listDevices(B *backdrop, detailed bool) error {
	rows, err := B.db.Queryx("SELECT * FROM inventory ORDER BY device_mac;")
	if err != nil {
		return errors.Wrap(err, "select inventory failed")
	}

	for rows.Next() {
		ri := RecordedInventory{}
		err = rows.StructScan(&ri)
		if err != nil {
			slog.Errorf("inventory scan failed: %v\n", err)
			continue
		}

		fmt.Printf("%v\n", ri)
	}

	return nil
}

func matchSites(B *backdrop, match string) ([]RecordedSite, error) {
	var err error

	sites := make([]RecordedSite, 0)

	if match == "" || match == "*" {
		err = B.db.Select(&sites, "SELECT site_uuid, site_name FROM site ORDER BY site_uuid;")
	} else {
		err = B.db.Select(&sites, "SELECT site_uuid, site_name FROM site WHERE site_uuid GLOB $1 OR site_name GLOB $1 ORDER BY site_uuid;", match)
	}
	if err != nil {
		return nil, errors.Wrap(err, "select site failed")
	}
	return sites, nil
}

func listSites(B *backdrop, includeDevices bool, noNames bool, args []string) error {
	var err error
	withClassifications := B.modelsLoaded

	models := []modeldb.RecordedClassifier{}
	if withClassifications {
		models, err = B.modeldb.GetModels()
		if err != nil {
			return err
		}

		slog.Infof("models: %d", len(models))
	}

	if len(args) == 0 {
		args = []string{"*"}
	}

	sites := make([]RecordedSite, 0)
	for _, arg := range args {
		nsites, err := matchSites(B, arg)
		if err != nil {
			return errors.Wrapf(err, "site match %q failed", arg)
		}
		sites = append(sites, nsites...)
	}

	for _, site := range sites {
		if noNames {
			fmt.Printf("%s\n", site.SiteUUID)
		} else {
			fmt.Printf("%18s %20s\n", site.SiteUUID, site.SiteName)
		}

		if includeDevices {
			var deviceMacs []string
			err := B.db.Select(&deviceMacs, `
				SELECT DISTINCT device_mac
				FROM inventory
				WHERE site_uuid = $1
				ORDER BY inventory_date ASC;`, site.SiteUUID)
			if err != nil {
				slog.Errorf("select inventory failed: %v\n", err)
				continue
			}

			for _, mac := range deviceMacs {
				fmt.Printf("  %15s %20s\n", mac, getMfgFromMAC(B, mac))
				if withClassifications {
					desc, sent := classifyMac(B, models, site.SiteUUID, mac, false)
					fmt.Printf("\t%s\n", desc)
					fmt.Printf("\t%s\n", sent.String())
				}
			}
		}
	}

	return nil
}

func siteSub(cmd *cobra.Command, args []string) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	noNames, _ := cmd.Flags().GetBool("no-names")

	return listSites(&_B, verbose, noNames, args)
}

func deviceSub(cmd *cobra.Command, args []string) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	return listDevices(&_B, verbose)
}

func classifySub(cmd *cobra.Command, args []string) error {
	persist, _ := cmd.Flags().GetBool("persist")

	if !_B.modelsLoaded {
		return errors.Errorf("Model not loaded.  You may need to pass --model-file")
	}
	models, err := _B.modeldb.GetModels()
	if err != nil {
		return errors.Wrap(err, "getModels failed")
	}

	slog.Infof("models: %d", len(models))

	// Loop over positional arguments.
	for _, arg := range args {
		// is it a mac?
		_, err = net.ParseMAC(arg)
		if err == nil {
			classifyMac(&_B, models, "", arg, persist)
			continue
		}

		// else try to run the site matcher on it
		sites, err := matchSites(&_B, arg)
		if err != nil {
			return errors.Wrapf(err, "couldn't find a site name or UUID matching %s", arg)
		}
		for _, site := range sites {
			err := classifySite(&_B, models, site.SiteUUID, persist)
			if err != nil {
				return err
			}
			slog.Debugf("finished classifying %s", site.SiteUUID)
		}
	}

	return nil
}

func setupIngester(cmd *cobra.Command, store deviceinfo.Store) (Ingester, error) {
	ingestProject, _ := cmd.Flags().GetString("project")
	workers, _ := cmd.Flags().GetInt("workers")

	if ingestProject != "" {
		slog.Infof("cloud ingest from %s", ingestProject)
		ingester, err := newCloudIngester(ingestProject, workers)
		if err != nil {
			slog.Warnf("failed setting up cloud ingester: %v", err)
			return nil, err
		}
		return ingester, nil
	}

	// None has been defined.
	return nil, errors.Errorf("no ingester configured")
}

func ingestSub(cmd *cobra.Command, args []string) error {
	var selectedUUIDs map[uuid.UUID]bool
	var err error

	if _B.ingester == nil {
		return errors.Errorf("You must provide --dir or --project")
	}

	if len(args) > 0 {
		selectedUUIDs = make(map[uuid.UUID]bool)
		for _, arg := range args {
			if arg == "*" {
				// * overrides whatever else was passed
				selectedUUIDs = nil
				break
			}
			uu, err := uuid.FromString(arg)
			if err != nil {
				return err
			}
			selectedUUIDs[uu] = true
		}
	}

	err = _B.ingester.Ingest(&_B, selectedUUIDs)
	if err != nil {
		return err
	}

	return nil
}

func extractSub(cmd *cobra.Command, args []string) error {
	if _B.ingester == nil {
		return errors.Errorf("You must provide --dir or --project")
	}

	if dhcp, _ := cmd.Flags().GetBool("dhcp"); dhcp {
		return extractDHCPRecords(&_B)
	}
	if dns, _ := cmd.Flags().GetBool("dns"); dns {
		return extractDNSRecords(&_B)
	}
	if mfg, _ := cmd.Flags().GetBool("mfg"); mfg {
		return extractMfgs(&_B)
	}
	if device, _ := cmd.Flags().GetBool("device"); device {
		return extractDevices(&_B)
	}
	return errors.New("please specify extraction type")
}

func trainSub(cmd *cobra.Command, args []string) error {
	modelFile, _ := cmd.Flags().GetString("model-file")
	outBucket, _ := cmd.Flags().GetString("output-bucket")

	if !_B.modelsLoaded {
		return errors.Errorf("Model not loaded.  You may need to pass --model-file")
	}
	if _B.store == nil {
		return errors.Errorf("You must provide --dir or --project")
	}

	if !_B.modelsLoaded {
		return errors.Errorf("Model not loaded.  You may need to pass --model-file")
	}

	if err := trainDeviceGenusBayesClassifier(&_B); err != nil {
		return err
	}
	if err := trainOSGenusBayesClassifier(&_B); err != nil {
		return err
	}
	if err := trainOSSpeciesBayesClassifier(&_B); err != nil {
		return err
	}
	trainInterfaceMfgLookupClassifier(&_B)

	slog.Infof("training models complete")

	// If the 'output-bucket' flag is set then copy the model output
	// file to GCS.
	// "bg-classifier-support" is the production bucket.
	if outBucket != "" {
		cenv := os.Getenv(googleCredentialsEnvVar)
		if cenv == "" {
			return fmt.Errorf("Provide cloud credentials through %s envvar",
				googleCredentialsEnvVar)
		}

		ctx := context.Background()
		client, err := storage.NewClient(ctx, option.WithCredentialsFile(cenv))

		if err != nil {
			slog.Fatalf("cannot access cloud storage: %v", err)
		}

		rdr, err := os.Open(modelFile)
		if err != nil {
			slog.Fatalf("cannot open '%s' to upload to bucket '%s': %v",
				modelFile, outBucket, err)
		}

		bkt := client.Bucket(outBucket)
		_, err = bkt.Attrs(ctx)
		if err != nil {
			slog.Fatalf("cannot retrieve bucket attrs for %s: %v", outBucket, err)
		}

		ufn := path.Base(modelFile)

		obj := bkt.Object(ufn)
		_, err = obj.Attrs(ctx)
		if err != nil {
			slog.Infof("cannot retrieve object attrs for %s: %v", obj.ObjectName(), err)
		}

		wrt := obj.NewWriter(ctx)

		written, err := io.Copy(wrt, rdr)
		if err != nil {
			slog.Infof("upload i/o error: %v", err)
		} else {
			slog.Infof("uploaded %d bytes", written)
		}

		err = wrt.Close()
		if err != nil {
			slog.Fatalf("close failure on upload: %v", err)
		}
	}

	return nil
}

func loadModel(B *backdrop, modelFile string) error {
	var modelPath string

	slog.Infof("load model %q", modelFile)
	modelPath, err := modeldb.GetModelFromURL(modelFile)
	if err != nil {
		return errors.Wrap(err, "getting model file")
	}
	slog.Infof("modelPath %q", modelPath)

	B.modeldb, err = modeldb.OpenSQLite(modelPath)
	if err != nil {
		slog.Fatalf("model database open: %v\n", err)
	}
	if err := B.modeldb.CheckDB(); err != nil {
		slog.Fatalf("modeldb check: %v\n", err)
	}
	classifiers, err := B.modeldb.GetModels()
	if err != nil {
		slog.Fatalf("modeldb get: %v\n", err)
	}
	B.bayesClassifiers = make([]*classifier.BayesClassifier, 0)

	for _, rc := range classifiers {
		if rc.ClassifierType == "bayes" {
			cl, err := classifier.NewBayesClassifier(rc)
			if err != nil {
				return errors.Wrap(err, "failed to make bayes classifier")
			}
			B.bayesClassifiers = append(_B.bayesClassifiers, cl)
		} else if rc.ModelName == "lookup-mfg" {
			cl := classifier.NewMfgLookupClassifier(B.ouidb)
			B.lookupMfgClassifier = cl
		} else {
			slog.Warnf("unknown classifier %v", rc)
		}
	}
	B.modelsLoaded = true
	return nil
}

func readyBackdrop(B *backdrop, cmd *cobra.Command) error {
	var err error

	ouiFile, _ := cmd.Flags().GetString("oui-file")
	B.ouidb, err = oui.OpenStaticFile(ouiFile)
	if err != nil {
		return errors.Wrap(err, "unable to open OUI database")
	}

	obsFile, _ := cmd.Flags().GetString("observations-file")
	obsPath := fmt.Sprintf("file:%s?cache=shared", obsFile)
	slog.Infof("Observations DB %s", obsPath)

	B.db, err = sqlx.Connect("sqlite3", obsPath)
	if err != nil {
		return errors.Wrap(err, "database open")
	}
	// This means no nested queries...
	B.db.SetMaxOpenConns(1)

	err = B.db.Ping()
	if err != nil {
		return errors.Wrap(err, "database ping")
	}

	checkDB(B.db)

	// These settings enable the write-ahead log, and relax the synchronous mode
	// from FULL to NORMAL.  This seems to provide a massive performance boost.
	_, err = _B.db.Exec("PRAGMA main.journal_mode = WAL; PRAGMA main.synchronous = NORMAL;")
	if err != nil {
		return errors.Wrap(err, "Couldn't set DB performance settings")
	}

	slog.Infof("running combined version: %s\n", extract.CombinedVersion)

	modelFile, _ := cmd.Flags().GetString("model-file")
	slog.Infof("Models DB %s", modelFile)
	err = loadModel(B, modelFile)
	if err != nil {
		slog.Warnf("loadModel failed: %v", err)
	}

	var store deviceinfo.Store
	if proj, _ := cmd.Flags().GetString("project"); proj != "" {
		client, err := storage.NewClient(context.Background())
		if err != nil {
			return errors.Wrap(err, "couldn't setup storage client")
		}
		// cl-obs uses a fixed mapping from site UUID to bucket name
		// other parts of the codebase approach this differently, which
		// is why this adapter is needed.
		mapper := func(ctx context.Context, uuid uuid.UUID) (string, string, error) {
			return "gcs", fmt.Sprintf("bg-appliance-data-%s", uuid), nil
		}
		store = deviceinfo.NewGCSStore(client, mapper)
	}

	B.store = store
	B.ingester, err = setupIngester(cmd, store)
	if err != nil {
		slog.Debugf("couldn't setup ingester: %v", err)
	}
	return nil
}

func closeBackdrop(B *backdrop) {
	B.db.Close()
	if B.modelsLoaded {
		B.modeldb.Close()
	}
}

func main() {
	var err error

	flag.Parse()
	log, slog = daemonutils.SetupLogs()

	clRoot := daemonutils.ClRoot()
	defaultOUIFile := filepath.Join(clRoot, ouiDefaultFile)

	rootCmd := &cobra.Command{
		Use: "cl-obs",
		PersistentPreRun: func(ccmd *cobra.Command, args []string) {
			log, slog = daemonutils.ResetupLogs()
			_ = zap.RedirectStdLog(log)
			if cpuProfile, _ := ccmd.Flags().GetString("cpuprofile"); cpuProfile != "" {
				pf, err := os.Create(cpuProfile)
				if err != nil {
					slog.Fatalf("CPU profiling file not created: %v", err)
				}

				slog.Infof("activating CPU profiling to %s", cpuProfile)
				if err = pprof.StartCPUProfile(pf); err != nil {
					panic(err.Error())
				}
			}

			if ccmd.Name() == "help" {
				return
			}

			if err = readyBackdrop(&_B, ccmd); err != nil {
				slog.Fatalf("initialization failed: %v", err)
			}

		},
		PersistentPostRun: func(ccmd *cobra.Command, args []string) {
			if cpuProfile, _ := ccmd.Flags().GetString("cpuprofile"); cpuProfile != "" {
				pprof.StopCPUProfile()
			}

			if ccmd.Name() == "help" {
				return
			}

			closeBackdrop(&_B)
		},
	}
	rootCmd.PersistentFlags().String("cpuprofile", "", "CPU profiling filename")
	rootCmd.PersistentFlags().String("observations-file", "obs.db", "observations index path")
	rootCmd.PersistentFlags().String("oui-file", defaultOUIFile, "OUI text database path")
	rootCmd.PersistentFlags().String("project", "", "GCP project for DeviceInfo files")
	rootCmd.PersistentFlags().String("model-file", "trained-models.db", "path to model file")
	rootCmd.PersistentFlags().AddFlagSet(daemonutils.GetLogFlagSet())

	siteCmd := &cobra.Command{
		Use:   "site [*|site-name|site-uuid]",
		Short: "List sites",
		Args:  cobra.MinimumNArgs(0),
		RunE:  siteSub,
	}
	siteCmd.Flags().BoolP("verbose", "v", false, "list site details")
	siteCmd.Flags().BoolP("no-names", "n", false, "only print site UUIDs; no names")
	rootCmd.AddCommand(siteCmd)

	deviceCmd := &cobra.Command{
		Use:   "device",
		Short: "List devices",
		Args:  cobra.NoArgs,
		RunE:  deviceSub,
	}
	deviceCmd.Flags().BoolP("verbose", "v", false, "list device details")
	rootCmd.AddCommand(deviceCmd)

	lsCmd := &cobra.Command{
		Use:   "ls [*|site-name|site-uuid|macaddr ...]",
		Short: "List deviceInfos for matching MACs or MACs for matching UUIDs",
		Args:  cobra.MinimumNArgs(1),
		RunE:  lsSub,
	}
	lsCmd.Flags().BoolP("verbose", "v", false, "detailed output")
	lsCmd.Flags().Bool("redundant", false, "also show redundant inventory records")
	rootCmd.AddCommand(lsCmd)

	ingestCmd := &cobra.Command{
		Use:   "ingest [*|site-name|site-uuid ...]",
		Short: "Ingest device info files from tree",
		Args:  cobra.MinimumNArgs(0),
		RunE:  ingestSub,
	}
	ingestCmd.Flags().Int("workers", 0, "number of asynchronous workers")
	rootCmd.AddCommand(ingestCmd)

	extractCmd := &cobra.Command{
		Use:   "extract",
		Short: "Extract data from training set",
		Args:  cobra.NoArgs,
		RunE:  extractSub,
	}
	extractCmd.Flags().Bool("dhcp", false, "list device DHCP parameters")
	extractCmd.Flags().Bool("dns", false, "list device DNS queries")
	extractCmd.Flags().Bool("mfg", false, "list OUI manufacturers")
	extractCmd.Flags().Bool("device", false, "list devices")
	rootCmd.AddCommand(extractCmd)

	trainCmd := &cobra.Command{
		Use:   "train",
		Short: "Train classifier",
		Args:  cobra.NoArgs,
		RunE:  trainSub,
	}
	trainCmd.Flags().String("output-bucket", "", "also write output to given bucket")
	rootCmd.AddCommand(trainCmd)

	reviewCmd := &cobra.Command{
		Use:   "review",
		Short: "Review training data and classifiers",
		Args:  cobra.NoArgs,
		RunE:  reviewSub,
	}
	rootCmd.AddCommand(reviewCmd)

	classifyCmd := &cobra.Command{
		Use:   "classify [*|site-name|site-uuid|macaddr ...]",
		Short: "Classify device",
		Args:  cobra.MinimumNArgs(0),
		RunE:  classifySub,
	}
	classifyCmd.Flags().Bool("persist", false, "record classifications")
	rootCmd.AddCommand(classifyCmd)

	err = rootCmd.Execute()
	os.Exit(map[bool]int{true: 0, false: 1}[err == nil])
}

