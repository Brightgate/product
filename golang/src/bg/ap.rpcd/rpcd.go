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
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/base_def"
	"bg/base_msg"
	"bg/cloud_rpc"
	"bg/common/cfgapi"
	"bg/common/grpcutils"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
)

var (
	connectFlag   = flag.String("connect", base_def.CL_SVC_RPC, "Override connection endpoint in credential")
	levelFlag     = zap.LevelFlag("log-level", zapcore.InfoLevel, "Log level [debug,info,warn,error,panic,fatal]")
	deadlineFlag  = flag.Duration("rpc-deadline", time.Second*20, "RPC completion deadline")
	enableTLSFlag = flag.Bool("enable-tls", true, "Enable Secure gRPC")

	pname string

	logger        *zap.Logger
	slogger       *zap.SugaredLogger
	zapConfig     zap.Config
	globalLevel   zap.AtomicLevel
	applianceCred *grpcutils.Credential
	config        *cfgapi.Handle

	metrics struct {
		events prometheus.Counter
	}
)

func publishEvent(ctx context.Context, tclient cloud_rpc.EventClient, subtopic string, evt proto.Message) error {
	name := proto.MessageName(evt)
	slogger.Debugw("Sending "+subtopic, "type", name, "payload", evt)
	serialized, err := proto.Marshal(evt)
	if err != nil {
		return err
	}

	ctx, err = applianceCred.MakeGRPCContext(ctx)
	if err != nil {
		slogger.Fatalf("Failed to make GRPC credential: %+v", err)
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
		return errors.Wrapf(err, "Failed to Put() Event")
	}
	slogger.Infow("Sent "+subtopic, "size", len(serialized), "response", response)
	return nil
}

func handleNetException(ctx context.Context, tclient cloud_rpc.EventClient, event []byte) error {
	exception := &base_msg.EventNetException{}
	err := proto.Unmarshal(event, exception)
	if err != nil {
		return errors.Wrap(err, "Failed to unmarshal exception")
	}
	slogger.Infof("[net.exception] %v", exception)
	metrics.events.Inc()

	cloudNetExc := &cloud_rpc.NetException{
		Timestamp: &timestamp.Timestamp{
			Seconds: *exception.Timestamp.Seconds,
			Nanos:   *exception.Timestamp.Nanos,
		},
	}

	if exception.Protocol != nil {
		protocols := base_msg.Protocol_name
		num := int32(*exception.Protocol)
		cloudNetExc.Protocol = protocols[num]
	}

	if exception.Reason != nil {
		reasons := base_msg.EventNetException_Reason_name
		num := int32(*exception.Reason)
		cloudNetExc.Reason = reasons[num]
	}

	if exception.Message != nil {
		cloudNetExc.Message = *exception.Message
	}

	if exception.MacAddress != nil {
		cloudNetExc.MacAddress = *exception.MacAddress
	}

	if exception.Ipv4Address != nil {
		cloudNetExc.Ipv4Address = *exception.Ipv4Address
	}

	if len(exception.Details) > 0 {
		cloudNetExc.Details = exception.Details
	}

	return publishEvent(ctx, tclient, "exception", cloudNetExc)
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

func prometheusInit() {
	metrics.events = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "rpcd_events_handled",
		Help: "Number of events logged.",
	})
	prometheus.MustRegister(metrics.events)
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.RPCD_PROMETHEUS_PORT, nil)
}

func daemonStart() {
	var wg sync.WaitGroup
	slogger.Infof("ap.rpcd starting")
	ctx := context.Background()

	// We don't do this in cmd mode because it's noisy at start
	grpc_zap.ReplaceGrpcLogger(logger)
	mcpd, err := mcp.New(pname)
	if err != nil {
		slogger.Fatalf("Failed to connect to mcp: %s", err)
	}

	prometheusInit()
	b := broker.New(pname)
	b.Handle(base_def.TOPIC_CONFIG, configEvent)
	defer b.Fini()

	if applianceCred, err = grpcutils.SystemCredential(); err != nil {
		_ = mcpd.SetState(mcp.BROKEN)
		slogger.Fatalf("Failed to load appliance credentials: %v", err)
	}

	if config, err = apcfg.NewConfigd(b, pname, cfgapi.AccessAdmin); err != nil {
		slogger.Fatalf("Failed to connect to configd: %v", err)
	}

	if !*enableTLSFlag {
		slogger.Warnf("Connecting insecurely due to '-enable-tls=false' flag (developers only!)")
	}
	slogger.Infof("Connecting to '%s'", *connectFlag)

	slogger.Infof("Setting state ONLINE")
	err = mcpd.SetState(mcp.ONLINE)
	if err != nil {
		slogger.Fatalf("Failed to set ONLINE: %+v", err)
	}

	conn, err := grpcutils.NewClientConn(*connectFlag, *enableTLSFlag, pname)
	if err != nil {
		slogger.Fatalf("Failed to make RPC client: %+v", err)
	}
	defer conn.Close()
	tclient := cloud_rpc.NewEventClient(conn)
	cclient := cloud_rpc.NewConfigBackEndClient(conn)
	slogger.Debugf("RPC client connected")

	b.Handle(base_def.TOPIC_EXCEPTION, func(event []byte) {
		wg.Add(1)
		defer wg.Done()
		err := handleNetException(ctx, tclient, event)
		if err != nil {
			slogger.Errorf("Failed handleNetException: %s", err)
		}
	})

	stopHeartbeat := make(chan bool)
	stopInventory := make(chan bool)
	stopConfig := make(chan bool)

	wg.Add(3)
	go heartbeatLoop(ctx, tclient, &wg, stopHeartbeat)
	go inventoryLoop(ctx, tclient, &wg, stopInventory)
	go configLoop(ctx, cclient, &wg, stopConfig)

	exitSig := make(chan os.Signal, 2)
	signal.Notify(exitSig, syscall.SIGINT, syscall.SIGTERM)

	s := <-exitSig
	slogger.Infof("Signal (%v) received, waiting for tasks to drain", s)

	stopHeartbeat <- true
	stopInventory <- true
	stopConfig <- true
	wg.Wait()
	slogger.Infof("Exiting")
}

func cmdStart() {
	var err error
	var wg sync.WaitGroup
	ctx := context.Background()

	applianceCred, err = grpcutils.SystemCredential()
	if err != nil {
		slogger.Fatalf("Failed to build credential: %s", err)
	}

	if !*enableTLSFlag {
		slogger.Warnf("Connecting insecurely due to '-enable-tls=false' flag (developers only!)")
	}
	slogger.Debugf("Connecting to '%s'", *connectFlag)

	conn, err := grpcutils.NewClientConn(*connectFlag, *enableTLSFlag, pname)
	if err != nil {
		slogger.Fatalf("Failed to make RPC client: %+v", err)
	}
	defer conn.Close()
	tclient := cloud_rpc.NewEventClient(conn)
	cclient := cloud_rpc.NewConfigBackEndClient(conn)
	slogger.Debugf("RPC client connected")

	if len(flag.Args()) == 0 {
		log.Fatalf("Service name required.\n")
	}
	stop := make(chan bool)
	svc := flag.Args()[0]
	err = nil
	switch svc {
	case "hello":
		err = hello(ctx, cclient)
	case "heartbeat":
		err = publishHeartbeat(ctx, tclient)
	case "heartbeat-loop":
		wg.Add(1)
		heartbeatLoop(ctx, tclient, &wg, stop)
	case "inventory":
		err = sendInventory(ctx, tclient)
	default:
		slogger.Fatalf("Unrecognized command %s", svc)
	}
	if err != nil {
		slogger.Errorf("%s failed: %s", svc, err)
	}
}

func main() {
	exec, err := os.Executable()
	if err != nil {
		panic("couldn't get executable name")
	}
	pname = filepath.Base(exec)

	flag.Parse()
	zapSetup()

	if pname == "ap.rpcd" {
		daemonStart()
	} else if pname == "ap-rpc" {
		cmdStart()
	} else {
		slogger.Fatalf("Couldn't determine program mode")
	}
}
