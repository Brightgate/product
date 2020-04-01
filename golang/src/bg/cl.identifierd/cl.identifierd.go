/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

/*
 * cl.identifierd ingests new observations received by the cloud from the
 * appliance fleet.  It summarizes these observations as sentences, and keeps a
 * window of recent sentences for each known client device.  It loads and runs
 * a bayesian classifier to produce classifications from the sentences.
 * It pushes these to the config trees of the corresponding sites.
 *
 * DeviceInfo records are backfilled from cloud storage as necessary.
 */

package main

import (
	"context"
	"flag"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"bg/base_def"
	"bg/cl-obs/classifier"
	"bg/cl-obs/modeldb"
	"bg/cl_common/daemonutils"
	"bg/cl_common/deviceinfo"
	"bg/cloud_models/appliancedb"

	"github.com/klauspost/oui"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"github.com/tomazk/envcfg"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.uber.org/zap"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/storage"
)

const checkMark = `✔︎ `

// Cfg contains the environment variable-based configuration settings
type Cfg struct {
	DiagPort           string `envcfg:"B10E_CLIDENTIFIERD_DIAG_PORT"`
	PostgresConnection string `envcfg:"B10E_CLIDENTIFIERD_POSTGRES_CONNECTION"`
	PubsubProject      string `envcfg:"B10E_CLIDENTIFIERD_PUBSUB_PROJECT"`
	PubsubTopic        string `envcfg:"B10E_CLIDENTIFIERD_PUBSUB_TOPIC"`
	OUIFile            string `envcfg:"B10E_CLIDENTIFIERD_OUIFILE"`
	ConfigdConnection  string `envcfg:"B10E_CLIDENTIFIERD_CLCONFIGD_CONNECTION"`
	DisableTLS         bool   `envcfg:"B10E_CLIDENTIFIERD_CLCONFIGD_DISABLE_TLS"`
	ModelURL           string `envcfg:"B10E_CLIDENTIFIERD_MODEL_URL"`
	DisablePush        bool   `envcfg:"B10E_CLIDENTIFIERD_DISABLE_PUSH"`
}

const (
	pname          = "cl.identifierd"
	ouiDefaultFile = "etc/oui.txt"
)

var (
	environ Cfg

	log  *zap.Logger
	slog *zap.SugaredLogger
)

func processEnv(environ *Cfg) {
	if environ.PostgresConnection == "" {
		slog.Fatalf("B10E_CLIDENTIFIERD_POSTGRES_CONNECTION must be set")
	}
	if environ.PubsubProject == "" {
		p, err := metadata.ProjectID()
		if err != nil {
			slog.Fatalf("Couldn't determine GCE ProjectID")
		}
		environ.PubsubProject = p
		slog.Infof("B10E_CLIDENTIFIERD_PUBSUB_PROJECT defaulting to %v", p)
	}
	if environ.PubsubTopic == "" {
		slog.Fatalf("B10E_CLIDENTIFIERD_PUBSUB_TOPIC must be set")
	}
	if environ.DiagPort == "" {
		environ.DiagPort = base_def.CLIDENTIFIERD_DIAG_PORT
	}
	slog.Infof(checkMark + "Environ looks good")
}

func prometheusInit(prometheusPort string) {
	if len(prometheusPort) == 0 {
		slog.Warnf("Prometheus disabled")
		return
	}
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(prometheusPort, nil)
		if err != nil {
			slog.Warnf("failed to start Prometheus listener: %s", err)
		}
	}()
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

func bayesClassifiersFromDB(modelPath string) ([]*classifier.BayesClassifier, error) {
	mdb, err := modeldb.OpenSQLite(modelPath)
	if err != nil {
		return nil, errors.Wrap(err, "open model")
	}
	classifiers, err := mdb.GetModels()
	if err != nil {
		return nil, errors.Wrap(err, "get models")
	}
	mdb.Close()

	result := make([]*classifier.BayesClassifier, 0)

	for _, rc := range classifiers {
		if rc.ClassifierType != "bayes" {
			continue
		}
		cl, err := classifier.NewBayesClassifier(rc)
		if err != nil {
			return nil, errors.Wrap(err, "failed to make bayes classifier")
		}
		result = append(result, cl)
	}
	return result, nil
}

func main() {
	log, slog = daemonutils.SetupLogs()
	flag.Parse()
	log, slog = daemonutils.ResetupLogs()
	defer func() { _ = log.Sync() }()

	err := envcfg.Unmarshal(&environ)
	if err != nil {
		slog.Fatalf("failed environment configuration: %v", err)
	}
	processEnv(&environ)
	slog.Infow(pname+" starting", "args", strings.Join(os.Args, " "))

	clRoot := daemonutils.ClRoot()
	var ouiFile string
	defaultOUIFile := filepath.Join(clRoot, ouiDefaultFile)
	if environ.OUIFile != "" {
		ouiFile = environ.OUIFile
	} else {
		ouiFile = defaultOUIFile
	}
	ouiDB, err := oui.OpenStaticFile(ouiFile)
	if err != nil {
		slog.Fatalf("unable to open OUI database: %s", err)
	}

	prometheusInit(environ.DiagPort)

	ctx, cancel := context.WithCancel(context.Background())

	if environ.DisablePush {
		slog.Warnf("pushing to config trees is disabled by DISABLE_PUSH!")
	}

	modelURL := environ.ModelURL
	if modelURL == "" {
		modelURL = "gs://bg-classifier-support/trained-models.db"
	}
	modelPath, err := modeldb.GetModelFromURL(modelURL)
	if err != nil {
		slog.Fatalf("model get (%s): %s", modelURL, err)
	}
	slog.Infof(checkMark+"got model URL %s; loaded from %s", modelURL, modelPath)
	bayesClassifiers, err := bayesClassifiersFromDB(modelPath)
	if err != nil {
		slog.Fatalf("newClassifier: %s", err)
	}
	mfgLookupClassifier := classifier.NewMfgLookupClassifier(ouiDB)

	applianceDB := makeApplianceDB(environ.PostgresConnection)
	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		slog.Fatalf("failed storage client creation: %v", err)
	}
	uuidToCSMapper := func(ctx context.Context, siteUUID uuid.UUID) (string, string, error) {
		res, err := applianceDB.CloudStorageByUUID(ctx, siteUUID)
		if err != nil {
			return "", "", err
		}
		return res.Provider, res.Bucket, nil
	}
	cloudStore := deviceinfo.NewGCSStore(storageClient, uuidToCSMapper)

	handler := newInventoryHandler(applianceDB, storageClient, cloudStore,
		ouiDB, bayesClassifiers, mfgLookupClassifier)

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
		applianceUUIDstr := m.Attributes["appliance_uuid"]
		siteUUIDstr := m.Attributes["site_uuid"]
		if applianceUUIDstr == "" || siteUUIDstr == "" {
			slog.Errorw("missing uuid or site attribute", "message", m)
			// We don't want to see this again
			m.Ack()
			return
		}
		siteUUID, err := uuid.FromString(siteUUIDstr)
		if err != nil {
			slog.Errorw("bad site uuid", "message", m)
			// We don't want to see this again
			m.Ack()
			return
		}

		typeName := strings.TrimPrefix(m.Attributes["typeURL"], base_def.API_PROTOBUF_URL+"/")
		// As we accumulate more of these, transition to a lookup table
		switch typeName {
		case "cloud_rpc.InventoryReport":
			handler.InventoryMessage(ctx, siteUUID, m)
		default:
		}
		m.Ack()
	})
	if err != nil {
		slog.Fatalf("failed to Receive(): %s", err)
	}
	slog.Infof("Shutdown complete.")
}
