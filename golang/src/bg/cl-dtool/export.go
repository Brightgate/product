/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


/*
 * Todo - reimplement import as an io.Reader.  The import/export will then be
 * done as a simple io.Copy() operation.  Currently the amount of memory
 * required for an export is bounded by the size of the input set.  A streaming
 * model will let us set an arbitrarily small ceiling.
 *
 * We should explore adding AVRO as a supported target format.  This is a
 * binary format, which will consume less space and which BigQuery can ingest
 * more quickly.
 */

package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"bg/common/archive"
)

// This is the format BigQuery expects
const timeFmt = "2006-01-02 15:04:05"

var (
	// CSV file headers
	dropHdr = "TIME,UUID,NET,INDEV,SRC,SPORT,DST,DPORT,SMAC,PROTO\n"
	statHdr = "START,END,UUID,MAC,LADDR,LPORT,RADDR,RPORT," +
		"PKTSSENT,PKTSRCVD,BYTESSENT,BYTESRCVD\n"
	portHdr string // constructed from tcpTrack and udpTrack lists

	// Interesting ports that we want to get their own columns, making the
	// devices easy to search for.
	tcpTrack = []int{
		21, 22, 23, 25, 80, 111, 137, 138, 139, 143, 389, 443, 445,
		554, 631, 2049, 3306, 3389, 4000, 4444, 5353, 5432, 6000,
		8000, 8080}
	udpTrack = []int{53, 111, 137, 138, 139, 389, 445, 3306, 3389}
)

// Exporter provides a source for a CSV generator.  Its internal state tracks
// progress through an imported dataset as it is incrementally converted into a
// CSV stream.
type Exporter interface {
	init([]object) error // Import all data.  Init export state.
	ctype() string       // content type of the exported data

	// The following 3 routines provide a simple model allowing a common
	// read() routine to iterate through the imported data.
	line() string // emit a single CSV line
	advance()     // advance to the next record to be exported
	done() bool   // has all data been exported?

	io.Reader
}

// Generate as many CSV lines from the imported data as will fit into the
// provided buffer.
func read(ex Exporter, p []byte) (int, error) {
	var err error
	var n int

	if ex.done() {
		return 0, io.EOF
	}

	for !ex.done() {
		line := ex.line()
		if line == "" || len(line)+n >= len(p) {
			break
		}
		copy(p[n:n+len(line)], []byte(line))
		n += len(line)
		ex.advance()
	}

	if n == 0 {
		err = io.ErrShortBuffer
	}
	return n, err
}

/**************************************************************
 *
 * Support for archives of firewall drop records
 */

const (
	listLan = iota
	listWan
)

type dropExporter struct {
	data []archive.DropArchive // imported data

	archiveIdx int // archive entry currently being exported
	dropList   int // exporting lan or wan list?
	dropIdx    int // index into the current list

	Exporter
}

func (ex *dropExporter) advanceArchive() {
	ex.archiveIdx++
	if ex.archiveIdx >= len(ex.data) {
		return
	}
	a := ex.data[ex.archiveIdx]
	ex.dropIdx = 0

	if len(a.LanDrops) > 0 {
		ex.dropList = listLan
	} else if len(a.WanDrops) > 0 {
		ex.dropList = listWan
	} else {
		ex.advanceArchive()
	}
}

func (ex *dropExporter) advance() {
	if ex.archiveIdx == -1 {
		ex.advanceArchive()
		return
	}

	a := ex.data[ex.archiveIdx]
	ex.dropIdx++
	if ex.dropList == listLan {
		if ex.dropIdx < len(a.LanDrops) {
			return
		}
		ex.dropIdx = 0
		ex.dropList = listWan
	}
	if ex.dropList == listWan {
		if ex.dropIdx >= len(a.WanDrops) {
			ex.advanceArchive()
		}
	}
}

func (ex *dropExporter) line() string {
	var rec *archive.DropRecord
	var network, src, sport, dst, dport string

	if ex.archiveIdx == -1 {
		return dropHdr
	}
	if ex.archiveIdx >= len(ex.data) {
		return ""
	}
	archive := ex.data[ex.archiveIdx]

	if ex.dropList == listLan {
		network = "lan"
		rec = archive.LanDrops[ex.dropIdx]
	} else {
		network = "wan"
		rec = archive.WanDrops[ex.dropIdx]
	}

	f := strings.Split(rec.Src, ":")
	src = f[0]
	if len(f) > 1 {
		sport = f[1]
	}
	f = strings.Split(rec.Dst, ":")
	dst = f[0]
	if len(f) > 1 {
		dport = f[1]
	}

	return fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
		rec.Time.Format(timeFmt), *uuidFlag, network, rec.Indev,
		src, sport, dst, dport, rec.Smac, rec.Proto)
}

func (ex *dropExporter) Read(p []byte) (n int, err error) {
	return read(ex, p)
}

func (ex *dropExporter) done() bool {
	return ex == nil || ex.archiveIdx >= len(ex.data)
}

func (ex *dropExporter) ctype() string {
	return "application/drops-csv"
}

func importOneDropArchive(obj object) ([]archive.DropArchive, error) {
	var list []archive.DropArchive

	slog.Debugf("  fetching %s\n", obj)
	data, err := readData(obj.name)
	if err != nil {
		err = fmt.Errorf("failed to fetch %s: %v", obj, err)
	} else {
		if obj.ctype == archive.DropContentType {
			err = json.Unmarshal(data, &list)
		} else {
			in := bytes.NewBuffer(data)
			dec := gob.NewDecoder(in)
			err = dec.Decode(&list)
		}
		if err != nil {
			err = fmt.Errorf("failed to parse %s: %v", obj, err)
		}
	}

	return list, err
}

// Import all of the identified drop archives and prepare to start emitting CSV
// lines.
func (ex *dropExporter) init(objs []object) error {
	all := make([]archive.DropArchive, 0)
	for _, o := range objs {
		slog.Infof("importing %v\n", o)
		archived, err := importOneDropArchive(o)
		if err != nil {
			return err
		}
		all = append(all, archived...)
	}

	ex.data = all
	ex.archiveIdx = -1

	return nil
}

/***************************************************************************
 *
 * Support for generating Open Port records from archives of archive.Snapshot
 */

type portExporter struct {
	data []*portRecord
	idx  int

	Exporter
}

type portRecord struct {
	time     time.Time
	mac      string
	ip       net.IP
	tcpPorts []int
	udpPorts []int
}

// Generate boolean columns for the tracked ports and a string for detected but
// untracked ports.
func addPorts(seen, track []int) string {
	var l string

	seenMap := make(map[int]bool)
	for _, p := range seen {
		seenMap[p] = true
	}

	// Add true/false entries for each of the ports we are explicitly
	// tracking
	for _, p := range track {
		if seenMap[p] {
			l += ",TRUE"
			delete(seenMap, p)
		} else {
			l += ",FALSE"
		}
	}
	l += ","

	// Add an ordered, space-delimited list of all ports seen, but which
	// aren't being explicitly tracked
	other := make([]int, 0)
	for p := range seenMap {
		other = append(other, p)
	}
	sort.Ints(other)
	delim := ""
	for _, p := range other {
		l += delim + strconv.Itoa(p)
		delim = " "
	}
	return l
}

func (ex *portExporter) advance() {
	ex.idx++
}

func (ex *portExporter) line() string {
	if ex.idx == -1 {
		return portHdr
	}
	if ex.idx >= len(ex.data) {
		return ""
	}

	rec := ex.data[ex.idx]
	l := rec.time.Format(timeFmt) + "," + *uuidFlag + "," + rec.mac + ","
	if rec.ip == nil {
		l += "unknown"
	} else {
		l += rec.ip.String()
	}
	l += addPorts(rec.tcpPorts, tcpTrack)
	l += addPorts(rec.udpPorts, udpTrack)
	l += "\n"
	return l
}

func (ex *portExporter) Read(p []byte) (n int, err error) {
	return read(ex, p)
}

// read one archive.Snapshot archive, and extract the open-port information from
// each entry.
func importPortData(obj object) ([]*portRecord, error) {
	var list []archive.Snapshot

	if *verbose {
		slog.Debugf("  fetching %s\n", obj)
	}
	data, err := readData(obj.name)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %v", obj, err)
	}

	if obj.ctype == archive.StatContentType {
		err = json.Unmarshal(data, &list)
	} else {
		in := bytes.NewBuffer(data)
		dec := gob.NewDecoder(in)
		err = dec.Decode(&list)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %v", obj, err)
	}

	records := make([]*portRecord, 0)
	for _, s := range list {
		for mac, device := range s.Data {
			rec := portRecord{
				time:     s.End,
				mac:      mac,
				ip:       device.Addr,
				tcpPorts: device.OpenTCP,
				udpPorts: device.OpenUDP,
			}
			records = append(records, &rec)
		}
	}

	return records, nil
}

func (ex *portExporter) done() bool {
	return ex == nil || ex.idx >= len(ex.data)
}

func (ex *portExporter) ctype() string {
	return "application/openports-csv"
}

// Build the CSV header based on the lists of ports we want to track explicitly.
// Each tracked port gets a boolean column, and there is a per-protocol string
// column used to list any other ports that were detected.
func buildPortHdr() {
	portHdr = "TIME,MAC,IP"

	for _, p := range tcpTrack {
		portHdr += fmt.Sprintf(",TCP %d", p)
	}
	portHdr += fmt.Sprintf(",TCP other")

	for _, p := range udpTrack {
		portHdr += fmt.Sprintf(",UDP %d", p)
	}
	portHdr += fmt.Sprintf(",UDP other\n")
}

func (ex *portExporter) init(objs []object) error {
	buildPortHdr()

	// Iterate over all the snapshot objects, building an interim
	// representation of the open ports information
	all := make([]*portRecord, 0)
	for _, o := range objs {
		slog.Infof("importing %v", o)
		tmp, err := importPortData(o)
		if err != nil {
			return err
		}
		all = append(all, tmp...)
	}

	ex.data = all
	ex.idx = -1
	return nil
}

/**************************************************************************
 *
 * Support for generating per-session statisticsfrom archives of
 * archive.Snapshot
 */

type statExporter struct {
	data []*statRecord
	idx  int

	Exporter
}

type statRecord struct {
	start time.Time
	end   time.Time
	mac   string

	localAddr  net.IP
	localPort  int
	remoteAddr net.IP
	remotePort int

	pktsSent  uint64
	pktsRcvd  uint64
	bytesSent uint64
	bytesRcvd uint64
}

func endpoint(addr net.IP, port int) string {
	var l string

	if addr == nil {
		l = "unknown"
	} else {
		l = addr.String()
	}
	l += "," + strconv.Itoa(port)
	return l
}

func (ex *statExporter) advance() {
	ex.idx++
}

func (ex *statExporter) line() string {
	if ex.idx == -1 {
		return statHdr
	}
	if ex.idx >= len(ex.data) {
		return ""
	}

	rec := ex.data[ex.idx]
	l := rec.start.Format(timeFmt)
	l += "," + rec.end.Format(timeFmt)
	l += "," + *uuidFlag
	l += "," + rec.mac
	l += "," + endpoint(rec.localAddr, rec.localPort)
	l += "," + endpoint(rec.remoteAddr, rec.remotePort)
	l += "," + strconv.FormatUint(rec.pktsSent, 10)
	l += "," + strconv.FormatUint(rec.pktsRcvd, 10)
	l += "," + strconv.FormatUint(rec.bytesSent, 10)
	l += "," + strconv.FormatUint(rec.bytesRcvd, 10)
	l += "\n"

	return l
}

func (ex *statExporter) Read(p []byte) (n int, err error) {
	return read(ex, p)
}

func newStatRecord(s archive.Snapshot, mac string, local net.IP,
	key uint64, stats archive.XferStats) *statRecord {

	session := archive.KeyToSession(key)

	rec := statRecord{
		start:      s.Start,
		end:        s.End,
		mac:        mac,
		localAddr:  local,
		localPort:  session.LPort,
		remoteAddr: session.RAddr,
		remotePort: session.RPort,
		pktsSent:   stats.PktsSent,
		pktsRcvd:   stats.PktsRcvd,
		bytesSent:  stats.BytesSent,
		bytesRcvd:  stats.BytesRcvd,
	}
	return &rec
}

func importStatData(obj object) ([]*statRecord, error) {
	var list []archive.Snapshot

	slog.Infof("  fetching %s", obj)
	data, err := readData(obj.name)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %v", obj, err)
	}

	if obj.ctype == archive.StatContentType {
		err = json.Unmarshal(data, &list)
	} else {
		in := bytes.NewBuffer(data)
		dec := gob.NewDecoder(in)
		err = dec.Decode(&list)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %v", obj, err)
	}

	records := make([]*statRecord, 0)
	for _, s := range list {
		for mac, device := range s.Data {
			laddr := device.Addr
			for key, stats := range device.LANStats {
				rec := newStatRecord(s, mac, laddr, key, stats)
				records = append(records, rec)
			}
			for key, stats := range device.WANStats {
				rec := newStatRecord(s, mac, laddr, key, stats)
				records = append(records, rec)
			}
		}
	}

	return records, nil
}

func (ex *statExporter) done() bool {
	return ex == nil || ex.idx >= len(ex.data)
}

func (ex *statExporter) ctype() string {
	return "application/xferstats-csv"
}

func (ex *statExporter) init(objs []object) error {
	// Iterate over all the snapshot objects, building an interim
	// representation of the stats information
	all := make([]*statRecord, 0)
	for _, o := range objs {
		slog.Infof("importing %v", o)
		tmp, err := importStatData(o)
		if err != nil {
			return err
		}
		all = append(all, tmp...)
	}

	ex.data = all
	ex.idx = -1
	return nil
}

func exportUsage() {
	e, _ := os.Executable()
	fmt.Printf("usage: %s [flags] export <dataset>\n", e)
	flag.PrintDefaults()
	os.Exit(1)
}

func export(args []string) error {
	var exporter Exporter

	if len(args) != 1 {
		exportUsage()
	}
	if *uuidFlag == "" {
		return fmt.Errorf("need to specify uuid for export")
	}
	dataset := args[0]

	_, objs, err := getObjects()
	if err != nil {
		return fmt.Errorf("failed to get object list: %v", err)
	}
	if len(objs) == 0 {
		return nil
	}

	ctype := objs[0].ctype
	switch ctype {
	case archive.DropContentType, archive.DropBinaryType:
		switch dataset {
		case "drops":
			exporter = &dropExporter{}
		}
	case archive.StatContentType, archive.StatBinaryType:
		switch dataset {
		case "stats":
			exporter = &statExporter{}
		case "ports":
			exporter = &portExporter{}
		}
	default:
		return fmt.Errorf("unrecognized datatype: %s", ctype)
	}

	if exporter == nil {
		return fmt.Errorf("'%s' cannot be extracted from %s",
			dataset, ctype)
	}

	if err = exporter.init(objs); err == nil {
		err = writeData(exporter.ctype(), exporter)
	}
	return err
}

