/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

/*
 *                                    +-----------------+
 *                                    |     Matches     |
 *                                    |-----------------|
 *                                    | int  MatchId    |
 *                                +---| text Char. List |
 *       +---------------------+  | +-| int  DeviceID   |
 *       |   Characteristics   |  | | +-----------------+
 *       |---------------------|  | |
 *       | int  Index          |<-+ |
 *    +->| text Characteristic |    |
 *    |  +---------------------+    |     +--------------------------------+
 *    |                             |     | Devices                        |
 *    |  +---------------+          |     |--------------------------------|
 *    |  | Manufacturers |          +---->| int      DeviceId              |
 *    |  |---------------|                | bool     Obsolete              |
 *    +--| int  MfgId    |< - -           | time     UpdateTime            |
 *       | text Name     |    | (future)  | text     Devtype               |
 *       +---------------+    +- - - - - -| text     Vendor (opt.)         |
 *                                        | text     ProductName (opt.)    |
 *                                        | text     ProductVersion (opt.) |
 *                                        | intarray UDPPorts (opt.)       |
 *                                        | intarray InboundPorts (opt.)   |
 *                                        | intarray OutBoundPorts (opt.)  |
 *                                        | strarray DNS (opt.)            |
 *                                        | text     Notes (opt.)          |
 *                                        +--------------------------------+
 *
 * The database currently holds 4 tables.
 *
 * The 'devices' table holds all of the information we know about a particular
 * device, and is used to generate (or can be generated from) devices.json.  Some
 * of the fields worth explanation:
 *
 *    DeviceID: This is a unique, monotonically increasing index
 *
 *    Obsolete: When a device record has been replaced (e.g. when two different
 *              device definitions merge), this field is set to 'true'.  This can
 *              be used to indicate that we should not export this record to
 *              clients, and that this device should not be included in the
 *              identifier pool
 *
 *    UpdateTime: When the record was last updated.  This can be used to identify
 *              which client-side records need to be updated.  (note: This is in
 *              lieu of the integral 'database version' originally envisioned).
 *
 *    Vendor: An unstructured text string, just used for reporting in the user
 *              interface.  Ideally we would have canonical names for each vendor,
 *              and this string could be replaced by an index into the
 *              Manufacturers table.
 *
 *    The int and string arrays are encoded as text strings, which need to be
 *    parsed when the record is exported to JSON.
 *
 * The 'manufacturers' table is used to generate ap_mfg.json.  This table is used
 * by the indentifier to translate observed text strings into manufacturer IDs.
 * Those IDs are used as identifying characteristics, which appear in the
 * 'characteristics' table.
 *
 * The 'characteristics' table contains the names of all characteristics recognized
 * by the identifier.  Each characteristic is represented by a simple string, which
 * is interpreted by the identifier.  Conceivably we could populate this table with
 * more structured data (e.g. "manufacturer: id", "tcp: port#", etc.).  This table
 * is used to generate the first line of ap_identities.csv.
 *
 * The 'matches' table contains the bulk of the identifier knowledge, and is used
 * to generate the remaining lines of ap_identities.csv.  Each record contains a
 * list of characteristics (corresponding to Indices in the characteristics table),
 * and the ID of the unique device that matches those characteristics.  Note: while
 * each set of characteristics resolves to a single device, a single device may
 * be resolved by multiple sets of characteristics.
 *
 * While these tables reference each other, there is no verification built into the
 * schema.  We could probably define the "matches -> devices" reference as a
 * foreign key, but the other linkages are embedded in opaque text strings where
 * the database has no means to interpret them.
 */

package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"ap_common/device"

	"github.com/lib/pq"
)

const (
	devTable   = "devices"
	mfgTable   = "manufacturers"
	charTable  = "characteristics"
	matchTable = "matches"

	dbUser     = "nils"
	dbPassword = ""
	dbName     = "nils"
	dbSSL      = "disable"
)

const devSchema = `
		Devid          integer NOT NULL PRIMARY KEY,
		Obsolete       boolean NOT NULL,
		UpdateTime     timestamp NOT NULL,
		Devtype        text NOT NULL,
		Vendor         text,
		ProductName    text,
		ProductVersion text,
		UDPPorts       text,
		InboundPorts   text,
		OutboundPorts  text,
		DNS            text,
		Notes          text
	`

const charSchema = `
		Index		integer NOT NULL PRIMARY KEY,
		Characteristic	text NOT NULL
	`

const mfgSchema = `
		MfgId	integer PRIMARY KEY,
		Name 	text NOT NULL
	`

const matchSchema = `
		MatchID		integer NOT NULL,
		Characteristics	text NOT NULL,
		Device 		integer NOT NULL
	`

var tables = map[string]string{
	devTable:   devSchema,
	charTable:  charSchema,
	mfgTable:   mfgSchema,
	matchTable: matchSchema,
}

type match struct {
	matchid int
	charstr string
	devid   int
}

var (
	devFile = flag.String("dev", "", "device JSON file")
	idFile  = flag.String("id", "", "identifier CSV")
	mfgFile = flag.String("mfg", "", "manufacturer json")
	impFlag = flag.Bool("import", false, "import into database")
	expFlag = flag.Bool("export", false, "export from database")
)

var (
	devices         device.DeviceMap
	manufacturers   map[string]int
	characteristics []string
	matches         []match
)

///////////////////////////////////////////////////
//
// Database interaction routines
//
func connectDB() (db *sql.DB, err error) {
	dbinfo := fmt.Sprintf("user=%s password=%s dbname=%s sslmode=%s",
		dbUser, dbPassword, dbName, dbSSL)

	db, err = sql.Open("postgres", dbinfo)

	if err != nil {
		err = fmt.Errorf("failed to connect to %s: %v", dbName, err)
	}
	return
}

func createTable(db *sql.DB, name, schema string) error {
	sqlStmt := fmt.Sprintf("DROP TABLE IF EXISTS %s;", name)
	if _, err := db.Exec(sqlStmt); err != nil {
		return fmt.Errorf("failed to drop old %s table: %v", name, err)
	}

	sqlStmt = fmt.Sprintf("CREATE TABLE %s (%s);", name, schema)
	if _, err := db.Exec(sqlStmt); err != nil {
		return fmt.Errorf("failed to create %s table: %v", name, err)
	}
	return nil
}

func createTables(db *sql.DB) error {
	for t, s := range tables {
		fmt.Printf("Creating %s\n", t)
		err := createTable(db, t, s)
		if err != nil {
			return fmt.Errorf("failed to create %s: %v", t, err)
		}
	}
	return nil
}

func insertRow(db *sql.DB, table, col, val string) error {
	stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);\n", table, col, val)
	_, err := db.Exec(stmt)
	return err
}

//////////////////////////////////////////////////////////////////////////////
//
// Utility functions for converting back and forth between the optional and
// variable-length arrays in the device structure and their serialized versions
// in the database.
//

// Strip out any single quotes from a string
func cleanQuote(in string) string {
	return strings.Replace(in, "'", "", -1)
}

// add an argument to an SQL statement
func addArg(c, v *string, f, s string) {
	*c += ", " + f
	*v += fmt.Sprintf(", '%s'", cleanQuote(s))
}

func addStringColumn(c, v *string, f, s string) {
	if s != "" {
		addArg(c, v, f, s)
	}
}

func getStringValue(f *string) string {
	s := ""
	if f != nil {
		s = *f
	}
	return s
}

// convert a slice of ints into a single-string argument
func addIntArrayColumn(c, v *string, f string, a []int) {
	if len(a) > 0 {
		// Given a slice of integers, build a string of 'int,int,int'
		s := ""
		for idx, i := range a {
			if idx != 0 {
				s += ","
			}
			s += strconv.Itoa(i)
		}
		addArg(c, v, f, s)
	}
}

func getIntArrayValue(f *string) []int {
	var r []int

	if f != nil {
		a := strings.Split(*f, ",")
		r = make([]int, len(a))

		for idx, n := range a {
			r[idx], _ = strconv.Atoi(n)
		}
	}

	return r
}

// convert a slice of strings into a single-string SQL argument
func addStrArrayColumn(c, v *string, f string, a []string) {
	if len(a) > 0 {
		s := cleanQuote(strings.Join(a, ","))
		addArg(c, v, f, s)
	}
}

func getStrArrayValue(f *string) []string {
	var r []string

	if f != nil {
		r = strings.Split(*f, ",")
	}

	return r
}

/////////////////////////////////////////////////////////////////////////
//
// Routines for populating the database from our internal representations
//
func insertOneDevice(db *sql.DB, devid uint32, dev *device.Device) error {
	tm := string(pq.FormatTimestamp(dev.UpdateTime))

	columns := "Devid, Obsolete, UpdateTime, Devtype"
	values := fmt.Sprintf("'%d', '%v', '%v', '%s'", devid, dev.Obsolete, tm, dev.Devtype)

	addStringColumn(&columns, &values, "Vendor", dev.Vendor)
	addStringColumn(&columns, &values, "ProductName", dev.ProductName)
	addStringColumn(&columns, &values, "ProductVersion", dev.ProductVersion)
	addIntArrayColumn(&columns, &values, "UDPPorts", dev.UDPPorts)
	addIntArrayColumn(&columns, &values, "InboundPorts", dev.InboundPorts)
	addIntArrayColumn(&columns, &values, "OutboundPorts", dev.OutboundPorts)
	addStrArrayColumn(&columns, &values, "DNS", dev.DNS)
	addStringColumn(&columns, &values, "Notes", dev.Notes)

	err := insertRow(db, devTable, columns, values)
	if err != nil {
		err = fmt.Errorf("failed to insert dev %v: %v", *dev, err)
	}

	return err
}

func insertOneMfg(db *sql.DB, id int, name string) error {
	columns := "MfgId, Name"
	values := fmt.Sprintf("'%d', '%s'", id, name)
	err := insertRow(db, mfgTable, columns, values)
	if err != nil {
		err = fmt.Errorf("failed to insert mfg %s: %v", name, err)
	}

	return err
}

func insertOneCharacteristic(db *sql.DB, index int, char string) error {
	columns := "Index, Characteristic"
	values := fmt.Sprintf("'%d', '%s'", index, char)
	err := insertRow(db, charTable, columns, values)
	if err != nil {
		err = fmt.Errorf("failed to insert char %s: %v", char, err)
	}

	return err
}

func insertOneMatch(db *sql.DB, m match) error {
	columns := "MatchID, Characteristics, Device"
	values := fmt.Sprintf("'%d', '%s', '%d'", m.matchid, m.charstr, m.devid)
	err := insertRow(db, matchTable, columns, values)
	if err != nil {
		err = fmt.Errorf("failed to insert match %d: %v", m.matchid, err)
	}

	return err
}

func populateDatabase(db *sql.DB) error {
	for id, d := range devices {
		if err := insertOneDevice(db, id, d); err != nil {
			return err
		}
	}

	for i, c := range characteristics {
		if err := insertOneCharacteristic(db, i, c); err != nil {
			return err
		}
	}

	for n, i := range manufacturers {
		if err := insertOneMfg(db, i, n); err != nil {
			return err
		}
	}

	for _, m := range matches {
		if err := insertOneMatch(db, m); err != nil {
			return err
		}
	}

	return nil
}

/////////////////////////////////////////////////////////////////////////
//
// Routines for importing data from flat json/csv files into our
// internal representations
//

func importDevices(fileName *string) error {
	var err error

	if *fileName == "" {
		err = fmt.Errorf("import requires a device database")
	} else {
		devices, err = device.DevicesLoad(*fileName)
	}

	return err
}

func exportDevices(name string) error {
	s, err := json.MarshalIndent(devices, "", "  ")
	if err != nil {
		err = fmt.Errorf("failed to construct device JSON: %v", err)
	} else if err = ioutil.WriteFile(name, s, 0644); err != nil {
		err = fmt.Errorf("failed to write device JSON file: %vn", err)
	}

	return err
}

// Import the identities CSV file, and generate the characteristics list and the
// characteristics -> device map.
//
func importIds(fileName *string) error {
	var file []byte
	var err error

	if *fileName == "" {
		return fmt.Errorf("import requires an identifier database")
	}
	if file, err = ioutil.ReadFile(*fileName); err != nil {
		return fmt.Errorf("failed to load identifiers from %s: %v",
			fileName, err)
	}

	r := csv.NewReader(strings.NewReader(string(file)))
	line, err := r.Read()
	if err != nil {
		return fmt.Errorf("failed to parse ID file %s: %v", *fileName, err)
	}

	// The first line of the CSV contains the characteristics
	fields := len(line)
	characteristics = make([]string, fields)
	for i, c := range line {
		characteristics[i] = c
	}

	// Each subsequent line records the characteristics for a single device
	matches = make([]match, 0)
	row := 0
	for {
		row++
		if line, err = r.Read(); line == nil {
			break
		}

		if len(line) != fields {
			fmt.Printf("%d has %d fields - needs %d\n",
				row, len(line), fields)
			continue
		}

		vals := line[0 : fields-1]
		id, _ := strconv.Atoi(line[fields-1])

		// Build a list of the '1' characteristics
		charstr := ""
		delim := ""
		for j, c := range vals {
			if c == "1" {
				charstr += delim
				charstr += strconv.Itoa(j)
				delim = ","
			}
		}
		match := match{
			matchid: row,
			charstr: charstr,
			devid:   id,
		}
		matches = append(matches, match)
	}

	return nil
}

func importManufacturers(fileName *string) error {
	var file []byte
	var err error

	if *fileName == "" {
		return fmt.Errorf("import requires a manufacturer database")
	}
	if file, err = ioutil.ReadFile(*fileName); err != nil {
		return fmt.Errorf("failed to load manufacturers from %s: %v",
			*fileName, err)
	}
	if err = json.Unmarshal(file, &manufacturers); err != nil {
		return fmt.Errorf("failed to import manufacturers from %s: %v",
			*fileName, err)
	}

	return nil
}

func importData(db *sql.DB) error {
	if err := importDevices(devFile); err != nil {
		return err
	}
	if err := importIds(idFile); err != nil {
		return err
	}
	if err := importManufacturers(mfgFile); err != nil {
		return err
	}

	if err := createTables(db); err != nil {
		return err
	}

	return nil
}

/////////////////////////////////////////////////////////////////////////
//
// Routines for populating our internal representations from tables in the
// database
//

// Retrieve all rows from the manufacturers table, and use them to populate the
// manufacturers map
//
func fetchManufacturers(db *sql.DB) error {
	fmt.Printf("Fetching manufacturers\n")
	rows, err := db.Query("SELECT * FROM " + mfgTable)
	if err != nil {
		return fmt.Errorf("failed to retrieve manufacturer data: %v", err)
	}
	defer rows.Close()

	manufacturers = make(map[string]int)
	for rows.Next() {
		var name string
		var id int

		if err := rows.Scan(&id, &name); err != nil {
			return fmt.Errorf("failed to extract manufacturer data: %v", err)
		}
		manufacturers[name] = id
	}
	return nil
}

// Retrieve all rows from the characteristics table, and use them to populate the
// ordered characteristics array
//
func fetchCharacteristics(db *sql.DB) error {
	fmt.Printf("Fetching characteristics\n")
	rows, err := db.Query("SELECT * FROM " + charTable)
	if err != nil {
		return fmt.Errorf("failed to retrieve characteristic data: %v", err)
	}
	defer rows.Close()

	unordered := make(map[int]string)
	for rows.Next() {
		var index int
		var char string

		if err := rows.Scan(&index, &char); err != nil {
			return fmt.Errorf("failed to extract characteristic: %v", err)
		}
		unordered[index] = char
	}

	characteristics = make([]string, len(unordered))
	for i, c := range unordered {
		characteristics[i] = c
	}

	return nil
}

// Retrieve all rows from the matches table, and use them to populate the
// matches array
//
func fetchMatches(db *sql.DB) error {
	fmt.Printf("Fetching matches\n")
	rows, err := db.Query("SELECT * FROM " + matchTable)
	if err != nil {
		return fmt.Errorf("failed to retrieve match data: %v", err)
	}
	defer rows.Close()

	matches = make([]match, 0)
	for rows.Next() {
		var char string
		var matchid, devid int

		if err := rows.Scan(&matchid, &char, &devid); err != nil {
			return fmt.Errorf("failed to extract match data: %v", err)
		}
		match := match{
			matchid: matchid,
			charstr: char,
			devid:   devid,
		}
		matches = append(matches, match)
	}
	return nil
}

// Build a single Device struct from its database row
//
func extractOneDevice(rows *sql.Rows) error {
	var d device.Device

	var (
		devid          uint32
		vendor         *string
		productName    *string
		productVersion *string
		udpPorts       *string
		inboundPorts   *string
		outboundPorts  *string
		dns            *string
		notes          *string
	)
	err := rows.Scan(&devid, &d.Obsolete, &d.UpdateTime,
		&d.Devtype, &vendor, &productName, &productVersion,
		&udpPorts, &inboundPorts, &outboundPorts, &dns, &notes)
	if err != nil {
		return err
	}

	d.Vendor = getStringValue(vendor)
	d.ProductName = getStringValue(productName)
	d.ProductVersion = getStringValue(productVersion)
	d.Notes = getStringValue(notes)
	d.UDPPorts = getIntArrayValue(udpPorts)
	d.InboundPorts = getIntArrayValue(inboundPorts)
	d.OutboundPorts = getIntArrayValue(outboundPorts)
	d.DNS = getStrArrayValue(dns)

	devices[devid] = &d

	return nil
}

//
// Retrieve all rows from the devices table, and use them to populate the
// devices map
//
func fetchDevices(db *sql.DB) error {
	fmt.Printf("Fetching devices\n")

	rows, err := db.Query("SELECT * FROM " + devTable)
	if err != nil {
		return fmt.Errorf("failed to retrieve data: %v", err)
	}
	defer rows.Close()

	devices = make(device.DeviceMap)
	for rows.Next() {
		if err := extractOneDevice(rows); err != nil {
			return fmt.Errorf("Failed to process row: %v", err)
		}
	}
	return nil
}

func fetchData(db *sql.DB) error {
	if err := fetchDevices(db); err != nil {
		return fmt.Errorf("failed to fetch device data: %v", err)
	}

	if err := fetchManufacturers(db); err != nil {
		return fmt.Errorf("failed to fetch manufacturer data: %v", err)
	}

	if err := fetchCharacteristics(db); err != nil {
		return fmt.Errorf("failed to fetch characteristics data: %v", err)
	}

	if err := fetchMatches(db); err != nil {
		return fmt.Errorf("failed to fetch matches data: %v", err)
	}

	return nil
}

/////////////////////////////////////////////////////////////////////////
//
// Routines for exporting data from our internal representations into
// flat json/csv files.
//

func exportIDs(fileName string) error {
	file, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("failed to create %s: %v",
			fileName, err)
	}
	defer file.Close()
	w := csv.NewWriter(file)

	columns := len(characteristics)
	row := make([]string, columns)
	for i, c := range characteristics {
		row[i] = c
	}
	w.Write(row)

	for _, m := range matches {
		row = make([]string, columns)

		// The default value for each characteristic is '0'
		for i := 0; i < columns-1; i++ {
			row[i] = "0"
		}

		for _, s := range strings.Split(m.charstr, ",") {
			idx, err := strconv.Atoi(s)
			if err != nil || (idx >= columns-1) {
				fmt.Printf("Invalid index: %s\n", s)
				fmt.Printf("  %d: %s\n", m.matchid, m.charstr)
			} else {
				row[idx] = "1"
			}
		}
		row[columns-1] = strconv.Itoa(m.devid)
		w.Write(row)
	}
	w.Flush()

	return nil
}

func exportManufacturers(name string) error {
	s, err := json.MarshalIndent(manufacturers, "", "  ")
	if err != nil {
		err = fmt.Errorf("failed to construct manufacturer JSON: %v", err)
	} else if err = ioutil.WriteFile(name, s, 0644); err != nil {
		err = fmt.Errorf("failed to write manufacturers file: %vn", err)
	}

	return err
}

func fileCheck(filename *string) error {
	if _, err := os.Stat(*filename); err == nil {
		return fmt.Errorf("%s already exists", *filename)
	}
	return nil
}

func exportData(db *sql.DB) error {
	var err error

	if err = fileCheck(devFile); err == nil {
		if err = fileCheck(mfgFile); err == nil {
			err = fileCheck(idFile)
		}
	}
	if err != nil {
		return err
	}

	if *devFile != "" {
		if err = exportDevices(*devFile); err != nil {
			return fmt.Errorf("failed to export devices: %v", err)
		}
	}

	if *mfgFile != "" {
		if err = exportManufacturers(*mfgFile); err != nil {
			return fmt.Errorf("failed to export manufacturers: %v",
				err)
		}
	}

	if *idFile != "" {
		if err = exportIDs(*idFile); err != nil {
			return fmt.Errorf("failed to export identities: %v", err)
		}
	}
	return nil
}

func usage(name string) {
	fmt.Printf("Usage: %s < -import | -export > -dev <device.json> -id <id.csv> -mfg <mfg.json>\n",
		name)
	os.Exit(1)
}

func main() {
	flag.Parse()

	imp := (impFlag != nil) && *impFlag
	exp := (expFlag != nil) && *expFlag
	if imp == exp {
		usage(os.Args[0])
	}

	db, err := connectDB()
	if err != nil {
		fmt.Printf("Failed to connect to DB: %v\n", err)
		os.Exit(1)
	}

	if imp {
		if err = importData(db); err == nil {
			err = populateDatabase(db)
		}
	} else {
		if err = fetchData(db); err == nil {
			err = exportData(db)
		}
	}

	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
}
