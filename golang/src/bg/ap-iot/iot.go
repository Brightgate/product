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
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/iotcore"
	"bg/cloud_rpc"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
	"github.com/tomazk/envcfg"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type cfg struct {
	// Override configuration value.
	LocalMode bool
	SrvURL    string
}

var services = map[string]bool{
	"upbeat":      true,
	"upbeat-loop": true,
	"exception":   true,
}

var (
	credPathFlag = flag.String("cred-path", "etc/secret/iotcore/iotcore.secret.json", "JSON credential file for this IoTCore device")
	levelFlag    = zap.LevelFlag("log-level", zapcore.InfoLevel, "Log level [debug,info,warn,error,panic,fatal]")

	environ cfg

	logger      *zap.Logger
	slogger     *zap.SugaredLogger
	zapConfig   zap.Config
	globalLevel zap.AtomicLevel

	// ApVersion will be replaced by go build step.
	ApVersion = "undefined"
)

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
	versions = append(versions, "git:rPS@"+ApVersion)

	upbeat := &cloud_rpc.UpcallRequest{
		BootTime:         proto.String(bootTime.Format(time.RFC3339)),
		RecordTime:       proto.String(time.Now().Format(time.RFC3339)),
		ComponentVersion: versions,
	}
	return publishEventProto(iotc, "upbeat", upbeat)
}

func publishException(iotc iotcore.IoTMQTTClient) error {
	// Build a "test" Exception
	exc := &cloud_rpc.NetException{
		Reason:  proto.String("TEST"),
		Message: proto.String("test exception"),
	}

	return publishEventProto(iotc, "exception", exc)
}

func onConfig(iotc iotcore.IoTMQTTClient, message mqtt.Message) {
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

func upbeatLoop(iotc iotcore.IoTMQTTClient) {
	sigs := make(chan os.Signal, 1)
	ticker := time.NewTicker(time.Minute)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	err := publishUpbeat(iotc)
	if err != nil {
		slogger.Errorw("Upbeat failed", "err", err)
	}

	for {
		select {
		case <-ticker.C:
			err := publishUpbeat(iotc)
			if err != nil {
				slogger.Errorw("Upbeat failed", "err", err)
			}
		case termSig := <-sigs:
			slogger.Infof("Received signal %s. Stopping", termSig)
			ticker.Stop()
			return
		}
	}
}

func usage(format string, opts ...interface{}) {
	pname := filepath.Base(os.Args[0])
	errmsg := fmt.Sprintf(format, opts...)
	if errmsg != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", errmsg)
	}
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "    %s [options] upbeat\n", pname)
	fmt.Fprintf(os.Stderr, "    %s [options] upbeat-loop\n", pname)
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	flag.Parse()
	zapSetup()

	if len(flag.Args()) == 0 {
		usage("")
	}

	svc := flag.Args()[0]
	if !services[svc] {
		usage("Unknown service %s\n", svc)
	}

	credPath := aputil.ExpandDirPath(*credPathFlag)

	var credFile []byte
	credFile, err := ioutil.ReadFile(credPath)
	if err != nil {
		slogger.Fatalf("Failed to read credential file: %s", err)
	}
	iotCred, err := iotcore.NewCredentialFromJSON(credFile)
	if err != nil {
		slogger.Fatalf("Failed to build credential: %s", err)
	}

	err = envcfg.Unmarshal(&environ)
	if err != nil {
		slogger.Fatalf("Failed to unmarshal from environment: %s", err)
	}

	iotcore.MQTTLogToZap(logger)
	iotc, err := iotcore.NewMQTTClient(iotCred, iotcore.DefaultTransportOpts)
	if err != nil {
		slogger.Fatalf("Could not initialize IoT core client: %s", err)
	}
	if token := iotc.Connect(); token.Wait() && token.Error() != nil {
		slogger.Fatalf("Could not connect IoT core client: %s", token.Error())
	}
	defer iotc.Disconnect(250)

	switch svc {
	case "exception":
		err := publishException(iotc)
		if err != nil {
			slogger.Errorw("publishException failed", "err", err)
		}
	case "upbeat":
		err := publishUpbeat(iotc)
		if err != nil {
			slogger.Errorw("publishUpbeat failed", "err", err)
		}
	case "upbeat-loop":
		// Subscribe to config events; this is basically demo-ware for now
		iotc.SubscribeConfig(onConfig)
		upbeatLoop(iotc)
	}

}
