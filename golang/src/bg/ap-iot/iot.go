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
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

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
}

var (
	apRootFlag   = flag.String("root", "proto.armv7l/appliance/opt/com.brightgate", "Root of AP installation")
	credPathFlag = flag.String("cred-path", "etc/secret/iotcore/iotcore.secret.json", "JSON credential file for this IoTCore device")

	environ cfg

	logger    *zap.Logger
	slogger   *zap.SugaredLogger
	zapConfig zap.Config

	cachedBootTime time.Time

	// ApVersion will be replaced by go build step.
	ApVersion = "undefined"
)

// LinuxBootTime retrieves the instance boot time using /proc/stat's "btime" field.
func LinuxBootTime() (time.Time, error) {
	if !cachedBootTime.IsZero() {
		return cachedBootTime, nil
	}
	statfile, err := os.Open("/proc/stat")
	if err != nil {
		return time.Time{}, errors.Wrap(err, "/proc/stat")
	}

	scanner := bufio.NewScanner(statfile)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), " ")
		if fields[0] == "btime" {
			var val int64
			val, err = strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return time.Time{}, errors.Wrap(err, "Failed to get Boot Time")
			}
			cachedBootTime = time.Unix(val, 0)
			return cachedBootTime, nil
		}
	}
	if err = scanner.Err(); err != nil {
		return time.Time{}, errors.Wrap(err, "Scanner error")
	}
	return time.Time{}, errors.New("/proc/stat possibly empty")
}

func sendUpbeat(iotc *iotcore.IoTMQTTClient) error {
	// Build UpcallRequest
	bootTime, err := LinuxBootTime()
	if err != nil {
		return errors.Wrap(err, "Failed to get boot time")
	}

	// Retrieve component versions.
	versions := make([]string, 0)
	versions = append(versions, "git:rPS@"+ApVersion)

	request := &cloud_rpc.UpcallRequest{
		BootTime:         proto.String(bootTime.Format(time.RFC3339)),
		RecordTime:       proto.String(time.Now().Format(time.RFC3339)),
		ComponentVersion: versions,
	}

	text, err := json.Marshal(request)
	if err != nil {
		return errors.Wrap(err, "Failed sending Upbeat")
	}
	slogger.Debugw("Sending upbeat", "text", string(text))
	t := iotc.PublishEvent("upbeat", 1, text)
	if t.Wait() && t.Error() != nil {
		slogger.Errorw("upbeat failed", "error", t.Error())
		return errors.Wrap(t.Error(), "Upbeat send failed")
	}
	slogger.Infow("Sent upbeat", "text", string(text))
	return nil
}

func onConfig(iotc *iotcore.IoTMQTTClient, message mqtt.Message) {
	slogger.Infow("Received Configuration", "config", string(message.Payload()))
}

func zapSetup() {
	var err error
	zapConfig = zap.NewDevelopmentConfig()
	logger, err = zapConfig.Build()
	if err != nil {
		log.Panicf("can't zap: %s", err)
	}
	zapConfig.Level.SetLevel(zapcore.InfoLevel)
	slogger = logger.Sugar()
	_ = zap.RedirectStdLog(logger)
}

func upbeatLoop(iotc *iotcore.IoTMQTTClient) {
	sigs := make(chan os.Signal, 1)
	ticker := time.NewTicker(time.Minute)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	err := sendUpbeat(iotc)
	if err != nil {
		slogger.Errorw("Upbeat failed", "err", err)
	}

	for {
		select {
		case <-ticker.C:
			err := sendUpbeat(iotc)
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
	zapSetup()
	flag.Parse()

	if len(flag.Args()) == 0 {
		usage("")
	}

	svc := flag.Args()[0]
	if !services[svc] {
		usage("Unknown service %s\n", svc)
	}

	var credPath string
	if filepath.IsAbs(*credPathFlag) {
		credPath = *credPathFlag
	} else {
		credPath = filepath.Join(*apRootFlag, *credPathFlag)
	}

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
	iotc, err := iotcore.NewMQTTClient(iotCred)
	if err != nil {
		slogger.Fatalf("Could not initialize IoT core client: %s", err)
	}
	if token := iotc.Connect(); token.Wait() && token.Error() != nil {
		slogger.Fatalf("Could not connect IoT core client: %s", err)
	}
	defer iotc.Disconnect(250)

	switch svc {
	case "upbeat":
		err := sendUpbeat(iotc)
		if err != nil {
			slogger.Errorw("Upbeat failed", "err", err)
		}
	case "upbeat-loop":
		// Subscribe to config events; this is basically demo-ware for now
		iotc.SubscribeConfig(onConfig)
		upbeatLoop(iotc)
	}
}
