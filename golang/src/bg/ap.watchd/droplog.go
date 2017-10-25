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

package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bg/ap_common/aputil"

	"database/sql"
	_ "github.com/mattn/go-sqlite3"
)

const (
	minRecords = 5000
	maxRecords = 10000
)

var (
	logpipe = flag.String("logpipe", "/var/tmp/bgpipe",
		"rsyslog named pipe to monitor")
	dropDB = flag.String("dropdb", "drop.db", "database of drop records")

	lanDrops, wanDrops *dropTable

	dbHandle *sql.DB

	wanIface string
)

type dropTable struct {
	name  string
	minId int
	maxId int
}

// Default logfile format:
//
// Sep 19 17:20:59 bgrouter kernel: [271855.655121] DROPPED
//             IN=brvlan5 OUT=brvlan4 MAC=9c:ef:d5:fe:e8:36:b8:27:eb:19:0f:23:08:00
//             SRC=192.168.137.13 DST=192.168.136.4 LEN=60 TOS=0x00 PREC=0x00 TTL=63
//             ID=35144 DF PROTO=TCP SPT=55276 DPT=22 WINDOW=29200 RES=0x00 SYN URGP=0
//
// Log entries sent to a named pipe should have a compatible format, if rsyslog
// is configured as follows:
//
//    $template BGFormat,"%timegenerated% %msg:::drop-last-lf%\n"
//    :msg, contains, "DROPPED IN" |/var/tmp/bgpipe ; BGFormat
//    & ~
//

const dropSchema = `
		Id	int PRIMARY KEY,
		Time	timestamp NOT NULL,
		InDev	text,
		OutDev	text,
		SrcIP	text,
		DstIP	text,
		SrcMAC	text,
		SrcPort	int,
		DstPort	int,
		Proto	text
	`

type dropRecord struct {
	id     int
	time   time.Time
	indev  string
	outdev string
	src    net.IP
	dst    net.IP
	smac   string
	sprt   int
	dprt   int
	proto  string
}

func countDrop(d *dropRecord) {
	dstIP := d.dst.String()
	srcIP := d.src.String()
	dport := strconv.Itoa(d.dprt)
	sport := strconv.Itoa(d.sprt)

	// Bump the 'outgoing blocks' count of the originating device
	if rec := GetDeviceRecord(d.smac); rec != nil {
		p := GetProtoRecord(rec, d.proto)
		if p != nil {
			tgt := dstIP + ":" + dport
			p.OutgoingBlocks[tgt]++
		}
		ReleaseDeviceRecord(rec)
	}

	// If the target device was local to our LAN, bump its 'incoming
	// blocks' count.
	if d.outdev != wanIface {
		if rec := GetDeviceRecordByIP(dstIP); rec != nil {
			p := GetProtoRecord(rec, d.proto)
			if p != nil {
				src := srcIP + ":" + sport
				p.IncomingBlocks[src]++
			}
			ReleaseDeviceRecord(rec)
		}
	}
}

func recordDrop(d *dropRecord) *dropTable {
	var table *dropTable

	if d.indev == wanIface {
		table = wanDrops
	} else {
		table = lanDrops
	}

	table.maxId++
	d.id = table.maxId
	columns := "Id, Time, InDev, OutDev, SrcIP, DstIP, " +
		"SrcMAC, SrcPort, DstPort, Proto"

	qs := "INSERT INTO " + table.name + "(" + columns +
		") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"

	_, err := dbHandle.Exec(qs, d.id, d.time, d.indev, d.outdev, d.src,
		d.dst, d.smac, d.sprt, d.dprt, d.proto)
	if err != nil {
		log.Printf("failed to insert drop: %v\n", err)
	}

	return table
}

func getDrop(line string) *dropRecord {
	var d dropRecord

	// Use a regular expression to extract the date and details of a
	// dropped packet message
	re := regexp.MustCompile(`(\S+ \S+ \S+) (.* DROPPED )(.*)`)

	if !re.MatchString(line) {
		// Ignore any log messages that don't look like drops
		return nil
	}
	l := re.FindStringSubmatch(line)

	// The first matched expression is the date
	when, err := time.Parse("Jan 2 15:04:05", l[1])
	if err == nil {
		year := time.Now().Year()
		d.time = when.AddDate(year, 0, 0)
	} else {
		fmt.Printf("Failed to read time (%s): %v\n", l[1], err)
	}

	// The third match contains the contents of the DROP message
	for _, field := range strings.Split(l[3], " ") {
		var key, val string

		f := strings.SplitN(field, "=", 2)
		key = strings.ToLower(f[0])
		if len(f) > 1 {
			val = strings.ToLower(f[1])
		}
		switch key {
		case "in":
			d.indev = val
		case "out":
			d.outdev = val
		case "src":
			d.src = net.ParseIP(val)
		case "dst":
			d.dst = net.ParseIP(val)
		case "mac":
			// The MAC field contains both the source and
			// destination MAC addresses.  Because we only drop
			// packets that are crossing (v)LAN boundaries, the
			// destination MAC address is generally meaningless.
			if len(f) > 1 {
				all := strings.Split(val, ":")
				if len(all) >= 12 {
					d.smac = strings.Join(all[6:12], ":")
				}
			}
		case "spt":
			d.sprt, _ = strconv.Atoi(val)
		case "dpt":
			d.dprt, _ = strconv.Atoi(val)
		case "proto":
			d.proto = val
		}
	}
	if d.indev == "" && d.outdev == "" {
		log.Printf("bad line: %s\n", line)
		return nil
	}

	return &d
}

func getVal(db *sql.DB, table, val string) int {
	id := 0

	query := "SELECT " + val + "(id) FROM " + table
	rows, err := db.Query(query)
	if err != nil {
		log.Printf("'%s' failed: %v\n", query, err)
	} else {
		defer rows.Close()
		rows.Next()
		if err = rows.Scan(&id); err != nil {
			log.Printf("failed to find %s ID: %v", val, err)
		}
	}
	return id
}

func tableInit(db *sql.DB, table string) (*dropTable, error) {
	var t dropTable

	sqlStmt := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s ( %s );",
		table, dropSchema)
	if _, err := db.Exec(sqlStmt); err != nil {
		return nil, fmt.Errorf("failed to create table: %v", err)
	}

	t.name = table
	t.minId = getVal(db, table, "MIN")
	t.maxId = getVal(db, table, "MAX")

	log.Printf("%s IDs %d - %d\n", table, t.minId, t.maxId)
	return &t, nil
}

func dbInit(name string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", name)
	if err != nil {
		err = fmt.Errorf("failed to connect to %s: %v", name, err)
	}

	if lanDrops, err = tableInit(db, "landrops"); err != nil {
		return nil, err
	}

	if wanDrops, err = tableInit(db, "wandrops"); err != nil {
		return nil, err
	}

	return db, nil
}

func trimTable(t *dropTable) {
	if t.maxId-t.minId <= maxRecords {
		return
	}

	log.Printf("%s table has %d entries.  Trimming to %d.\n",
		t.name, t.maxId-t.minId, minRecords)

	newMin := t.maxId - minRecords
	stmnt := fmt.Sprintf("DELETE FROM %s WHERE id < %d", t.name, newMin)
	if res, err := dbHandle.Exec(stmnt, 1); err != nil {
		log.Printf("'%s' failed: %v\n", stmnt, err)
	} else {
		deleted, _ := res.RowsAffected()
		log.Printf("Deleted %d entries\n", deleted)
		t.minId = newMin
	}
}

func logMonitor(logpipe *os.File) {
	scanner := bufio.NewScanner(logpipe)
	for scanner.Scan() {
		d := getDrop(scanner.Text())
		if d != nil {
			t := recordDrop(d)
			trimTable(t)

			// We only maintain per-device firewall statistics for
			// packets that originate within our LAN.
			if d.indev != wanIface {
				countDrop(d)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("error processing log pipe: %v\n", err)
	}
}

func openPipe(name string) (pipe *os.File, err error) {
	if !aputil.FileExists(name) {
		log.Printf("Creating named pipe %s for log input\n", name)
		if err = syscall.Mkfifo(name, 0600); err != nil {
			err = fmt.Errorf("Failed to create %s: %v\n", name, err)
			return
		}

		log.Printf("Restarting rsyslogd\n")
		c := exec.Command("/bin/systemctl", "restart", "rsyslog")
		if err = c.Run(); err != nil {
			err = fmt.Errorf("failed to restart rsyslogd: %v", err)
			return
		}
	}

	pipe, err = os.OpenFile(name, os.O_RDONLY, os.ModeNamedPipe)
	return
}

func droplogFini() {
	dbHandle.Close()
}

func droplogInit() error {
	wanIface, _ = config.GetProp("@/network/wan_nic")
	pipe, err := openPipe(*logpipe)
	if err != nil {
		return fmt.Errorf("failed to open syslog pipe %s: %v",
			*logpipe, err)
	}

	dbHandle, err = dbInit(*watchDir + "/" + *dropDB)
	if err != nil {
		return fmt.Errorf("database error: %v", err)
	}

	go logMonitor(pipe)
	return nil
}

func init() {
	addWatcher("droplog", droplogInit, droplogFini)
}
