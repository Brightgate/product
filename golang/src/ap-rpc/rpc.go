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
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"ap_common/apcfg"
	// "base_def"
	"cloud_rpc"

	"github.com/tomazk/envcfg"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
	// "github.com/golang/protobuf/proto"
)

type Cfg struct {
	// Override configuration value.
	B10E_LOCAL_MODE bool
	B10E_SVC_URL    string
}

const pname = "ap-rpc"

var (
	config *apcfg.APConfig
	// ApVersion will be replaced by go build step.
	ApVersion = "undefined"
)

// Return the MAC address for the defined WAN interface.
func get_wan_interface(config *apcfg.APConfig) string {
	wan_nic, err := config.GetProp("@/network/wan_nic")
	if err != nil {
		fmt.Printf("property get @/network/wan_nic failed: %v\n", err)
		os.Exit(1)
	}

	iface, err := net.InterfaceByName(wan_nic)
	if err != nil {
		log.Printf("could not retrieve %s interface: %v\n",
			wan_nic, err)
		os.Exit(1)
	}

	return iface.HardwareAddr.String()
}

func first_version() string {
	return "git:rPS" + ApVersion
}

// Retrieve the instance uptime. os.Stat("/proc/1") returns
// start-of-epoch for the creation time on Raspbian, so we will instead
// use the contents of /proc/uptime.  uptime records in seconds, so we
// multiply by 10^9 to create a time.Duration.
func retrieveUptime() time.Duration {
	uptime, err := os.Open("/proc/uptime")
	if err != nil {
		log.Printf("could not open /proc/uptime: %v\n", err)
		os.Exit(1)
	}

	scanner := bufio.NewScanner(uptime)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), " ")
		val, err := strconv.ParseFloat(fields[0], 10)
		if err != nil {
			log.Printf("/proc/uptime contents unusual: %v\n", err)
			os.Exit(1)
		}
		return time.Duration(val * 1e9)
	}
	if err = scanner.Err(); err != nil {
		log.Printf("/proc/uptime scan failed: %v\n", err)
		os.Exit(1)
	}

	log.Printf("/proc/uptime possibly empty\n")
	os.Exit(1)

	// Not reached.
	return time.Duration(0)
}

func main() {
	var environ Cfg
	var serverAddr string
	var elapsed time.Duration

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	envcfg.Unmarshal(&environ)

	config, err := apcfg.NewConfig(pname)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}

	if len(environ.B10E_SVC_URL) == 0 {
		// XXX ap.configd lookup.
		serverAddr = "svc0.b10e.net:4430"
	} else {
		serverAddr = environ.B10E_SVC_URL
	}

	// Retrieve appliance UUID
	uuid, err := config.GetProp("@/uuid")
	if err != nil {
		fmt.Printf("property get failed: %v\n", err)
		os.Exit(1)
	}

	// Retrieve appliance MAC.
	hwaddr := make([]string, 0)
	hwaddr = append(hwaddr, get_wan_interface(config))

	// Retrieve appliance uptime.
	elapsed = retrieveUptime()

	// Retrieve component versions.
	versions := make([]string, 0)
	versions = append(versions, first_version())

	year := time.Now().Year()
	rhmac := hmac.New(sha256.New, cloud_rpc.HMACKeys[year])
	data := fmt.Sprintf("%v %v", hwaddr, int64(elapsed))
	rhmac.Write([]byte(data))

	// Build UpcallRequest
	request := &cloud_rpc.UpcallRequest{
		HMAC:             rhmac.Sum(nil),
		Uuid:             uuid,
		UptimeElapsed:    int64(elapsed),
		WanHwaddr:        hwaddr,
		ComponentVersion: versions,
		NetHostCount:     0,
	}

	var opts []grpc.DialOption

	if !environ.B10E_LOCAL_MODE {
		cp, nocperr := x509.SystemCertPool()
		if nocperr != nil {
			log.Printf("no system certificate pool: %v\n", nocperr)
			os.Exit(1)
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
		log.Printf("grpc Dial() to '%s' failed: %v\n", serverAddr, err)
		os.Exit(1)
	}
	defer conn.Close()

	client := cloud_rpc.NewUpbeatClient(conn)

	response, err := client.Upcall(context.Background(), request)
	if err != nil {
		grpclog.Fatalf("%v.Upcall(_) = _, %v: ", client, err)
	}

	log.Println(response)
	grpclog.Println(response)
}
