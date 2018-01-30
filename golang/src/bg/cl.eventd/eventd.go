/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

/*
 * cloud pub/sub message server
 *
 * Follows 12 factor app design.
 */

package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"bg/cl_common/daemonutils"
	"bg/cloud_models/appliancedb"
	"bg/cloud_models/cloudiotsvc"
	"bg/cloud_rpc"

	"github.com/tomazk/envcfg"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.uber.org/zap"

	"cloud.google.com/go/pubsub"
	"github.com/golang/protobuf/proto"
	"github.com/satori/uuid"
	"golang.org/x/net/context"
)

const checkMark = `✔︎ `

// Cfg contains the environment variable-based configuration settings
type Cfg struct {
	PrometheusPort     string `envcfg:"B10E_CLEVENTD_PROMETHEUS_PORT"`
	PostgresConnection string `envcfg:"B10E_CLEVENTD_POSTGRES_CONNECTION"`
	IoTProject         string `envcfg:"B10E_CLEVENTD_IOT_PROJECT"`
	IoTRegion          string `envcfg:"B10E_CLEVENTD_IOT_LOCATION"`
	IoTRegistry        string `envcfg:"B10E_CLEVENTD_IOT_REGISTRY"`
}

const (
	pname = "cl.eventd"
)

var (
	environ Cfg

	log  *zap.Logger
	slog *zap.SugaredLogger
)

func init() {
	log, slog = daemonutils.SetupLogs()
}

// This function translates the "deviceNumId" passed in the message (Google
// says this is globally unique) to the Cloud UUID which we selected at device
// provisioning time.  When we see new devices for the first time, we register
// them in the ID map, copying in some data from the device registry.
//
// To reduce load on the postgres DB, we can start to add caching of this
// information if it's done with care.  For example, the translation of
// deviceNumID --> cloudUUID should be 100% stable, although the reverse might
// not necessarily be true (for example, a device moved from one registry to
// another could retain its cloudUUID, but its deviceNumId would likely change.
//
func messageToIDMap(ctx context.Context, applianceDB appliancedb.DataStore,
	cloudIoT cloudiotsvc.Service, m *pubsub.Message) (*appliancedb.ApplianceID, error) {
	var idmap *appliancedb.ApplianceID
	var err error

	numID, err := strconv.ParseUint(m.Attributes["deviceNumId"], 10, 64)
	if err != nil {
		return nil, err
	}

	// XXX cache goes here.  See above.
	idmap, err = applianceDB.ApplianceIDByDeviceNumID(ctx, numID)
	if err != nil {
		switch err.(type) {
		case appliancedb.NotFoundError:
			// Move the information we have, and what we've looked up,
			// into the IDMap in the database.
			if m.Attributes["projectId"] != environ.IoTProject ||
				m.Attributes["deviceRegistryLocation"] != environ.IoTRegion ||
				m.Attributes["deviceRegistryId"] != environ.IoTRegistry {
				return nil, fmt.Errorf("Message from unexpected registry: %v", m)
			}
			device, err := cloudIoT.GetDevice(m.Attributes["deviceId"])
			if err != nil {
				return nil, err
			}
			u, err := uuid.FromString(device.Metadata["net_b10e_iot_cloud_uuid"])
			if err != nil {
				slog.Errorf("failed to make uuid from %s", device.Metadata["net_b10e_iot_cloud_uuid"])
				return nil, err
			}
			idmap = &appliancedb.ApplianceID{
				CloudUUID:         u,
				GCPIoTProject:     m.Attributes["projectId"],
				GCPIoTRegion:      m.Attributes["deviceRegistryLocation"],
				GCPIoTRegistry:    m.Attributes["deviceRegistryId"],
				GCPIoTDeviceID:    m.Attributes["deviceId"],
				GCPIoTDeviceNumID: numID,
			}
			slog.Infow("Transfering device information from Registry to Database", "id", idmap)
			err = applianceDB.UpsertApplianceID(ctx, idmap)
			if err != nil {
				return nil, err
			}
		default:
			slog.Errorf("Error: %s", err)
			return nil, err
		}
	}
	return idmap, nil
}

func upbeatMessage(ctx context.Context, applianceDB appliancedb.DataStore,
	idmap *appliancedb.ApplianceID, m *pubsub.Message) {

	upbeat := &cloud_rpc.UpcallRequest{}

	// For now we have nothing we can really do with malformed messages
	defer m.Ack()

	// The logic below is convoluted, on a temporary basis, to allow
	// both JSON and protobufs messages through.  In the future, it
	// should just be protobufs
	dst, err := base64.StdEncoding.DecodeString(string(m.Data))
	if err == nil {
		slog.Debugf("Looks like a base64 encoded message: <%s>", dst)
		err = proto.Unmarshal(dst, upbeat)
		if err != nil {
			slog.Errorw("failed to unmarshal", "message", m, "error", err, "data", string(m.Data))
			return
		}
	} else {
		// XXX temporary: Try again, treating m.Data as not-base64-encoded JSON
		err = json.Unmarshal(m.Data, upbeat)
		if err != nil {
			slog.Errorw("failed to decode as protobuf and as JSON", "error", err, "data", string(m.Data))
			return
		}
	}

	if upbeat.BootTime == nil || upbeat.RecordTime == nil {
		slog.Errorw("field check failed")
		return
	}

	bootTS, err := time.Parse(time.RFC3339, *upbeat.BootTime)
	if err != nil {
		slog.Errorf("Couldn't Parse BootTime %s: %s", *upbeat.BootTime, err)
		return
	}
	recordTS, err := time.Parse(time.RFC3339, *upbeat.RecordTime)
	if err != nil {
		slog.Errorf("Couldn't Parse RecordTime %s: %s", *upbeat.RecordTime, err)
		return
	}
	upbeatIngest := &appliancedb.UpbeatIngest{
		ApplianceID: idmap.CloudUUID,
		BootTS:      bootTS,
		RecordTS:    recordTS,
	}
	slog.Infow("Insert upbeat ingest", "upbeat", upbeatIngest)
	err = applianceDB.InsertUpbeatIngest(ctx, upbeatIngest)
	if err != nil {
		slog.Errorw("Failed upbeat ingest insert", "error", err)
	}
}

func exceptionMessage(ctx context.Context, applianceDB appliancedb.DataStore,
	idmap *appliancedb.ApplianceID, m *pubsub.Message) {

	exc := &cloud_rpc.NetException{}

	// For now we have nothing we can really do with malformed messages
	defer m.Ack()

	excBuf, err := base64.StdEncoding.DecodeString(string(m.Data))
	if err != nil {
		slog.Errorw("failed to decode", "message", m, "error", err, "data", string(m.Data))
		return
	}
	err = proto.Unmarshal(excBuf, exc)
	if err != nil {
		slog.Errorw("failed to unmarshal", "message", m, "error", err)
		return
	}

	// This is temporary.  For now we don't store exceptions except as JSON blobs.
	jsonExc, err := json.Marshal(exc)
	if err != nil {
		slog.Errorw("failed to json.Marhal", "message", m, "error", err, "data", string(m.Data))
		return
	}
	slog.Infow("Client Exception", "appliance", idmap, "exception", string(jsonExc))
}

func main() {
	var environ Cfg

	defer log.Sync()
	flag.Parse()
	log, slog = daemonutils.ResetupLogs()

	err := envcfg.Unmarshal(&environ)
	if err != nil {
		slog.Fatalf("failed environment configuration: %v", err)
	}

	slog.Infow(pname+" starting", "args", os.Args, "envcfg", environ)

	if len(environ.PrometheusPort) != 0 {
		http.Handle("/metrics", promhttp.Handler())
		go http.ListenAndServe(environ.PrometheusPort, nil)
		slog.Info("prometheus client launched")
	}

	ctx := context.Background()

	// Connect to Postgres DB
	applianceDB, err := appliancedb.Connect(environ.PostgresConnection)
	if err != nil {
		slog.Fatalf("failed to connect to DB: %v", err)
	}
	err = applianceDB.Ping()
	if err != nil {
		slog.Fatalf("failed to ping DB: %s", err)
	}
	slog.Infow(checkMark + "Can connect to and ping SQL DB")

	// Connect to Google Cloud IoT API
	cloudIoT, err := cloudiotsvc.NewDefaultService(ctx,
		environ.IoTProject, environ.IoTRegion, environ.IoTRegistry)
	if err != nil {
		slog.Fatalf("failed to create IoT service: %s", err)
	}

	registry, err := cloudIoT.GetRegistry()
	if err != nil {
		slog.Fatalf("failed to lookup IoT registry: %s", err)
	}
	slog.Infof(checkMark+"Found IoT registry %s", registry.Name)

	events, err := cloudIoT.SubscribeEvents(ctx)
	if err != nil {
		slog.Fatalf("failed to subscribe to IoT events: %s", err)
	}

	slog.Infof(checkMark + "Starting event receiver")
	err = events.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		var idmap *appliancedb.ApplianceID

		slog.Debugw("Message", "data", string(m.Data), "attrs", m.Attributes)
		idmap, err = messageToIDMap(ctx, applianceDB, cloudIoT, m)
		if err != nil {
			slog.Errorw("failed ID mapping", "error", err, "message", m)
			return
		}
		switch m.Attributes["subFolder"] {
		case "upbeat":
			upbeatMessage(ctx, applianceDB, idmap, m)
		case "exception":
			exceptionMessage(ctx, applianceDB, idmap, m)
		default:
			slog.Errorf("unknown message type (subfolder): %s", m.Attributes["subFolder"])
		}
	})
	if err != nil {
		slog.Fatalf("failed to Receive(): %s", err)
	}

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Infof("Signal (%v) received, stopping", s)
}
