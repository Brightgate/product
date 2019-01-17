/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
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
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/base_def"
	"bg/base_msg"
	"bg/cloud_rpc"
	"bg/common/cfgapi"
	"bg/common/grpcutils"

	"github.com/pkg/errors"

	"go.uber.org/zap"
	"go.uber.org/zap/zapgrpc"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/golang/protobuf/ptypes/timestamp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
)

var (
	connectFlag = flag.String("connect", "",
		"Override connection endpoint in credential")
	enableTLSFlag = flag.Bool("enable-tls", true, "Enable Secure gRPC")

	templateDir    = apcfg.String("template_dir", "/etc/templates/ap.rpcd", true, nil)
	tunnelLife     = apcfg.Duration("tunnel_lifespan", time.Hour*3, true, nil)
	rpcDeadline    = apcfg.Duration("rpc_deadline", time.Second*20, true, nil)
	maxCmds        = apcfg.Int("max_cmds", 64, true, nil)
	maxCompletions = apcfg.Int("max_completions", 64, true, nil)
	maxUpdates     = apcfg.Int("max_updates", 32, true, nil)
	logLevel       = apcfg.String("log_level", "info", false, aputil.LogSetLevel)

	pname string

	slog          *zap.SugaredLogger
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
	urlProperty    = "@/cloud/svc_rpc/url"
	tlsProperty    = "@/cloud/svc_rpc/tls"
	bucketProperty = "@/cloud/update/bucket"
	defaultURL     = "svc1.b10e.net:4430"
)

func publishEvent(ctx context.Context, tclient cloud_rpc.EventClient, subtopic string, evt proto.Message) error {
	name := proto.MessageName(evt)
	//slog.Debugw("Sending "+subtopic, "type", name, "payload", evt)
	slog.Debugw("Sending "+subtopic, "type", name)
	serialized, err := proto.Marshal(evt)
	if err != nil {
		return err
	}

	ctx, err = applianceCred.MakeGRPCContext(ctx)
	if err != nil {
		slog.Fatalf("Failed to make GRPC credential: %+v", err)
	}

	clientDeadline := time.Now().Add(*rpcDeadline)
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
	slog.Debugf("Sent: %s  size %d  response: %v", subtopic,
		len(serialized), response)
	if response.Result == cloud_rpc.PutEventResponse_BAD_ENDPOINT {
		// Allow our cl.rpcd partner to redirect us to a new endpoint
		slog.Infof("Moving to new RPC server: " + response.Url)
		err := config.CreateProp(urlProperty, response.Url, nil)
		if err == nil {
			go daemonStop()
		} else {
			slog.Warnf("failed to update %s: %v", urlProperty,
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
	slog.Infof("[net.exception] %v", exception)
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

func prometheusInit() {
	metrics.events = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "rpcd_events_handled",
		Help: "Number of events logged.",
	})
	prometheus.MustRegister(metrics.events)
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.RPCD_DIAG_PORT, nil)
}

func grpcInit() (*grpc.ClientConn, error) {
	var err error

	connectURL := *connectFlag
	enableTLS := *enableTLSFlag

	applianceCred, err = aputil.SystemCredential()
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
		if url, cerr := config.GetProp(urlProperty); cerr == nil {
			connectURL = url
		}
		if tls, cerr := config.GetProp(tlsProperty); cerr == nil {
			enableTLS = (strings.ToLower(tls) == "true")
		}
	}

	if !enableTLS {
		slog.Warnf("Connecting insecurely due to '-enable-tls=false' " +
			"flag (developers only!)")
	}

	if connectURL == "" {
		// XXX: rather than having a single hardcoded default, we could
		// have a list, or iterate over svc[0-X].b10e.net until we find
		// one willing to respond.
		connectURL = defaultURL
		slog.Warnf(urlProperty + " not set - defaulting to " +
			defaultURL)
	}

	slog.Infof("Connecting to '%s'", connectURL)

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
	slog.Infof("shutting down threads")
	for _, c := range cleanup.chans {
		c <- true
	}

	cleanup.wg.Wait()
	slog.Infof("Exiting")
	os.Exit(0)
}

func daemonStart() {
	slog.Infof("ap.rpcd starting")
	ctx := context.Background()

	mcpd, err := mcp.New(pname)
	if err != nil {
		slog.Fatalf("Failed to connect to mcp: %s", err)
	}

	prometheusInit()
	brokerd = broker.New(pname)
	defer brokerd.Fini()

	conn, err := grpcInit()
	if err != nil {
		mcpd.SetState(mcp.BROKEN)
		slog.Fatalf("grpc init failed: %v", err)
	}
	defer conn.Close()

	tclient := cloud_rpc.NewEventClient(conn)
	cclient := cloud_rpc.NewConfigBackEndClient(conn)
	sclient := cloud_rpc.NewCloudStorageClient(conn)
	slog.Debugf("RPC client connected")

	brokerd.Handle(base_def.TOPIC_EXCEPTION, func(event []byte) {
		cleanup.wg.Add(1)
		defer cleanup.wg.Done()
		if err = handleNetException(ctx, tclient, event); err != nil {
			slog.Errorf("Failed handleNetException: %s", err)
		}
	})

	go heartbeatLoop(ctx, tclient, &cleanup.wg, addDoneChan())
	go inventoryLoop(ctx, tclient, &cleanup.wg, addDoneChan())
	go updateLoop(&cleanup.wg, addDoneChan())
	go uploadLoop(sclient, &cleanup.wg, addDoneChan())
	go configLoop(ctx, cclient, &cleanup.wg, addDoneChan())
	go tunnelLoop(&cleanup.wg, addDoneChan())

	slog.Infof("Setting state ONLINE")
	err = mcpd.SetState(mcp.ONLINE)
	if err != nil {
		slog.Fatalf("Failed to set ONLINE: %+v", err)
	}

	exitSig := make(chan os.Signal, 2)
	signal.Notify(exitSig, syscall.SIGINT, syscall.SIGTERM)
	s := <-exitSig
	slog.Infof("Signal (%v) received, waiting for tasks to drain", s)

	daemonStop()
}

func cmdStart() {
	var wg sync.WaitGroup
	ctx := context.Background()

	conn, err := grpcInit()
	if err != nil {
		slog.Fatalf("grpc init failed: %v", err)
	}
	defer conn.Close()
	slog.Debugf("RPC client connected")

	if len(flag.Args()) == 0 {
		slog.Fatalf("Service name required.")
	}
	stop := make(chan bool)
	svc := flag.Args()[0]
	switch svc {
	case "hello":
		c := rpcClient{
			ctx:    ctx,
			client: cloud_rpc.NewConfigBackEndClient(conn),
		}
		err = c.hello()

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
		slog.Fatalf("Unrecognized command %s", svc)
	}
	if err != nil {
		slog.Errorf("%s failed: %s", svc, err)
	}
}

func main() {
	pname = filepath.Base(os.Args[0])

	flag.Parse()
	slog = aputil.NewLogger(pname)
	// Redirect grpc internal log messages to zap, at DEBUG
	glogger := slog.Desugar().WithOptions(
		// zapgrpc adds extra frames, which need to be skipped
		zap.AddCallerSkip(3),
	)
	grpclog.SetLogger(zapgrpc.NewLogger(glogger, zapgrpc.WithDebug()))

	if pname == "ap.rpcd" {
		daemonStart()
	} else if pname == "ap-rpc" {
		cmdStart()
	} else {
		slog.Fatalf("Couldn't determine program mode")
	}
}
