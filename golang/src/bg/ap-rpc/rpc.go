/*
 * COPYRIGHT 2017 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"hash"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"
	"bg/cloud_rpc"
	"bg/common"

	"github.com/golang/protobuf/proto"
	"github.com/tomazk/envcfg"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
)

type cfg struct {
	// Override configuration value.
	LocalMode bool   `envcfg:"LOCAL_MODE"`
	SrvURL    string `envcfg:"SRV_URL"`
}

const pname = "ap-rpc"

// gRPC has a default maximum message size of 4MiB
const msgsize = 2097152

var services = map[string]func(context.Context, *grpc.ClientConn){
	"upbeat":    sendUpbeat,
	"inventory": sendInventory,
}

var (
	aproot = flag.String("root", "proto.armv7l/appliance/opt/com.brightgate",
		"Root of AP installation")

	environ    cfg
	serverAddr string
	config     *apcfg.APConfig

	apuuid   string
	aphwaddr []string
)

func gethmac(data string) hash.Hash {
	year := time.Now().Year()
	rhmac := hmac.New(sha256.New, cloud_rpc.HMACKeys[year])
	rhmac.Write([]byte(data))
	return rhmac
}

// Return the MAC address for the defined WAN interface.
func getWanInterface(config *apcfg.APConfig) string {
	nics, err := config.GetNics(base_def.RING_WAN, true)
	if err != nil {
		log.Fatalf("failed to get list of WAN NICs: %v\n", err)
	}
	if len(nics) == 0 {
		log.Fatalf("No WAN interface identified\n")
	}

	return nics[0]
}

func firstVersion() string {
	return "git:rPS" + common.GitVersion
}

// Retrieve the instance uptime. os.Stat("/proc/1") returns
// start-of-epoch for the creation time on Raspbian, so we will instead
// use the contents of /proc/uptime.  uptime records in seconds, so we
// multiply by 10^9 to create a time.Duration.
func retrieveUptime() time.Duration {
	uptime, err := os.Open("/proc/uptime")
	if err != nil {
		log.Fatalf("could not open /proc/uptime: %v\n", err)
	}

	scanner := bufio.NewScanner(uptime)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), " ")
		val, err := strconv.ParseFloat(fields[0], 10)
		if err != nil {
			log.Fatalf("/proc/uptime contents unusual: %v\n", err)
		}
		return time.Duration(val * 1e9)
	}
	if err = scanner.Err(); err != nil {
		log.Fatalf("/proc/uptime scan failed: %v\n", err)
	}

	log.Fatalf("/proc/uptime possibly empty\n")

	// Not reached.
	return time.Duration(0)
}

func dial() (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	if !environ.LocalMode {
		cp, nocperr := x509.SystemCertPool()
		if nocperr != nil {
			return nil, fmt.Errorf("no system certificate pool: %v", nocperr)
		}

		tc := tls.Config{
			RootCAs: cp,
		}

		ctls := credentials.NewTLS(&tc)
		opts = append(opts, grpc.WithTransportCredentials(ctls))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}

	opts = append(opts, grpc.WithUserAgent(pname))

	conn, err := grpc.Dial(serverAddr, opts...)
	if err != nil {
		return nil, fmt.Errorf("grpc Dial() to '%s' failed: %v", serverAddr, err)
	}
	return conn, nil
}

func sendUpbeat(ctx context.Context, conn *grpc.ClientConn) {
	var elapsed time.Duration

	// Retrieve appliance uptime.
	elapsed = retrieveUptime()

	// Retrieve component versions.
	versions := make([]string, 0)
	versions = append(versions, firstVersion())

	// Build UpcallRequest
	rhmac := gethmac(fmt.Sprintf("%x %d", aphwaddr, int64(elapsed)))
	request := &cloud_rpc.UpcallRequest{
		HMAC:             rhmac.Sum(nil),
		Uuid:             proto.String(apuuid),
		UptimeElapsed:    proto.Int64(int64(elapsed)),
		WanHwaddr:        aphwaddr,
		ComponentVersion: versions,
		NetHostCount:     proto.Int32(0), // XXX not finished
	}

	client := cloud_rpc.NewUpbeatClient(conn)

	response, err := client.Upcall(context.Background(), request)
	if err != nil {
		grpclog.Fatalf("%v.Upcall(_) = _, %v: ", client, err)
	}

	log.Println(response)
	grpclog.Println(response)
}

func sendChanged(client cloud_rpc.InventoryClient, changed *base_msg.DeviceInventory) {
	// Build InventoryReport
	rhmac := gethmac(changed.String())
	report := &cloud_rpc.InventoryReport{
		HMAC:      rhmac.Sum(nil),
		Uuid:      proto.String(apuuid),
		WanHwaddr: aphwaddr,
		Inventory: changed,
	}

	response, err := client.Upcall(context.Background(), report,
		grpc.UseCompressor("gzip"))
	if err != nil {
		grpclog.Fatalf("%v.Upcall(_) = _, %v: ", client, err)
	}

	log.Println(response)
	grpclog.Println(response)
}

func sendInventory(ctx context.Context, conn *grpc.ClientConn) {
	invPath := filepath.Join(*aproot, "/var/spool/identifierd/")
	manPath := filepath.Join(*aproot, "/var/spool/rpc/")
	manFile := filepath.Join(manPath, "identifierd.json")

	// Read device inventories from disk
	files, err := ioutil.ReadDir(invPath)
	if err != nil {
		log.Printf("could not read dir %s: %s\n", invPath, err)
		return
	}

	// Read manifest from disk
	manifest := make(map[string]time.Time)
	m, err := ioutil.ReadFile(manFile)
	if err != nil {
		log.Printf("failed to read manifest %s: %s\n", manFile, err)
	} else {
		err = json.Unmarshal(m, &manifest)
		if err != nil {
			log.Printf("failed to import manifest %s: %s\n", manFile, err)
		}
	}

	client := cloud_rpc.NewInventoryClient(conn)

	changed := &base_msg.DeviceInventory{
		Timestamp: aputil.NowToProtobuf(),
	}

	now := time.Now()
	for _, file := range files {
		path := filepath.Join(invPath, file.Name())
		in, err := ioutil.ReadFile(path)
		if err != nil {
			log.Printf("failed to read device inventory %s: %s\n", path, err)
			continue
		}
		inventory := &base_msg.DeviceInventory{}
		proto.Unmarshal(in, inventory)

		for _, devInfo := range inventory.Devices {
			mac := devInfo.GetMacAddress()
			if mac == 0 || devInfo.Updated == nil {
				continue
			}
			hwaddr := network.Uint64ToHWAddr(mac)
			updated := aputil.ProtobufToTime(devInfo.Updated)
			sent := manifest[hwaddr.String()]
			if updated.After(sent) {
				changed.Devices = append(changed.Devices, devInfo)
				manifest[hwaddr.String()] = now
			}

			if proto.Size(changed) >= msgsize {
				sendChanged(client, changed)
				changed = &base_msg.DeviceInventory{
					Timestamp: aputil.NowToProtobuf(),
				}
			}
		}
	}

	if len(changed.Devices) != 0 {
		sendChanged(client, changed)
	}

	// Write manifest
	s, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		log.Printf("failed to construct JSON: %s\n", err)
		return
	}

	if err := os.MkdirAll(manPath, 0755); err != nil {
		log.Printf("failed to mkdir %s: %s\n", manPath, err)
		return
	}

	tmpPath := manFile + ".tmp"
	err = ioutil.WriteFile(tmpPath, s, 0644)
	if err != nil {
		log.Printf("failed to write file %s: %s\n", tmpPath, err)
		return
	}

	os.Rename(tmpPath, manFile)
}

func main() {
	var err error
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()

	if len(flag.Args()) == 0 {
		log.Fatalf("Service name required.\n")
	}
	svc := flag.Args()[0]
	svcFunc, ok := services[svc]
	if !ok {
		log.Fatalf("Unknown service %s\n", svc)
	}

	envcfg.Unmarshal(&environ)
	log.Printf("environ: %+v", environ)

	config, err := apcfg.NewConfig(nil, pname)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}

	// Retrieve appliance UUID
	apuuid, err = config.GetProp("@/uuid")
	if err != nil {
		log.Fatalf("property get failed: %v\n", err)
	}

	// Retrieve appliance MAC.
	aphwaddr = make([]string, 0)
	aphwaddr = append(aphwaddr, getWanInterface(config))

	if len(environ.SrvURL) == 0 {
		// XXX ap.configd lookup.
		serverAddr = "svc0.b10e.net:4430"
	} else {
		serverAddr = environ.SrvURL
	}

	ctx := context.Background()

	conn, err := dial()
	if err != nil {
		grpclog.Fatalf("dial() failed: %v", err)
	}
	defer conn.Close()
	svcFunc(ctx, conn)
}
