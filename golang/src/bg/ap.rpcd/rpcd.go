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
	"sync"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/cloud/rpcclient"
	"bg/ap_common/mcp"
	"bg/base_def"
	"bg/base_msg"
	"bg/cloud_rpc"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	any "github.com/golang/protobuf/ptypes/any"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
)

var (
	connectFlag   = flag.String("connect", base_def.CL_SVC_RPC, "Override connection endpoint in credential")
	levelFlag     = zap.LevelFlag("log-level", zapcore.InfoLevel, "Log level [debug,info,warn,error,panic,fatal]")
	deadlineFlag  = flag.Duration("rpc-deadline", time.Second*20, "RPC completion deadline")
	enableTLSFlag = flag.Bool("enable-tls", true, "Enable Secure gRPC")

	logger      *zap.Logger
	slogger     *zap.SugaredLogger
	zapConfig   zap.Config
	globalLevel zap.AtomicLevel

	metrics struct {
		events prometheus.Counter
	}
)

const pname = "ap.rpcd"

func publishEvent(ctx context.Context, tclient cloud_rpc.EventClient, subtopic string, evt proto.Message) error {
	name := proto.MessageName(evt)
	slogger.Debugw("Sending "+subtopic, "type", name, "payload", evt)
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
		return errors.Wrapf(err, "Failed to Put() Event")
	}
	slogger.Infow("Sent "+subtopic, "payload", evt, "response", response)
	return nil
}

func publishHeartbeat(ctx context.Context, tclient cloud_rpc.EventClient) error {
	bootTime, err := ptypes.TimestampProto(aputil.LinuxBootTime())
	if err != nil {
		slogger.Fatalf("failed to make Heartbeat: %v", err)
	}
	heartbeat := &cloud_rpc.Heartbeat{
		BootTime:   bootTime,
		RecordTime: ptypes.TimestampNow(),
	}

	return publishEvent(ctx, tclient, "heartbeat", heartbeat)
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
	grpc_zap.ReplaceGrpcLogger(logger)
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

func heartbeat(ctx context.Context, tclient cloud_rpc.EventClient, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := publishHeartbeat(ctx, tclient)
		if err != nil {
			slogger.Errorf("Failed heartbeat: %s", err)
		}
	}()
}

func main() {
	var wg sync.WaitGroup

	flag.Parse()
	zapSetup()
	ctx := context.Background()
	slogger.Infof("ap.rpcd starting")

	mcpd, err := mcp.New(pname)
	if err != nil {
		slogger.Fatalf("Failed to connect to mcp: %s", err)
	}

	applianceCred, err := rpcclient.SystemCredential()
	if err != nil {
		_ = mcpd.SetState(mcp.BROKEN)
		slogger.Fatalf("Failed to build credential: %s", err)
	}

	prometheusInit()
	b := broker.New(pname)
	defer b.Fini()

	if !*enableTLSFlag {
		slogger.Warnf("Connecting insecurely due to '-enable-tls=false' flag (developers only!)")
	}
	slogger.Infof("Connecting to '%s'", *connectFlag)

	ctx, err = applianceCred.MakeGRPCContext(ctx)
	if err != nil {
		slogger.Fatalf("Failed to make GRPC credential: %+v", err)
	}

	slogger.Infof("Setting state ONLINE")
	err = mcpd.SetState(mcp.ONLINE)
	if err != nil {
		slogger.Fatalf("Failed to set ONLINE: %+v", err)
	}

	conn, err := rpcclient.NewRPCClient(*connectFlag, *enableTLSFlag, pname)
	if err != nil {
		slogger.Fatalf("Failed to make RPC client: %+v", err)
	}
	defer conn.Close()
	slogger.Infof("RPC client connected")
	tclient := cloud_rpc.NewEventClient(conn)

	b.Handle(base_def.TOPIC_EXCEPTION, func(event []byte) {
		wg.Add(1)
		defer wg.Done()
		err := handleNetException(ctx, tclient, event)
		if err != nil {
			slogger.Errorf("Failed handleNetException: %s", err)
		}
	})

	ticker := time.NewTicker(time.Minute * 7)
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	heartbeat(ctx, tclient, &wg)
MainLoop:
	for {
		select {
		case s := <-sig:
			slogger.Infof("Signal (%v) received, waiting for tasks to drain\n", s)
			wg.Wait()
			break MainLoop
		case <-ticker.C:
			heartbeat(ctx, tclient, &wg)
		}
	}
	slogger.Infof("Exiting")
}
