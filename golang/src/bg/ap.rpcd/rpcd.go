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
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	"google.golang.org/grpc"
)

var (
	connectFlag   = flag.String("connect", "", "Override connection endpoint in credential")
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
	brokerd       *broker.Broker

	metrics struct {
		events prometheus.Counter
	}

	cleanup struct {
		chans []chan bool
		wg    sync.WaitGroup
	}
)

const (
	urlProperty = "@/cloud/svc_rpc/url"
	tlsProperty = "@/cloud/svc_rpc/tls"
	defaultURL  = "svc1.b10e.net:4430"
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
	slogger.Debugf("Sent: %s  size %d  response: %v", subtopic,
		len(serialized), response)
	if response.Result == cloud_rpc.PutEventResponse_BAD_ENDPOINT {
		// Allow our cl.rpcd partner to redirect us to a new endpoint
		slogger.Infof("Moving to new RPC server: " + response.Url)
		err := config.CreateProp(urlProperty, response.Url, nil)
		if err == nil {
			go daemonStop()
		} else {
			slogger.Warnf("failed to update %s: %v", urlProperty,
				err)
		}
	}

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

func grpcInit() (*grpc.ClientConn, error) {
	var err error

	connectURL := *connectFlag
	enableTLS := *enableTLSFlag

	applianceCred, err = grpcutils.SystemCredential()
	if err != nil {
		return nil, fmt.Errorf("loading appliance credentials: %v", err)
	}

	if brokerd != nil {
		config, err = apcfg.NewConfigd(brokerd, pname,
			cfgapi.AccessInternal)
		if err != nil {
			return nil, fmt.Errorf("connecting to configd: %v", err)
		}
	}

	if config != nil {
		if url, err := config.GetProp(urlProperty); err == nil {
			connectURL = url
		}
		if tls, err := config.GetProp(tlsProperty); err == nil {
			enableTLS = (strings.ToLower(tls) == "true")
		}
	}

	if !enableTLS {
		slogger.Warnf("Connecting insecurely due to '-enable-tls=false'" +
			"flag (developers only!)")
	}

	if connectURL == "" {
		// XXX: rather than having a single hardcoded default, we could
		// have a list, or iterate over svc[0-X].b10e.net until we find
		// one willing to respond.
		connectURL = defaultURL
		slogger.Warnf(urlProperty + " not set - defaulting to " +
			defaultURL)
	}

	slogger.Infof("Connecting to '%s'", connectURL)

	conn, err := grpcutils.NewClientConn(connectURL, enableTLS, pname)
	if err != nil {
		return nil, fmt.Errorf("making RPC client: %+v", err)
	}

	return conn, err
}

func addDoneChan() chan bool {
	dc := make(chan bool, 1)

	if cleanup.chans == nil {
		cleanup.chans = make([]chan bool, 0)
	}
	cleanup.chans = append(cleanup.chans, dc)
	cleanup.wg.Add(1)

	return dc
}

func daemonStop() {
	slogger.Infof("shutting down threads")
	for _, c := range cleanup.chans {
		c <- true
	}

	cleanup.wg.Wait()
	slogger.Infof("Exiting")
	os.Exit(0)
}

func daemonStart() {
	slogger.Infof("ap.rpcd starting")
	ctx := context.Background()

	// We don't do this in cmd mode because it's noisy at start
	grpc_zap.ReplaceGrpcLogger(logger)
	mcpd, err := mcp.New(pname)
	if err != nil {
		slogger.Fatalf("Failed to connect to mcp: %s", err)
	}

	prometheusInit()
	brokerd = broker.New(pname)
	brokerd.Handle(base_def.TOPIC_CONFIG, configEvent)
	defer brokerd.Fini()

	conn, err := grpcInit()
	if err != nil {
		mcpd.SetState(mcp.BROKEN)
		slogger.Fatalf("grpc init failed: %v", err)
	}
	defer conn.Close()

	tclient := cloud_rpc.NewEventClient(conn)
	cclient := cloud_rpc.NewConfigBackEndClient(conn)
	slogger.Debugf("RPC client connected")

	brokerd.Handle(base_def.TOPIC_EXCEPTION, func(event []byte) {
		cleanup.wg.Add(1)
		defer cleanup.wg.Done()
		err := handleNetException(ctx, tclient, event)
		if err != nil {
			slogger.Errorf("Failed handleNetException: %s", err)
		}
	})

	go heartbeatLoop(ctx, tclient, &cleanup.wg, addDoneChan())
	go inventoryLoop(ctx, tclient, &cleanup.wg, addDoneChan())
	go configLoop(ctx, cclient, &cleanup.wg, addDoneChan())

	slogger.Infof("Setting state ONLINE")
	err = mcpd.SetState(mcp.ONLINE)
	if err != nil {
		slogger.Fatalf("Failed to set ONLINE: %+v", err)
	}

	exitSig := make(chan os.Signal, 2)
	signal.Notify(exitSig, syscall.SIGINT, syscall.SIGTERM)
	s := <-exitSig
	slogger.Infof("Signal (%v) received, waiting for tasks to drain", s)

	daemonStop()
}

func cmdStart() {
	var wg sync.WaitGroup
	ctx := context.Background()

	conn, err := grpcInit()
	if err != nil {
		slogger.Fatalf("grpc init failed: %v", err)
	}
	defer conn.Close()
	slogger.Debugf("RPC client connected")

	if len(flag.Args()) == 0 {
		log.Fatalf("Service name required.\n")
	}
	stop := make(chan bool)
	svc := flag.Args()[0]
	switch svc {
	case "hello":
		cclient := cloud_rpc.NewConfigBackEndClient(conn)
		err = hello(ctx, cclient)
	case "heartbeat":
		tclient := cloud_rpc.NewEventClient(conn)
		err = publishHeartbeat(ctx, tclient)
	case "heartbeat-loop":
		tclient := cloud_rpc.NewEventClient(conn)
		wg.Add(1)
		heartbeatLoop(ctx, tclient, &wg, stop)
	case "inventory":
		tclient := cloud_rpc.NewEventClient(conn)
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
