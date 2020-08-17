/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


/*
 * cl-dtool is a tool for managing and manipulating time-series data collected
 * from appliances and stored in the cloud.  In general, an invocation of the
 * tool will include start and end times, source and target destinations, and a
 * management command.
 *
 * A standard workflow would be:
 *
 *   1. Appliance gathers data.  5-minute snapshots uploaded to cloud storage at
 *      bg-appliance-data-<uuid>/stats/<timestamp>.json
 *
 *   2. 'cl-dtool merge' to combine multiple 5-minute snapshots in
 *      bg-appliance-data to a single full-day json archive in
 *      bg-appliance-merge.  This archive will be preserved indefinitely, while
 *      the 5-minute snapshots can be removed.  If desired, the full-day
 *      archives can be subsequently re-merged into full-month archives, etc.
 *
 *   3. 'cl-dtool export stats' to extract a single type of data into a CSV file
 *
 *   4. 'bq load' to insert the CSV data into a Google BigQuery table
 *
 * Eventually this tool will do the BigQuery insert as well.  BigQuery is
 * optimized for bulk loading of data from GCP storage, which is why we have an
 * interim CSV stage rather than inserting each record into the table
 * individually.
 *
 */

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"time"

	"bg/cl_common/daemonutils"
	"bg/common/archive"

	"cloud.google.com/go/storage"
	"go.uber.org/zap"
	"google.golang.org/api/iterator"
)

const (
	googleStorage = "https://storage.googleapis.com/"

	gcpEnv = "GOOGLE_APPLICATION_CREDENTIALS"
)

var (
	helpFlag  = flag.Bool("h", false, "help")
	credFile  = flag.String("creds", "", "GCP credentials file")
	startFlag = flag.String("start", "", "start time")
	endFlag   = flag.String("end", "", "end time")
	srcFlag   = flag.String("src", "", "data source")
	dstFlag   = flag.String("dst", "", "data destination")
	binFlag   = flag.Bool("b", false, "output binary data")
	verbose   = flag.Bool("v", false, "verbose output")
	uuidFlag  = flag.String("u", "", "uuid for export")
	ctypeFlag = flag.String("ctype", "", "impose a content-type")

	gcpCtx    context.Context
	gcpClient *storage.Client

	startTime time.Time
	endTime   time.Time

	log  *zap.Logger
	slog *zap.SugaredLogger

	objNameRE = regexp.MustCompile(`(.*)/(.*)\.(.*)`)
)

type object struct {
	name  string
	ctype string
}

var typeFamily = map[string]string{
	archive.DropContentType: "drop",
	archive.DropBinaryType:  "drop",
	archive.StatContentType: "stat",
	archive.StatBinaryType:  "stat",
}

func compatible(a, b string) bool {
	typeA := typeFamily[a]
	typeB := typeFamily[b]

	return typeA == typeB
}

// Given a string that should contain a timestamp, we will try to parse it using
// a number of different formats.  This routine is used for parsing both
// command-line options and file/object names.
func extractTime(text string) (time.Time, error) {
	var rval time.Time
	var err error

	formats := []string{
		time.RFC3339,
		"200601021504",
		"2006010215",
		"20060102",
		"01-02-15:04-2006",
		"Jan 2 15:04 2006",
		"Jan 2 15:04",
	}

	for _, f := range formats {
		if rval, err = time.Parse(f, text); err == nil {
			if rval.Year() == 0 {
				rval.AddDate(time.Now().Year(), 0, 0)
			}
			return rval, nil
		}
	}

	return rval, fmt.Errorf("invalid time format: %s", text)
}

// Attempts to parse and classify a full object name.
func parseName(full string) (string, string, error) {
	var bucket, name string
	var err error

	if len(full) == 0 {
		// No name defaults to stdin/stdout

	} else if strings.HasPrefix(full, "gs://") {
		// gs:// refers to a google storage bucket
		full := full[5:]
		delim := strings.Index(full, "/")
		if delim == -1 || delim == len(full)-1 {
			err = fmt.Errorf("invalid google storage name")
		} else {
			bucket = full[:delim]
			name = full[delim+1:]
		}

	} else {
		// Anything else we assume is a local file
		name = full
	}
	return bucket, name, err
}

// When working with local storage, we don't have the content-type metadata, so
// we try to make a reasonable guess as to the data we're working with.
func inferDatatype(path string) (string, error) {
	if *ctypeFlag != "" {
		return *ctypeFlag, nil
	}
	if strings.Contains(path, "/drops") {
		return "drops", nil
	}
	if strings.Contains(path, "/stats") {
		return "stats", nil
	}

	return "", fmt.Errorf("can't identify data type")
}

// Is this object's timestamp within the desired range?
func objectWithinBounds(name string) bool {
	// Objects names should be <source>/<timestamp>.<extension>
	tok := objNameRE.FindStringSubmatch(name)
	if len(tok) != 4 {
		return false
	}

	t, err := extractTime(tok[2])
	if err != nil {
		return false
	}

	return t.Equal(startTime) || (t.After(startTime) && t.Before(endTime))
}

// Fetch the names of all objects in the provided bucket that have the desired
// prefix and are within the desired time range.
func getObjectsBucket(bucket, name string) ([]object, error) {
	var err error

	slog.Debugf("Fetching objects between %s and %s from %s",
		startTime.Format(time.Stamp), endTime.Format(time.Stamp),
		bucket)

	objs := make([]object, 0)
	filter := storage.Query{Prefix: name}
	it := gcpClient.Bucket(bucket).Objects(gcpCtx, &filter)
	for {
		var obj object

		attrs, ierr := it.Next()
		if ierr == iterator.Done {
			break
		} else if ierr != nil {
			err = fmt.Errorf("iterator failed: %v", ierr)
			break
		}

		if !objectWithinBounds(attrs.Name) {
			continue
		}

		obj.name = attrs.Name
		if attrs.ContentType != "" {
			obj.ctype = attrs.ContentType
		} else if *ctypeFlag != "" {
			obj.ctype = *ctypeFlag
		} else {
			err = fmt.Errorf("missing content type for %s",
				attrs.Name)
			break
		}
		objs = append(objs, obj)
	}

	return objs, err
}

// Fetch the names of all files in the provided directory that have the desired
// prefix and are within the desired time range.
func getObjectsLocal(dir string) ([]object, error) {
	ctype, err := inferDatatype(dir)
	if err != nil {
		return nil, err
	}

	objs := make([]object, 0)
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		err = fmt.Errorf("unable to get contents of %s: %v",
			dir, err)
	} else {
		for _, f := range files {
			if objectWithinBounds(f.Name()) {
				obj := object{
					name:  f.Name(),
					ctype: ctype,
				}

				objs = append(objs, obj)
			}
		}
	}
	return objs, err
}

func getObjects() (string, []object, error) {
	var fullname string
	var objs []object
	var err error

	bucket, name, _ := parseName(*srcFlag)
	if bucket != "" {
		objs, err = getObjectsBucket(bucket, name)
		fullname = bucket + "/" + name
	} else if name != "" {
		objs, err = getObjectsLocal(name)
		fullname = name
	} else {
		err = fmt.Errorf("must provide a data source")
	}

	if len(objs) == 0 || err != nil {
		return fullname, nil, err
	}

	// Verify that all of the objects found are of compatible types
	ctype := objs[0].ctype
	for _, obj := range objs {
		if !compatible(ctype, obj.ctype) {
			err = fmt.Errorf("found multiple content "+
				"types: %s and %s",
				ctype, obj.ctype)
			break
		}
	}
	return fullname, objs, err
}

func readData(obj string) ([]byte, error) {
	var src io.ReadCloser
	var err error

	bucket, name, _ := parseName(*srcFlag)
	if bucket != "" {
		slog.Debugf("Reading from google storage %s", *srcFlag)

		hdl := gcpClient.Bucket(bucket).Object(obj)
		src, err = hdl.NewReader(gcpCtx)
	} else if name != "" {
		slog.Debugf("Reading from file %s", obj)
		src, err = os.Open(name)
	} else {
		err = fmt.Errorf("must provide a data source")
	}

	if err != nil {
		return nil, fmt.Errorf("open failure: %v", err)
	}

	data, err := ioutil.ReadAll(src)
	src.Close()
	if err != nil {
		data = nil
		err = fmt.Errorf("read failure: %v", err)
	}
	return data, err
}

func writeToBucket(bucket, name, ctype string, src io.Reader) error {
	slog.Debugf("Writing to google storage %s", *dstFlag)

	if name == "" {
		return fmt.Errorf("must specify a target object name")
	}

	object := gcpClient.Bucket(bucket).Object(name)
	wc := object.NewWriter(gcpCtx)
	_, err := io.Copy(wc, src)
	if cerr := wc.Close(); cerr != nil && err == nil {
		err = cerr
	}

	if err == nil {
		u := storage.ObjectAttrsToUpdate{
			ContentType: ctype,
		}
		if _, uerr := object.Update(gcpCtx, u); uerr != nil {
			slog.Warnf("unable to update content type: %v", uerr)
		}
	} else {
		err = fmt.Errorf("uploading merged data to gcp: %v", err)
	}
	return err
}

func writeToFile(name string, src io.Reader) error {
	slog.Debugf("Writing to file %s", name)
	out, err := os.Create(name)
	if err != nil {
		err = fmt.Errorf("creating file %s: %v", name, err)
	} else {
		if _, err = io.Copy(out, src); err != nil {
			err = fmt.Errorf("writing file %s: %v", name, err)
		}
		out.Close()
	}
	return err
}

func writeData(ctype string, src io.Reader) error {
	var err error

	bucket, name, _ := parseName(*dstFlag)
	if bucket != "" {
		err = writeToBucket(bucket, name, ctype, src)
	} else if name != "" {
		err = writeToFile(name, src)
	} else {
		data, _ := ioutil.ReadAll(src)
		fmt.Printf("%v\n", string(data))
	}
	return err
}

// list the objects in the source bucket
func list() error {
	_, objs, err := getObjects()
	if err != nil {
		return fmt.Errorf("failed to get object list: %v", err)
	}

	for _, n := range objs {
		fmt.Printf("%s\n", n.name)
	}

	return nil
}

// Use the -start and -end flags to set upper and lower time bounds.  If -start
// is missing, we default to the dawn of time.  If -end is missing, we default
// to now.
func setTimeBounds() {
	var err error

	if *startFlag == "" {
		startTime = time.Unix(0, 0)
	} else if startTime, err = extractTime(*startFlag); err != nil {
		fail(fmt.Errorf("bad start time: %v", err))
	}

	if *endFlag == "" {
		endTime = time.Now()
	} else if endTime, err = extractTime(*endFlag); err != nil {
		fail(fmt.Errorf("bad end time: %v", err))
	}

	if endTime.Before(startTime) {
		fail(fmt.Errorf("illegal time range %v to %v", startTime,
			endTime))
	}
}

// Examine the source and destination names.  If either is in GCP storage,
// set ourselves up as a GCP client.
func gcpInit() {
	sb, _, err := parseName(*srcFlag)
	if err != nil {
		fail(fmt.Errorf("invalid data source: %v", err))
	}
	db, _, err := parseName(*dstFlag)
	if err != nil {
		fail(fmt.Errorf("invalid data destination: %v", err))
	}

	if sb != "" || db != "" {
		gcpCtx = context.Background()
		gcpClient, err = storage.NewClient(gcpCtx)
		if err != nil {
			fail(fmt.Errorf("failed to connect to cloud storage: %v", err))
		}
	}
}

func fail(err error) {
	slog.Errorf("failed: %v", err)
	os.Exit(1)
}

func usage(err bool) {
	fmt.Printf("usage: %s <flags> <list | merge | export>\n", os.Args[0])
	flag.PrintDefaults()
	if err {
		os.Exit(1)
	}
	os.Exit(0)
}

func main() {
	var err error
	var cmdArgs []string

	log, slog = daemonutils.SetupLogs()
	flag.Parse()
	log, slog = daemonutils.ResetupLogs()
	defer log.Sync()
	if *helpFlag {
		usage(false)
	}

	tmp := flag.Args()
	if len(tmp) == 0 {
		usage(true)
	}

	cmd := tmp[0]
	if len(tmp) > 1 {
		cmdArgs = tmp[1:]
	}

	if *credFile != "" {
		os.Setenv(gcpEnv, *credFile)
	}
	if f := os.Getenv(gcpEnv); f == "" {
		fail(fmt.Errorf("Must provide GCP credentials through -creds "+
			"option or %s envvar\n", gcpEnv))
	}

	setTimeBounds()
	gcpInit()

	switch cmd {
	case "list":
		err = list()
	case "merge":
		err = merge()
	case "export":
		err = export(cmdArgs)
	default:
		usage(true)
	}
	if err != nil {
		fmt.Printf("%s failed: %v\n", cmd, err)
		os.Exit(1)
	}
}

