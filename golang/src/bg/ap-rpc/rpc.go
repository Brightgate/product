/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/cloud/rpcclient"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"
	"bg/cloud_rpc"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	any "github.com/golang/protobuf/ptypes/any"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/grpclog"
)

const pname = "ap-rpc"

// gRPC has a default maximum message size of 4MiB
const msgsize = 2097152

var services = map[string]func(context.Context, *grpc.ClientConn){
	"heartbeat":      sendHeartbeat,
	"heartbeat-loop": sendHeartbeatLoop,
	"inventory":      sendInventory,
}

var (
	forceInventory = flag.Bool("force-inventory", false, "Force; always send inventory")
	connectFlag    = flag.String("connect", base_def.CL_SVC_RPC, "Override connection endpoint in credential")
	deadlineFlag   = flag.Duration("rpc-deadline", time.Second*20, "RPC completion deadline")
	enableTLSFlag  = flag.Bool("enable-tls", true, "Enable Secure gRPC")
)

func publishEvent(ctx context.Context, tclient cloud_rpc.EventClient, subtopic string, evt proto.Message) error {
	name := proto.MessageName(evt)
	serialized, err := proto.Marshal(evt)
	if err != nil {
		return err
	}

	clientDeadline := time.Now().Add(*deadlineFlag)
	ctx, ctxcancel := context.WithDeadline(ctx, clientDeadline)
	defer ctxcancel()

	eventRequest := &cloud_rpc.PutEventRequest{
		SubTopic: subtopic,
		Payload: &any.Any{
			TypeUrl: base_def.API_PROTOBUF_URL + "/" + name,
			Value:   serialized,
		},
	}

	response, err := tclient.Put(ctx, eventRequest)
	if err != nil {
		return fmt.Errorf("Failed to Put() Event: %v", err)
	}
	grpclog.Infoln(response)
	return nil
}

func sendHeartbeat(ctx context.Context, conn *grpc.ClientConn) {
	bootTime, err := ptypes.TimestampProto(aputil.LinuxBootTime())
	if err != nil {
		grpclog.Fatalf("failed to make Heartbeat: %v", err)
	}
	heartbeat := &cloud_rpc.Heartbeat{
		BootTime:   bootTime,
		RecordTime: ptypes.TimestampNow(),
	}

	client := cloud_rpc.NewEventClient(conn)
	err = publishEvent(ctx, client, "heartbeat", heartbeat)
	if err != nil {
		grpclog.Errorf("sendHeartbeat failed: %s", err)
	} else {
		log.Printf("heartbeat: %+v", heartbeat)
	}
}

func sendHeartbeatLoop(ctx context.Context, conn *grpc.ClientConn) {
	for {
		sendHeartbeat(ctx, conn)
		time.Sleep(10 * time.Second)
	}
}

func sendChanged(ctx context.Context, client cloud_rpc.EventClient, changed *base_msg.DeviceInventory) {
	var err error
	// Build InventoryReport
	report := &cloud_rpc.InventoryReport{
		Inventory: changed,
	}

	err = publishEvent(ctx, client, "inventory", report)
	if err != nil {
		grpclog.Errorf("sendHeartbeat failed: %s", err)
	}
}

func sendInventory(ctx context.Context, conn *grpc.ClientConn) {
	invPath := aputil.ExpandDirPath("/var/spool/identifierd/")
	manPath := aputil.ExpandDirPath("/var/spool/rpc/")
	manFile := filepath.Join(manPath, "identifierd.json")

	// Read device inventories from disk
	files, err := ioutil.ReadDir(invPath)
	if err != nil {
		log.Printf("could not read dir %s: %s\n", invPath, err)
		return
	}
	if len(files) == 0 {
		log.Printf("no files found in %s", invPath)
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

	client := cloud_rpc.NewEventClient(conn)

	changed := &base_msg.DeviceInventory{
		Timestamp: aputil.NowToProtobuf(),
	}

	now := time.Now()
	for _, file := range files {
		var in []byte

		path := filepath.Join(invPath, file.Name())
		if in, err = ioutil.ReadFile(path); err != nil {
			log.Printf("failed to read device inventory %s: %s\n", path, err)
			continue
		}
		inventory := &base_msg.DeviceInventory{}
		err = proto.Unmarshal(in, inventory)
		if err != nil {
			log.Printf("failed to unmarshal device inventory %s: %s\n", path, err)
			continue
		}

		for _, devInfo := range inventory.Devices {
			mac := devInfo.GetMacAddress()
			if mac == 0 || devInfo.Updated == nil {
				continue
			}
			hwaddr := network.Uint64ToHWAddr(mac)
			updated := aputil.ProtobufToTime(devInfo.Updated)
			sent := manifest[hwaddr.String()]
			if *forceInventory || updated.After(sent) {
				log.Printf("Reporting %s > %s", file.Name(), hwaddr)
				changed.Devices = append(changed.Devices, devInfo)
				manifest[hwaddr.String()] = now
			} else {
				log.Printf("Skipping %s > %s", file.Name(), hwaddr)
			}

			if proto.Size(changed) >= msgsize {
				sendChanged(ctx, client, changed)
				changed = &base_msg.DeviceInventory{
					Timestamp: aputil.NowToProtobuf(),
				}
			}
		}
	}

	if len(changed.Devices) != 0 {
		sendChanged(ctx, client, changed)
	}

	// Write manifest
	s, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		log.Printf("failed to construct JSON: %s\n", err)
		return
	}

	if err = os.MkdirAll(manPath, 0755); err != nil {
		log.Printf("failed to mkdir %s: %s\n", manPath, err)
		return
	}

	tmpPath := manFile + ".tmp"
	err = ioutil.WriteFile(tmpPath, s, 0644)
	if err != nil {
		log.Printf("failed to write file %s: %s\n", tmpPath, err)
		return
	}

	err = os.Rename(tmpPath, manFile)
	if err != nil {
		log.Printf("failed to rename %s -> %s: %s\n", tmpPath, manFile, err)
		return
	}
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

	applianceCred, err := rpcclient.SystemCredential()
	if err != nil {
		log.Fatalf("Failed to build get credential: %s", err)
	}
	ctx, err := applianceCred.MakeGRPCContext(context.Background())
	if err != nil {
		log.Fatalf("Failed to build GRPC context: %s", err)
	}

	conn, err := rpcclient.NewRPCClient(*connectFlag, *enableTLSFlag, pname)
	if err != nil {
		grpclog.Fatalf("Failed to make RPC client: %+v", err)
	}
	defer conn.Close()

	svcFunc(ctx, conn)
}
