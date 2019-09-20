//
// COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

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
	"database/sql"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path"
	"regexp"
	"runtime/pprof"
	"strings"
	"time"

	"bg/base_msg"

	"golang.org/x/crypto/sha3"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	"github.com/klauspost/oui"

	"github.com/golang/protobuf/proto"
)

const (
	pname = "cl-obs"

	unknownSite = "-unknown-site-"

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
	InventoryDate        time.Time `db:"inventory_date"`
	UnixTimestamp        string    `db:"unix_timestamp"`
	SiteUUID             string    `db:"site_uuid"`
	DeviceMAC            string    `db:"device_mac"`
	DHCPVendor           string    `db:"dhcp_vendor"`
	BayesSentenceVersion string    `db:"bayes_sentence_version"`
	BayesSentence        string    `db:"bayes_sentence"`
}

// RecordedDeviceInfo represents a row of the device table.  The device table
// is a collection of the devices and value assignments for the training set.
type RecordedDeviceInfo struct {
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

// RecordedIngest represents a row of the ingest table. The ingest table
// contains information about some number of the most recent ingestion
// runs.
type RecordedIngest struct {
	IngestDate         time.Time `db:"ingest_date"`
	NewSites           int       `db:"new_sites"`
	NewInventories     int       `db:"new_inventories"`
	UpdatedInventories int       `db:"updated_inventories"`
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

type mfgBucket struct {
	Prefix string
	Name   string
	Count  int
}

type dhcpBucket struct {
	Options     []byte
	Vendor      string
	VendorMatch string
}

type backdrop struct {
	db                     *sqlx.DB
	modeldb                *sqlx.DB
	modelsLoaded           bool
	persistClassifications bool
	ouidb                  oui.OuiDB
}

var (
	_B backdrop

	cpuProfile            string
	deviceDetails         bool
	extractListDHCPParams bool
	extractListDNSRecords bool
	extractListModels     bool
	extractListOUIMfgs    bool
	extractOutput         string
	ingestCap             int
	ingestDir             string
	lsDetails             bool
	modelDir              string
	modelFile             string
	persistent            bool
	reviewDetails         bool
	observationsFile      string
	ouiFile               string
	siteDetails           bool
	siteMatch             string
	// trainOutput           string
	trainSelect string
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
		log.Fatalf("could not create '%s' table: %v\n", tname, err)
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
			log.Printf("insert version failed: %v\n", err)
		}
		return
	}

	if err != nil {
		log.Printf("scan err %v\n", err)
		return
	}

	// Mismatch.
	if tschemaHash != schemaHash {
		log.Printf("tname %s tschema %s; name %s, schema %s, create %v\n", tname, tschemaHash, name, schemaHash, creationDate)
		log.Printf("schema hash mismatch for '%s'; delete and re-%s", tname, verb)
		os.Exit(1)
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
		log.Fatalf("could not create version table: %v\n", err)
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
	inventory_date timestamp,
	unix_timestamp text,
	site_uuid text,
	device_mac text,
	dhcp_vendor text,
	bayes_sentence_version text,
	bayes_sentence text,
	PRIMARY KEY(site_uuid, device_mac, unix_timestamp)
    );`
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
	new_sites int,
	new_inventories int,
	updated_inventories int
    );`
	const classifySchema = `
    CREATE TABLE IF NOT EXISTS classification (
	site_uuid text,
	mac text,
	model_name text,
	classification text,
	probability float,
	classification_created timestamp,
	classification_updated timestamp
    );`

	mustCreateVersionTable(idb)

	checkTableSchema(idb, "inventory", inventorySchema, "ingest")
	checkTableSchema(idb, "site", siteSchema, "ingest")
	checkTableSchema(idb, "device", deviceSchema, "ingest")
	checkTableSchema(idb, "training", trainingSchema, "ingest")
	checkTableSchema(idb, "ingest", ingestSchema, "ingest")
	checkTableSchema(idb, "classify", classifySchema, "classify")

	const inventoryIndex = `
    CREATE INDEX IF NOT EXISTS ix_inventory ON inventory (
	inventory_date ASC
    );`

	_, err := idb.Exec(inventoryIndex)
	if err != nil {
		log.Fatalf("could not create ix_inventory index: %v\n", err)
	}
}

// Classifier levels are ordered so that we can train new classifiers
// without impacting the production output.
func checkModelDB(mdb *sqlx.DB) {
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
	mustCreateVersionTable(mdb)

	checkTableSchema(mdb, "model", modelSchema, "train")
}

func getMfgFromMAC(B *backdrop, mac string) string {
	if strings.HasPrefix(strings.ToLower(mac), "60:90:84:a") {
		return "Brightgate, Inc."
	}

	entry, err := B.ouidb.Query(mac)
	if err == nil {
		return entry.Manufacturer
	}

	return unknownMfg
}

type hostBucket struct {
	ACount     int
	AAAACount  int
	OtherCount int
}

const dnsINRequestPat = ";(.*)\tIN\t (.*)"

var dnsINRequestRE *regexp.Regexp

func printDHCPOptions(w io.Writer, do []*base_msg.DHCPOptions) {
	var params []byte
	var vendor []byte

	for o := range do {
		if len(do[o].ParamReqList) > 0 {
			params = do[o].ParamReqList
			break
		}
	}
	for o := range do {
		if len(do[o].VendorClassId) > 0 {
			vendor = do[o].VendorClassId
			break
		}
	}

	fmt.Fprintf(w, "  [DHCP] options = %v %v\n", params, string(vendor))
}

func printNetEntity(w io.Writer, ne *base_msg.EventNetEntity) {
	fmt.Fprintf(w, "  [Entity] %v\n", ne)
}

func printNetRequests(w io.Writer, nr []*base_msg.EventNetRequest) {
	for i := range nr {
		fmt.Fprintf(w, "  [Requests] %d %v\n", i, nr[i])
	}
}

func printNetScans(w io.Writer, ns []*base_msg.EventNetScan) {
	for i := range ns {
		fmt.Fprintf(w, "  [Scans] %d %v\n", i, ns[i])
	}
}

func printNetListens(w io.Writer, nl []*base_msg.EventListen) {
	for i := range nl {
		fmt.Fprintf(w, "  [Listens] %d %v\n", i, nl[i])
	}
}

func printInventory(w io.Writer, B *backdrop, ri RecordedInventory) {
	fmt.Fprintf(w, "%v\n", ri)
}

func printDevice(w io.Writer, B *backdrop, dmac string, dinfo string, detailed bool) {
	hw, err := net.ParseMAC(dmac)
	if err != nil {
		fmt.Fprintf(w, "** couldn't parse MAC '%s': %v\n", dmac, err)
		return
	}

	buf, rerr := ioutil.ReadFile(dinfo)
	if rerr != nil {
		fmt.Fprintf(w, "** couldn't read %s: %v", dinfo, err)
		return
	}

	di := &base_msg.DeviceInfo{}
	err = proto.Unmarshal(buf, di)
	if err != nil {
		fmt.Fprintf(w, "** unmarshalling failure: %v", err)
		return
	}

	dns := "-"
	if di.DnsName != nil {
		dns = *di.DnsName
	}

	dhcpn := "-"
	if di.DhcpName != nil {
		dhcpn = *di.DhcpName
	}

	fmt.Fprintf(w, "%18s %26s %26s %4d\n", hw.String(), dns, dhcpn, 0)

	if hw.String() != "" {
		fmt.Fprintln(w, getMfgFromMAC(B, hw.String()))
	}

	if detailed {
		fmt.Fprintln(w, "{{")
		printDHCPOptions(w, di.Options)
		printNetEntity(w, di.Entity)
		printNetRequests(w, di.Request)
		printNetScans(w, di.Scan)
		printNetListens(w, di.Listen)
		fmt.Fprintln(w, "}}")
	}

	fmt.Fprintf(w, "== %s ==\n", dinfo)
	fmt.Fprintf(w, "insert or replace into training (fact_id, dgroup_id, info_file) values (00, 0, \"%s\");\n", dinfo)
	fmt.Fprintf(w, "%v", di)
}

func listDevices(B *backdrop, modelDir string, detailed bool) error {
	rows, err := B.db.Queryx("SELECT * FROM inventory ORDER BY device_mac;")
	if err != nil {
		return errors.Wrap(err, "select inventory failed")
	}

	for rows.Next() {
		ri := RecordedInventory{}
		err = rows.StructScan(&ri)
		if err != nil {
			log.Printf("inventory scan failed: %v\n", err)
			continue
		}

		printInventory(os.Stdout, B, ri)
	}

	return nil
}

func listSites(B *backdrop, includeDevices bool, match string) error {
	var rows *sql.Rows
	var err error

	withClassifications := B.modelsLoaded

	models := []RecordedClassifier{}
	if withClassifications {
		err = B.modeldb.Select(&models, "SELECT * FROM model ORDER BY name ASC")
		if err != nil {
			return errors.Wrap(err, "model select failed")
		}

		log.Printf("models: %d", len(models))
	}

	if match == "" {
		rows, err = B.db.Query("SELECT site_uuid, site_name FROM site;")
	} else {
		rows, err = B.db.Query("SELECT site_uuid, site_name FROM site WHERE site_uuid GLOB $1 OR site_name GLOB $1;", match)
	}
	if err != nil {
		return errors.Wrap(err, "select site failed")
	}

	for rows.Next() {
		var suuid, sname string

		err = rows.Scan(&suuid, &sname)
		if err != nil {
			log.Printf("site scan failed: %v\n", err)
			continue
		}

		fmt.Printf("%18s %20s\n", suuid, sname)

		if includeDevices {
			drows, err := B.db.Query("SELECT DISTINCT device_mac FROM inventory WHERE site_uuid = $1 ORDER BY inventory_date ASC;", suuid)
			if err != nil {
				log.Printf("select inventory failed: %v\n", err)
				continue
			}

			for drows.Next() {
				var mac string

				err = drows.Scan(&mac)
				if err != nil {
					log.Printf("device scan failed: %v\n", err)
					continue
				}

				p := ""
				if withClassifications {
					p = classifyMac(B, models, suuid, mac, false)
				}
				fmt.Printf("  %15s %20s %s\n", mac, getMfgFromMAC(B, mac), p)
			}

		}
	}

	return nil
}

func siteSub(cmd *cobra.Command, args []string) error {
	return listSites(&_B, siteDetails, siteMatch)
}

func deviceSub(cmd *cobra.Command, args []string) error {
	return listDevices(&_B, modelDir, deviceDetails)
}

func reviewSub(cmd *cobra.Command, args []string) error {
	// Training data

	if !_B.modelsLoaded {
		return fmt.Errorf("model database does not exist")
	}

	// Model review
	models := []RecordedClassifier{}
	err := _B.modeldb.Select(&models, "SELECT * FROM model ORDER BY name ASC")
	if err != nil {
		return fmt.Errorf("model select failed: %+v", err)
	}

	log.Printf("models: %d", len(models))

	for _, m := range models {
		switch m.ClassifierType {
		case "bayes":
			fmt.Println(reviewBayes(m))
		case "lookup":
			fmt.Printf("Lookup Classifier, Name: %s\n", m.ModelName)
		}
	}

	return nil
}

func getModels(B *backdrop) ([]RecordedClassifier, error) {
	models := make([]RecordedClassifier, 0)

	// For reporting, we restrict based on the readiness level.
	err := _B.modeldb.Select(&models, "SELECT * FROM model ORDER BY name ASC")
	if err != nil {
		return nil, errors.Wrap(err, "model select failed")
	}

	return models, nil
}

func classifySub(cmd *cobra.Command, args []string) error {
	models, err := getModels(&_B)
	if err != nil {
		return errors.Wrap(err, "getModels failed")
	}

	log.Printf("models: %d", len(models))

	// Loop over positional arguments.
	for _, mac := range args {
		_, err = net.ParseMAC(mac)
		if err == nil {
			classifyMac(&_B, models, "", mac, _B.persistClassifications)
			continue
		}

		classifySite(&_B, models, mac)
	}

	return nil
}

func ingestSub(cmd *cobra.Command, args []string) error {
	return ingestTree(&_B, ingestDir)
}

func catSub(cmd *cobra.Command, args []string) error {
	for _, v := range args {
		printDevice(os.Stdout, &_B, path.Base(path.Dir(v)), v, true)
	}

	return nil
}

func lsSub(cmd *cobra.Command, args []string) error {
	// Each argument to the ls subcommand is a MAC address.
	for _, v := range args {
		rows, err := _B.db.Queryx("SELECT * FROM inventory WHERE device_mac = ? ORDER BY inventory_date;", v)

		if err != nil {
			log.Printf("inventory Queryx error: %v", err)
			continue
		}

		for rows.Next() {
			var ri RecordedInventory

			err = rows.StructScan(&ri)
			if err != nil {
				log.Printf("struct scan failed : %v", err)
				continue
			}

			infoFile := infofileFromRecord(ri, ingestDir)
			content := getContentStatusFromDeviceInfo(infoFile)
			if content == "----" && !lsDetails {
				continue
			}

			fmt.Printf("%v %v %v %v\n",
				infoFile,
				getMfgFromMAC(&_B, ri.DeviceMAC),
				ri.InventoryDate.String(),
				content)

			fmt.Printf("insert or replace into training (fact_id, dgroup_id, site_uuid, device_mac, unix_timestamp) values (00, 0, \"%s\", \"%s\", \"%s\");\n", ri.SiteUUID, ri.DeviceMAC, ri.UnixTimestamp)
		}

		rows.Close()
	}

	return nil
}

func extractSub(cmd *cobra.Command, args []string) error {
	if extractListDHCPParams {
		return extractDHCPRecords(&_B, ingestDir)
	} else if extractListDNSRecords {
		return extractDNSRecords(&_B, ingestDir)
	} else if extractListOUIMfgs {
		return extractMfgs(&_B, ingestDir)
	} else if extractListModels {
		return extractDevices(&_B)
	}

	return errors.New("please specify extraction list")
}

func trainSub(cmd *cobra.Command, args []string) error {
	err := attachBackdropModels(&_B)
	if err != nil {
		return errors.Wrap(err, "attach backdrop models")
	}

	trainDeviceGenusBayesClassifier(&_B, ingestDir)
	trainOSGenusBayesClassifier(&_B, ingestDir)
	trainOSSpeciesBayesClassifier(&_B, ingestDir)

	trainInterfaceMfgLookupClassifier(&_B)

	return nil
}

func readyBackdrop(B *backdrop) error {
	var err error

	B.ouidb, err = oui.OpenStaticFile(ouiFile)
	if err != nil {
		log.Fatalf("unable to open OUI database: %v", err)
	}

	B.db, err = sqlx.Connect("sqlite3", observationsFile)
	if err != nil {
		log.Fatalf("database open: %v\n", err)
	}

	err = B.db.Ping()
	if err != nil {
		log.Fatalf("database ping: %v\n", err)
	}

	checkDB(B.db)

	// If modelFile doesn't exist, don't create it.
	if _, err := os.Stat(modelFile); os.IsNotExist(err) {
		B.modelsLoaded = false
		return nil
	}

	B.modeldb, err = sqlx.Connect("sqlite3", modelFile)
	if err != nil {
		log.Fatalf("model database open: %v\n", err)
	}

	checkModelDB(B.modeldb)

	B.modelsLoaded = true

	if persistent {
		B.persistClassifications = true
	}
	return nil
}

func attachBackdropModels(B *backdrop) error {
	if B.modelsLoaded {
		return nil
	}

	var err error

	B.modeldb, err = sqlx.Connect("sqlite3", modelFile)
	if err != nil {
		log.Fatalf("model database open: %v\n", err)
	}

	checkModelDB(B.modeldb)

	B.modelsLoaded = true
	return nil
}

func closeBackdrop(B *backdrop) error {
	B.db.Close()
	if B.modelsLoaded {
		B.modeldb.Close()
	}
	return nil
}

func main() {
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()

	rootCmd := &cobra.Command{
		Use: "cl-obs",
		PersistentPreRun: func(ccmd *cobra.Command, args []string) {
			if cpuProfile != "" {
				pf, err := os.Create(cpuProfile)
				if err != nil {
					log.Fatalf("CPU profiling file not created: %v", err)
				}

				log.Printf("activating CPU profiling to %s", cpuProfile)
				pprof.StartCPUProfile(pf)
			}

			if ccmd.Name() == "help" {
				return
			}

			readyBackdrop(&_B)
		},
		PersistentPostRun: func(ccmd *cobra.Command, args []string) {
			if cpuProfile != "" {
				pprof.StopCPUProfile()
			}

			if ccmd.Name() == "help" {
				return
			}

			closeBackdrop(&_B)

		},
	}
	rootCmd.PersistentFlags().StringVar(&cpuProfile, "cpuprofile", "", "CPU profiling filename")
	rootCmd.PersistentFlags().StringVar(&ingestDir, "dir", ".", "directory for DeviceInfo files")
	rootCmd.PersistentFlags().StringVar(&observationsFile, "observations-file", "obs.db", "observations index path")
	rootCmd.PersistentFlags().StringVar(&ouiFile, "oui-file", "oui.txt", "OUI text database path")

	siteCmd := &cobra.Command{
		Use:   "site",
		Short: "List sites",
		Args:  cobra.NoArgs,
		RunE:  siteSub,
	}
	siteCmd.Flags().BoolVar(&siteDetails, "verbose", false, "list site details")
	siteCmd.Flags().StringVar(&modelFile, "model-file", "trained-models.db", "path to model file")
	siteCmd.Flags().StringVar(&siteMatch, "match", "", "match site UUID or name pattern")
	rootCmd.AddCommand(siteCmd)

	deviceCmd := &cobra.Command{
		Use:   "device",
		Short: "List devices",
		Args:  cobra.NoArgs,
		RunE:  deviceSub,
	}
	deviceCmd.Flags().BoolVar(&deviceDetails, "verbose", false, "list device details")
	rootCmd.AddCommand(deviceCmd)

	lsCmd := &cobra.Command{
		Use:   "ls",
		Short: "List deviceInfos for matching MACs",
		Args:  cobra.MinimumNArgs(1),
		RunE:  lsSub,
	}
	lsCmd.Flags().BoolVar(&lsDetails, "verbose", false, "detailed output")
	rootCmd.AddCommand(lsCmd)

	catCmd := &cobra.Command{
		Use:   "cat",
		Short: "Print deviceInfos",
		Args:  cobra.MinimumNArgs(1),
		RunE:  catSub,
	}
	rootCmd.AddCommand(catCmd)

	ingestCmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest device info files from tree",
		Args:  cobra.NoArgs,
		RunE:  ingestSub,
	}
	ingestCmd.Flags().IntVar(&ingestCap, "ingest-cap", 0, "maximum files to ingest")
	rootCmd.AddCommand(ingestCmd)

	extractCmd := &cobra.Command{
		Use:   "extract",
		Short: "Extract ingested data",
		Args:  cobra.NoArgs,
		RunE:  extractSub,
	}
	extractCmd.Flags().BoolVar(&extractListDHCPParams, "dhcp", false, "list device DHCP parameters")
	extractCmd.Flags().BoolVar(&extractListDNSRecords, "dns", false, "list device DNS queries")
	extractCmd.Flags().BoolVar(&extractListOUIMfgs, "mfg", false, "list OUI manufacturers")
	extractCmd.Flags().BoolVar(&extractListModels, "device", false, "list devices")
	extractCmd.Flags().StringVar(&extractOutput, "output", "obs-grid.out", "path for output file")
	rootCmd.AddCommand(extractCmd)

	trainCmd := &cobra.Command{
		Use:   "train",
		Short: "Train classifier",
		Args:  cobra.NoArgs,
		RunE:  trainSub,
	}
	trainCmd.Flags().StringVar(&trainSelect, "classifier", "bayes-os", "select classifier to run [bayes-os, bayes-device]")
	trainCmd.Flags().StringVar(&modelFile, "model-file", "trained-models.db", "output path to write classifier")
	rootCmd.AddCommand(trainCmd)

	reviewCmd := &cobra.Command{
		Use:   "review",
		Short: "Review training data and classifiers",
		Args:  cobra.NoArgs,
		RunE:  reviewSub,
	}
	reviewCmd.Flags().BoolVar(&reviewDetails, "verbose", false, "detailed output")
	reviewCmd.Flags().StringVar(&modelFile, "model-file", "trained-models.db", "path to model file")
	rootCmd.AddCommand(reviewCmd)

	classifyCmd := &cobra.Command{
		Use:   "classify",
		Short: "Classify device",
		Args:  cobra.MinimumNArgs(1),
		RunE:  classifySub,
	}
	classifyCmd.Flags().StringVar(&modelFile, "model-file", "trained-models.db", "path to model file")
	classifyCmd.Flags().BoolVar(&persistent, "persist", false, "record classifications")
	rootCmd.AddCommand(classifyCmd)

	dnsINRequestRE = regexp.MustCompile(dnsINRequestPat)

	initMaps()

	err = rootCmd.Execute()
	os.Exit(map[bool]int{true: 0, false: 1}[err == nil])
}
