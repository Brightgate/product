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
	"encoding/base64"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/base_def"
	"bg/base_msg"
	"bg/common"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"bg/ap_common/iotcore"
	"bg/cloud_rpc"
)

var (
	logDir  = flag.String("logdir", "", "Log file directory")
	logFile *os.File

	credPathFlag = flag.String("cred-path", "etc/secret/iotcore/iotcore.secret.json", "JSON credential file for this IoTCore device")

	levelFlag   = zap.LevelFlag("log-level", zapcore.InfoLevel, "Log level [debug,info,warn,error,panic,fatal]")
	logger      *zap.Logger
	slogger     *zap.SugaredLogger
	zapConfig   zap.Config
	globalLevel zap.AtomicLevel

	metrics struct {
		events prometheus.Counter
	}
)

const pname = "ap.iotd"

func publishEventProto(iotc iotcore.IoTMQTTClient, subfolder string, evt proto.Message) error {
	pb, err := proto.Marshal(evt)
	if err != nil {
		return errors.Wrapf(err, "Failed to marshal %s to protobuf", subfolder)
	}
	pb64 := base64.StdEncoding.EncodeToString(pb)
	slogger.Debugw("Sending "+subfolder, "payload", evt)
	t := iotc.PublishEvent(subfolder, pb64)
	if t.Wait() && t.Error() != nil {
		return errors.Wrap(t.Error(), "Send failed")
	}
	slogger.Infow("Sent "+subfolder, "payload", evt)
	return nil
}

func publishUpbeat(iotc iotcore.IoTMQTTClient) error {
	// Build UpcallRequest
	bootTime := aputil.LinuxBootTime()

	// Retrieve component versions.
	versions := make([]string, 0)
	versions = append(versions, "git:rPS@"+common.GitVersion)

	upbeat := &cloud_rpc.UpcallRequest{
		BootTime:         proto.String(bootTime.Format(time.RFC3339)),
		RecordTime:       proto.String(time.Now().Format(time.RFC3339)),
		ComponentVersion: versions,
	}

	return publishEventProto(iotc, "upbeat", upbeat)
}

func handleNetException(ctx context.Context, iotc iotcore.IoTMQTTClient, event []byte) error {
	exception := &base_msg.EventNetException{}
	err := proto.Unmarshal(event, exception)
	if err != nil {
		return errors.Wrap(err, "Failed to unmarshal exception")
	}
	slogger.Infof("[net.exception] %v", exception)
	metrics.events.Inc()

	t := aputil.ProtobufToTime(exception.Timestamp)
	timestamp := t.Format(time.RFC3339)

	cloudNetExc := &cloud_rpc.NetException{
		Timestamp: proto.String(timestamp),
	}

	if exception.Protocol != nil {
		protocols := base_msg.Protocol_name
		num := int32(*exception.Protocol)
		cloudNetExc.Protocol = proto.String(protocols[num])
	}

	if exception.Reason != nil {
		reasons := base_msg.EventNetException_Reason_name
		num := int32(*exception.Reason)
		cloudNetExc.Reason = proto.String(reasons[num])
	}

	if exception.Message != nil {
		cloudNetExc.Message = proto.String(*exception.Message)
	}

	if exception.MacAddress != nil {
		cloudNetExc.MacAddress = proto.Uint64(*exception.MacAddress)
	}

	if exception.Ipv4Address != nil {
		cloudNetExc.Ipv4Address = proto.Uint32(*exception.Ipv4Address)
	}

	if len(exception.Details) > 0 {
		cloudNetExc.Details = exception.Details
	}

	return publishEventProto(iotc, "exception", cloudNetExc)
}

func onConfig(iotc *iotcore.IoTMQTTClient, message mqtt.Message) {
	slogger.Infow("Received Configuration", "config", string(message.Payload()))
}

func zapSetup() {
	var err error
	zapConfig = zap.NewDevelopmentConfig()
	globalLevel = zap.NewAtomicLevelAt(*levelFlag)
	zapConfig.Level = globalLevel
	logger, err = zapConfig.Build()
	if err != nil {
		log.Panicf("can't zap: %s", err)
	}
	slogger = logger.Sugar()
	_ = zap.RedirectStdLog(logger)
}

func getCred() (*iotcore.IoTCredential, error) {
	credPath := aputil.ExpandDirPath(*credPathFlag)

	var credFile []byte
	credFile, err := ioutil.ReadFile(credPath)
	if err != nil {
		return nil, err
	}
	c, err := iotcore.NewCredentialFromJSON(credFile)
	return c, err
}

func prometheusInit() {
	metrics.events = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "iotd_events_handled",
		Help: "Number of events logged.",
	})
	prometheus.MustRegister(metrics.events)
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.IOTD_PROMETHEUS_PORT, nil)
}

func main() {
	var wg sync.WaitGroup

	flag.Parse()
	zapSetup()
	ctx := context.Background()

	mcpd, err := mcp.New(pname)
	if err != nil {
		slogger.Fatalf("Failed to connect to mcp: %s", err)
	}

	iotCred, err := getCred()
	if err != nil {
		mcpd.SetState(mcp.BROKEN)
		slogger.Fatalf("Failed to build credential: %s", err)
	}

	prometheusInit()
	b := broker.New(pname)
	defer b.Fini()

	iotcore.MQTTLogToZap(logger)
	iotc, err := iotcore.NewMQTTClient(iotCred, iotcore.DefaultTransportOpts)
	if err != nil {
		slogger.Fatalf("Could not initialize IoT core client: %s", err)
	}
	if token := iotc.Connect(); token.Wait() && token.Error() != nil {
		slogger.Fatalf("Could not connect IoT core client: %s", token.Error())
	}

	b.Handle(base_def.TOPIC_EXCEPTION, func(event []byte) {
		wg.Add(1)
		defer wg.Done()
		err := handleNetException(ctx, iotc, event)
		if err != nil {
			slogger.Errorf("Failed handleNetException: %s", err)
		}
	})

	mcpd.SetState(mcp.ONLINE)

	ticker := time.NewTicker(time.Minute * 7)
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
MainLoop:
	for {
		select {
		case s := <-sig:
			slogger.Infof("Signal (%v) received, waiting for tasks to drain\n", s)
			wg.Wait()
			break MainLoop
		case <-ticker.C:
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := publishUpbeat(iotc)
				if err != nil {
					slogger.Errorf("Failed upbeat: %s", err)
				}
			}()
		}
	}
	iotc.Disconnect(250)
	slogger.Infof("Exiting")
}
