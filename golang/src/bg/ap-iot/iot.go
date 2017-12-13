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

	jwt "github.com/dgrijalva/jwt-go"
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
	"upbeat": true,
}

var (
	apRootFlag = flag.String("root", "proto.armv7l/appliance/opt/com.brightgate",
		"Root of AP installation")
	pemPathFlag  = flag.String("pem-path", "", "PEM file path")
	projectFlag  = flag.String("project", "", "Google Cloud Project ID [required]")
	regionFlag   = flag.String("region", "us-central1", "Google Cloud Region ID")
	registryFlag = flag.String("registry", "", "Google Cloud IoT Registry ID [required]")
	deviceIDFlag = flag.String("device-id", "", "Device ID in the registry [required]")

	environ cfg

	// ApVersion will be replaced by go build step.
	ApVersion = "undefined"
)

func firstVersion() string {
	return "git:rPS" + ApVersion
}

var cachedBootTime time.Time

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
			val, err := strconv.ParseInt(fields[1], 10, 64)
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

func sendUpbeat(mqttc mqtt.Client) error {
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
	topic := fmt.Sprintf("/devices/%s/events", *deviceIDFlag)
	slogger.Infow("Sending upbeat", "text", string(text))
	mqttc.Publish(topic, 1, false, text)
	return nil
}

func onConfigReceived(client mqtt.Client, message mqtt.Message) {
	slogger.Infow("Received Configuration Msg", "config", string(message.Payload()))
}

func subscribeConfig(mqttc mqtt.Client) {
	topic := fmt.Sprintf("/devices/%s/config", *deviceIDFlag)
	if token := mqttc.Subscribe(topic, 1, onConfigReceived); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
}

// XXX Needs to go?
var msgFunc mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("TOPIC: %s\n", msg.Topic())
	fmt.Printf("MSG: %s\n", msg.Payload())
}

var logger *zap.Logger
var slogger *zap.SugaredLogger
var zapConfig zap.Config

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

func upbeatLoop(mqttc mqtt.Client) {
	sigs := make(chan os.Signal, 1)
	ticker := time.NewTicker(time.Second * 60)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	err := sendUpbeat(mqttc)
	if err != nil {
		slogger.Errorw("Upbeat failed", "err", err)
	}

	for {
		select {
		case <-ticker.C:
			err := sendUpbeat(mqttc)
			if err != nil {
				slogger.Errorw("Upbeat failed", "err", err)
			}
		case <-sigs:
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
	fmt.Fprintf(os.Stderr, "Usage: %s [options] upbeat\n", pname)
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

	if *pemPathFlag == "" {
		usage("Must specify PEM path with -pem-path")
	}
	if *projectFlag == "" {
		usage("Must specify GCP project with -project")
	}
	if *regionFlag == "" {
		usage("Must specify GCP region with -region")
	}
	if *registryFlag == "" {
		usage("Must specify GCP registry with -registry")
	}
	if *deviceIDFlag == "" {
		usage("Must specify Device ID with -device-id")
	}

	envcfg.Unmarshal(&environ)

	pemFile, err := ioutil.ReadFile(*pemPathFlag)
	if err != nil {
		slogger.Fatalf("Failed reading PEM file: %s", err)
	}
	privKey, err := jwt.ParseRSAPrivateKeyFromPEM(pemFile)
	if err != nil {
		slogger.Fatalf("Failed parsing PEM file: %s", err)
	}

	iotcore.MQTTLogToZap(logger)
	mqttc, err := iotcore.NewMQTTClient(*projectFlag, *regionFlag,
		*registryFlag, *deviceIDFlag, privKey, msgFunc)
	if err != nil {
		slogger.Fatalf("Could not initialize IoT core client: %s", err)
	}
	if token := mqttc.Connect(); token.Wait() && token.Error() != nil {
		slogger.Fatalf("Could not connect IoT core client: %s", err)
	}
	defer mqttc.Disconnect(250)
	subscribeConfig(mqttc)

	switch svc {
	case "upbeat":
		upbeatLoop(mqttc)
	}
}
