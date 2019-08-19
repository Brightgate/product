/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"context"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"bg/base_def"
	"bg/cl_common/daemonutils"
	"bg/cloud_models/appliancedb"
	"bg/cloud_rpc"

	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"github.com/tomazk/envcfg"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.uber.org/zap"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/storage"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
)

const checkMark = `✔︎ `

// Cfg contains the environment variable-based configuration settings
type Cfg struct {
	DiagPort           string `envcfg:"B10E_CLEVENTD_DIAG_PORT"`
	PostgresConnection string `envcfg:"B10E_CLEVENTD_POSTGRES_CONNECTION"`
	PubsubProject      string `envcfg:"B10E_CLEVENTD_PUBSUB_PROJECT"`
	PubsubTopic        string `envcfg:"B10E_CLEVENTD_PUBSUB_TOPIC"`
}

const (
	pname = "cl.eventd"
)

var (
	reportBasePath string

	log  *zap.Logger
	slog *zap.SugaredLogger
)

// XXX Should this move to cl_common?  Or should we do like we do for stats and
// drops, and just have the client request a signed URL to write the output to
// directly?
func writeCSObject(ctx context.Context, applianceDB appliancedb.DataStore,
	siteUU uuid.UUID, filePath string, data []byte) (string, error) {
	scs, err := applianceDB.CloudStorageByUUID(ctx, siteUU)
	if err != nil {
		return "", errors.Wrapf(err, "could not get Cloud Storage record for %s",
			siteUU.String())
	}
	if scs.Provider != "gcs" {
		return "", fmt.Errorf("writeCSObject not implemented for provider %s",
			scs.Provider)
	}
	client, err := storage.NewClient(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to create storage client")
	}
	bkt := client.Bucket(scs.Bucket)

	obj := bkt.Object(filePath)
	w := obj.NewWriter(ctx)
	p := gcsBaseURL + path.Join(obj.BucketName(), obj.ObjectName())
	if _, err := w.Write(data); err != nil {
		return "", errors.Wrapf(err, "failed writing to %s", p)
	}
	if err := w.Close(); err != nil {
		return "", errors.Wrapf(err, "failed closing %s", p)
	}

	return p, nil
}

func heartbeatMessage(ctx context.Context, applianceDB appliancedb.DataStore,
	applianceUUID, siteUUID uuid.UUID, m *pubsub.Message) {
	var err error
	heartbeat := &cloud_rpc.Heartbeat{}

	slog := slog.With("appliance_uuid", m.Attributes["appliance_uuid"],
		"site_uuid", m.Attributes["site_uuid"])

	err = proto.Unmarshal(m.Data, heartbeat)
	if err != nil {
		slog.Errorw("failed to decode message", "error", err, "data", string(m.Data))
		return
	}

	if heartbeat.BootTime == nil || heartbeat.RecordTime == nil {
		slog.Errorw("field check failed")
		return
	}

	bootTS, err := ptypes.Timestamp(heartbeat.BootTime)
	if err != nil {
		slog.Errorf("Couldn't Convert BootTS %s: %s", heartbeat.BootTime, err)
		return
	}
	recordTS, err := ptypes.Timestamp(heartbeat.RecordTime)
	if err != nil {
		slog.Errorf("Couldn't Parse RecordTime %s: %s", heartbeat.RecordTime, err)
		return
	}
	heartbeatIngest := &appliancedb.HeartbeatIngest{
		ApplianceUUID: applianceUUID,
		SiteUUID:      siteUUID,
		BootTS:        bootTS.UTC(),
		RecordTS:      recordTS.UTC(),
	}
	slog.Infow("Insert heartbeat ingest", "heartbeat", heartbeatIngest)
	err = applianceDB.InsertHeartbeatIngest(ctx, heartbeatIngest)
	if err != nil {
		slog.Errorw("Failed heartbeat ingest insert", "error", err)
	}
}

func exceptionMessage(ctx context.Context, applianceDB appliancedb.DataStore,
	siteUUID uuid.UUID, m *pubsub.Message) {
	var err error

	exc := &cloud_rpc.NetException{}

	slog := slog.With("appliance_uuid", m.Attributes["appliance_uuid"],
		"site_uuid", m.Attributes["site_uuid"])

	err = proto.Unmarshal(m.Data, exc)
	if err != nil {
		slog.Errorw("failed to unmarshal", "message", m, "error", err)
		return
	}

	marshaler := jsonpb.Marshaler{}
	jsonExc, err := marshaler.MarshalToString(exc)
	if err != nil {
		slog.Errorw("failed to json.Marshal", "message", m, "error", err, "data", string(m.Data))
		return
	}
	slog.Infow("Client Exception", "site", siteUUID, "exception", jsonExc)
	ts := exc.GetTimestamp()
	t, err := ptypes.Timestamp(ts)
	if err != nil {
		slog.Errorw("failed to get time from exception", "error", err)
		return
	}
	var macptr *uint64
	mac := exc.GetMacAddress()
	if mac != 0 {
		macptr = &mac
	}
	err = applianceDB.InsertSiteNetException(ctx, siteUUID, t, exc.GetReason(), macptr, jsonExc)
	if err != nil {
		slog.Errorw("Failed net exception insert", "error", err)
	}
}

func upgradeMessage(ctx context.Context, applianceDB appliancedb.DataStore,
	applianceUUID, siteUUID uuid.UUID, m *pubsub.Message) {

	slog := slog.With("appliance_uuid", m.Attributes["appliance_uuid"],
		"site_uuid", m.Attributes["site_uuid"])

	report := &cloud_rpc.UpgradeReport{}
	err := proto.Unmarshal(m.Data, report)
	if err != nil {
		slog.Errorw("failed to process upgrade report: couldn't unmarshal message",
			"message", m, "error", err)
		return
	}

	relUU, err := uuid.FromString(report.ReleaseUuid)
	if err != nil {
		slog.Errorw("failed to process upgrade report: bad release UUID",
			"error", err)
		return
	}

	// If record_time in the message is missing or invalid, log a warning,
	// but continue with the current server time.
	reportTS, err := ptypes.Timestamp(report.RecordTime)
	if err != nil {
		slog.Warnw("invalid record_time in UpgradeReport", "error", err)
		reportTS = time.Now().UTC()
	}

	switch report.Result {
	case cloud_rpc.UpgradeReport_REPORT:
		slog.Infow("Set current release", "release_uuid", relUU)
		err = applianceDB.SetCurrentRelease(ctx, applianceUUID, relUU, reportTS)
		if err != nil {
			slog.Errorw("failed to process upgrade report: DB failure",
				"error", err)
		}

		// XXX Do we want to log these to the database, too?
	case cloud_rpc.UpgradeReport_SUCCESS:
		filePath := path.Join("upgrade_log", applianceUUID.String(),
			reportTS.Format(time.RFC3339)+"-success")
		url, err := writeCSObject(ctx, applianceDB, siteUUID, filePath, report.Output)
		if err != nil {
			slog.Errorw("failed to archive successful upgrade log",
				"url", url, "error", err)
		} else {
			slog.Infow("archived successful upgrade log", "url", url)
		}
	case cloud_rpc.UpgradeReport_FAILURE:
		filePath := path.Join("upgrade_log", applianceUUID.String(),
			reportTS.Format(time.RFC3339)+"-failure")
		url, err := writeCSObject(ctx, applianceDB, siteUUID, filePath, report.Output)
		if err != nil {
			slog.Errorw("failed to archive failed upgrade log",
				"url", url, "error", err)
		} else {
			slog.Infow("archived failed upgrade log", "url", url)
		}

	default:
		slog.Warnw("unknown upgrade report result",
			"report_result", report.Result)
	}
}

func processEnv(environ *Cfg) {
	if environ.PostgresConnection == "" {
		slog.Fatalf("B10E_CLEVENTD_POSTGRES_CONNECTION must be set")
	}
	if environ.PubsubProject == "" {
		p, err := metadata.ProjectID()
		if err != nil {
			slog.Fatalf("Couldn't determine GCE ProjectID")
		}
		environ.PubsubProject = p
		slog.Infof("B10E_CLEVENTD_PUBSUB_PROJECT defaulting to %v", p)
	}
	if environ.PubsubTopic == "" {
		slog.Fatalf("B10E_CLEVENTD_PUBSUB_TOPIC must be set")
	}
	if environ.DiagPort == "" {
		environ.DiagPort = base_def.CLEVENTD_DIAG_PORT
	}
	slog.Infof(checkMark + "Environ looks good")
}

func prometheusInit(prometheusPort string) {
	if len(prometheusPort) == 0 {
		slog.Warnf("Prometheus disabled")
		return
	}
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(prometheusPort, nil)
	slog.Infof(checkMark+"Prometheus launched on port %v", prometheusPort)
}

func makeSub(ctx context.Context, pubsubClient *pubsub.Client, topicName, subName string) (*pubsub.Subscription, error) {
	sub := pubsubClient.Subscription(subName)
	ok, err := sub.Exists(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to test if subscription %s exists", subName)
	}
	if !ok {
		slog.Infof("Creating pubsub Subscription %v", subName)
		topic := pubsubClient.Topic(topicName)
		sub, err = pubsubClient.CreateSubscription(ctx, subName,
			pubsub.SubscriptionConfig{Topic: topic})
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to CreateSubscription %s", subName)
		}
		slog.Infof(checkMark+"Created pubsub Subscription %v", subName)
	}
	return sub, nil
}

// makeApplianceDB handles connection setup to the appliance database
func makeApplianceDB(postgresURI string) appliancedb.DataStore {
	applianceDB, err := appliancedb.Connect(postgresURI)
	if err != nil {
		slog.Fatalf("failed to connect to DB: %v", err)
	}
	slog.Infof(checkMark + "Connected to Appliance DB")
	err = applianceDB.Ping()
	if err != nil {
		slog.Fatalf("failed to ping DB: %s", err)
	}
	slog.Infof(checkMark + "Pinged Appliance DB")
	return applianceDB
}

func main() {
	var environ Cfg
	log, slog = daemonutils.SetupLogs()
	flag.Parse()
	log, slog = daemonutils.ResetupLogs()
	defer log.Sync()

	err := envcfg.Unmarshal(&environ)
	if err != nil {
		slog.Fatalf("failed environment configuration: %v", err)
	}
	processEnv(&environ)
	slog.Infow(pname+" starting", "args", strings.Join(os.Args, " "))

	reportBasePath = filepath.Join(daemonutils.ClRoot(), "var", "spool")
	slog.Infof("report storage: %s", reportBasePath)

	prometheusInit(environ.DiagPort)

	ctx, cancel := context.WithCancel(context.Background())

	applianceDB := makeApplianceDB(environ.PostgresConnection)

	pubsubClient, err := pubsub.NewClient(ctx, environ.PubsubProject)
	if err != nil {
		slog.Fatalf("failed to make client: %v", err)
	}
	subName := environ.PubsubTopic + "-" + pname
	applianceRegEvents, err := makeSub(ctx, pubsubClient, environ.PubsubTopic, subName)
	if err != nil {
		slog.Fatalf("failed to subscribe: %v", err)
	}

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sig
		slog.Infof("Signal (%v) received, stopping", s)
		cancel()
	}()

	slog.Infof(checkMark + "Starting ApplianceRegistry event receiver")
	err = applianceRegEvents.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		slog.Debugw("Message", "size", len(m.Data), "attrs", m.Attributes)
		applianceUUIDstr := m.Attributes["appliance_uuid"]
		siteUUIDstr := m.Attributes["site_uuid"]
		if applianceUUIDstr == "" || siteUUIDstr == "" {
			slog.Errorw("missing uuid or site attribute", "message", m)
			// We don't want to see this again
			m.Ack()
			return
		}
		applianceUUID, errA := uuid.FromString(applianceUUIDstr)
		siteUUID, errS := uuid.FromString(siteUUIDstr)
		if errA != nil || errS != nil {
			slog.Errorw("bad appliance or site uuid", "message", m)
			// We don't want to see this again
			m.Ack()
			return
		}

		typeName := strings.TrimPrefix(m.Attributes["typeURL"], base_def.API_PROTOBUF_URL+"/")
		// As we accumulate more of these, transition to a lookup table
		switch typeName {
		case "cloud_rpc.Heartbeat":
			heartbeatMessage(ctx, applianceDB, applianceUUID, siteUUID, m)
		case "cloud_rpc.InventoryReport":
			inventoryMessage(ctx, applianceDB, siteUUID, m)
		case "cloud_rpc.FaultReport":
			faultMessage(ctx, applianceDB, siteUUID, m)
		case "cloud_rpc.NetException":
			exceptionMessage(ctx, applianceDB, siteUUID, m)
		case "cloud_rpc.UpgradeReport":
			upgradeMessage(ctx, applianceDB, applianceUUID, siteUUID, m)
		default:
			slog.Errorw("unknown message type", "message", m)
		}
		m.Ack()
	})
	if err != nil {
		slog.Fatalf("failed to Receive(): %s", err)
	}
	slog.Infof("Shutdown complete.")
}
