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
	"bg/ap_common/platform"
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
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/golang/protobuf/ptypes/timestamp"
	"google.golang.org/grpc/grpclog"
)

var (
	connectHost   = flag.String("host", "", "Override cl.rpcd address")
	connectPort   = flag.Int("port", 0, "Override cl.rpcd port")
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
	mcpd          *mcp.MCP

	plat         *platform.Platform
	nodeUUID     string
	nodeBootTime time.Time
	rpcSuccess   *bool

	metrics struct {
		events prometheus.Counter
	}

	cleanup struct {
		chans []chan bool
		wg    sync.WaitGroup
	}
)

func rpcHealthUpdate(ok bool) {
	var connection string

	if config == nil || (rpcSuccess != nil && *rpcSuccess == ok) {
		return
	}
	rpcSuccess = &ok

	if ok {
		connection = "success"
	} else {
		connection = "fail"
	}

	prop := "@/metrics/health/" + nodeUUID + "/cloud_rpc/" + connection
	now := time.Now().Format(time.RFC3339)
	config.CreateProp(prop, now, nil)
}

func publishEventSerialized(ctx context.Context, tclient cloud_rpc.EventClient,
	name, subtopic string, serialized []byte) error {

	slog.Debugw("Sending "+subtopic, "type", name)

	ctx, err := applianceCred.MakeGRPCContext(ctx)
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
	rpcHealthUpdate(err == nil)
	if err != nil {
		return errors.Wrapf(err, "Failed to Put() Event")
	}
	slog.Debugf("Sent: %s  size %d  response: %v", subtopic,
		len(serialized), response)
	if response.Result == cloud_rpc.PutEventResponse_BAD_ENDPOINT {
		// XXX - once we support multiple cl.rpcd stanzas under
		// @/cloud/svc_rpc, this should be used to select a new default
		// index in that list.  Our response should be to store the new
		// index in the config tree and restart ap.rpcd to effect the
		// changeover.
		slog.Infof("Got unsupported BAD_ENDPOINT from cl.rpcd")
	}

	return nil
}

func publishEvent(ctx context.Context, tclient cloud_rpc.EventClient, subtopic string, evt proto.Message) error {
	name := proto.MessageName(evt)
	serialized, err := proto.Marshal(evt)
	if err == nil {
		err = publishEventSerialized(ctx, tclient, name, subtopic, serialized)
	}
	return err
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

	if exception.VirtualAP != nil {
		cloudNetExc.VirtualAP = *exception.VirtualAP
	}

	return publishEvent(ctx, tclient, "exception", cloudNetExc)
}

func testNetException(ctx context.Context, tclient cloud_rpc.EventClient) error {
	exc := &cloud_rpc.NetException{
		Timestamp:   ptypes.TimestampNow(),
		Reason:      "TEST_EXCEPTION",
		Message:     "This is a test of the emergency broadcast system.",
		MacAddress:  0x112233445566,
		Details:     []string{"detail 1", "detail 2"},
		Ipv4Address: 0xaabbccdd,
		Protocol:    "IP",
		VirtualAP:   "psk",
	}

	return publishEvent(ctx, tclient, "exception", exc)
}

func healthConfig(prop, val string) {
	path := "@/metrics/health/" + nodeUUID + "/" + prop
	err := config.CreateProp(path, val, nil)
	if err != nil {
		slog.Warnf("setting %s to %s: %v", path, val, err)
	}
}

func healthLoop(ctx context.Context, tclient cloud_rpc.EventClient,
	wg *sync.WaitGroup, doneChan chan bool) {

	var done bool

	healthConfig("boot_time", nodeBootTime.Format(time.RFC3339))
	if aputil.IsSatelliteMode() {
		healthConfig("role", "satellite")
	} else {
		healthConfig("role", "gateway")
	}

	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()
	for !done {
		healthConfig("alive", time.Now().Format(time.RFC3339))
		select {
		case done = <-doneChan:
		case <-ticker.C:
		}
	}
	slog.Infof("health loop done")
	wg.Done()
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

func commonInit() error {
	var err error

	applianceCred, err = aputil.SystemCredential()
	if err != nil {
		slog.Errorf("loading appliance credentials: %v", err)
	}

	if brokerd != nil {
		config, err = apcfg.NewConfigd(brokerd, pname,
			cfgapi.AccessInternal)
		if err != nil {
			return fmt.Errorf("connecting to configd: %v", err)
		}
		go apcfg.HealthMonitor(config, mcpd)
	}
	return nil
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

func cloud(isFailsafe bool, wg *sync.WaitGroup, doneChan chan bool) {
	var cclient cloud_rpc.ConfigBackEndClient
	var sclient cloud_rpc.CloudStorageClient

	isGateway := !aputil.IsSatelliteMode()
	ctx := context.Background()

	conn := grpcConnect(ctx)
	defer conn.Close()

	tclient := cloud_rpc.NewEventClient(conn)
	uclient := cloud_rpc.NewReleaseManagerClient(conn)
	if isGateway {
		cclient = cloud_rpc.NewConfigBackEndClient(conn)
		sclient = cloud_rpc.NewCloudStorageClient(conn)
	}
	slog.Debugf("RPC client connected")

	brokerd.Handle(base_def.TOPIC_EXCEPTION, func(event []byte) {
		cleanup.wg.Add(1)
		defer cleanup.wg.Done()
		if err := handleNetException(ctx, tclient, event); err != nil {
			slog.Errorf("Failed handleNetException: %s", err)
		}
	})

	go upgradeLoop(ctx, uclient, tclient, &cleanup.wg, addDoneChan())
	go heartbeatLoop(ctx, tclient, &cleanup.wg, addDoneChan())
	if isGateway {
		if isFailsafe {
			slog.Infof("Starting in failsafe mode - disabling " +
				"inventory, fault, update, upload, and cert loops")
		} else {
			go inventoryLoop(ctx, tclient, &cleanup.wg, addDoneChan())
			go faultLoop(ctx, tclient, &cleanup.wg, addDoneChan())
			go updateLoop(&cleanup.wg, addDoneChan())
			go uploadLoop(sclient, &cleanup.wg, addDoneChan())
			go cloudCertLoop(ctx, conn, &cleanup.wg, addDoneChan())
		}

		go configLoop(ctx, cclient, &cleanup.wg, addDoneChan())

		// XXX - should allow tunneling into satellites.
		go tunnelLoop(&cleanup.wg, addDoneChan())
	} else {
		if isFailsafe {
			slog.Infof("Starting in failsafe mode - disabling " +
				"fault loop")
		} else {
			go faultLoop(ctx, tclient, &cleanup.wg, addDoneChan())
		}
	}

	<-doneChan
	wg.Done()
}

func daemonStart() {
	var err error
	var mcpState int

	slog.Infof("ap.rpcd starting")
	isFailsafe := strings.EqualFold(os.Getenv("BG_FAILSAFE"), "true")

	if mcpd, err = mcp.New(pname); err != nil {
		slog.Fatalf("Failed to connect to mcp: %s", err)
	}

	prometheusInit()
	brokerd, err = broker.NewBroker(slog, pname)
	if err != nil {
		slog.Fatal(err)
	}
	defer brokerd.Fini()

	if err := commonInit(); err != nil {
		mcpd.SetState(mcp.BROKEN)
		slog.Fatalf("commonInit failed: %v", err)
	}
	aputil.ReportInit(slog, pname)
	go healthLoop(nil, nil, &cleanup.wg, addDoneChan())

	if applianceCred == nil {
		// We can't perform most of our rpc duties, but we at least want
		// the local cert and fault reaping stuff to work.
		isFailsafe = true
		go faultLoop(nil, nil, &cleanup.wg, addDoneChan())
	} else {
		go cloud(isFailsafe, &cleanup.wg, addDoneChan())
	}

	if isFailsafe {
		mcpState = mcp.FAILSAFE
	} else {
		mcpState = mcp.ONLINE
	}

	stateName := mcp.States[mcpState]
	slog.Infof("Setting state %s", stateName)
	err = mcpd.SetState(mcpState)
	if err != nil {
		slog.Fatalf("Failed to set %s: %v", stateName, err)
	}

	setCertExpirationHandler()
	slog.Info("Provisionally generating self-signed certificate")
	genNo := "0"
	err = config.CreateProp("@/cert_generation", genNo, nil)
	if err != nil {
		slog.Fatalf("Couldn't set initial certificate generation number: %v", err)
	}
	go ssCertGen(genNo)

	exitSig := make(chan os.Signal, 2)
	signal.Notify(exitSig, syscall.SIGINT, syscall.SIGTERM)
	s := <-exitSig
	slog.Infof("Signal (%v) received, waiting for tasks to drain", s)

	daemonStop()
}

func cmdStart() {
	var wg sync.WaitGroup
	ctx := context.Background()

	// No brokerd means no connection to configd; it's not fatal, but cloud
	// RPC config will have to be done on the commandline.
	brokerd, _ = broker.NewBroker(slog, pname)
	if brokerd != nil {
		defer brokerd.Fini()
	}

	err := commonInit()
	if err != nil || applianceCred == nil {
		slog.Fatalf("grpc init failed: %v", err)
	}

	conn := grpcConnect(ctx)
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
	case "faults":
		tclient := cloud_rpc.NewEventClient(conn)
		err = sendFaults(ctx, tclient)
	case "net-exception":
		tclient := cloud_rpc.NewEventClient(conn)
		err = testNetException(ctx, tclient)
	case "release":
		tclient := cloud_rpc.NewReleaseManagerClient(conn)
		var resp *cloud_rpc.ReleaseResponse
		resp, err = fetchReleaseDescriptor(ctx, tclient)
		if err != nil {
			break
		}
		switch flag.Args()[1] {
		case "show":
			fmt.Println(indentReleaseJSON(resp.Release))
		case "fetch":
			rel, err := unmarshalRelease(resp.Release)
			if err != nil {
				break
			}
			err = fetchArtifacts(ctx, rel, rel.Release.UUID.String())
		}
	case "storageurl":
		tclient := cloud_rpc.NewCloudStorageClient(conn)
		var urls []*cloud_rpc.SignedURL
		urls, err = generateSignedURLs(tclient,
			&uploadType{"/test", "test", "application/json"},
			[]string{"test1", "test2"})
		if err != nil {
			break
		}
		slog.Infow("Response", "urls", urls)
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

func init() {
	plat = platform.NewPlatform()
	nodeUUID, _ = plat.GetNodeID()
	nodeBootTime = aputil.LinuxBootTime()
}
