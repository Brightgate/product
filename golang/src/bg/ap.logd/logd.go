/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

/*
 * message logger
 */

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/ap_common/platform"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/network"

	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

var (
	logDir  string
	logFile *os.File
	slog    *zap.SugaredLogger
	mcpd    *mcp.MCP

	metrics struct {
		pingEvents      prometheus.Counter
		configEvents    prometheus.Counter
		entityEvents    prometheus.Counter
		errorEvents     prometheus.Counter
		exceptionEvents prometheus.Counter
		requestEvents   prometheus.Counter
		resourceEvents  prometheus.Counter
		identityEvents  prometheus.Counter
	}
)

const pname = "ap.logd"

func handlePing(event []byte) {
	ping := &base_msg.EventPing{}
	proto.Unmarshal(event, ping)
	log.Printf("[sys.ping] %v", ping)
	metrics.pingEvents.Inc()
}

func handleConfig(event []byte) {
	config := &base_msg.EventConfig{}
	proto.Unmarshal(event, config)
	log.Printf("[sys.config] %v", config)
	metrics.configEvents.Inc()
}

func handleEntity(event []byte) {
	entity := &base_msg.EventNetEntity{}
	proto.Unmarshal(event, entity)
	log.Printf("[net.entity] %v", entity)
	metrics.entityEvents.Inc()
}

func handleError(event []byte) {
	syserror := &base_msg.EventSysError{}
	proto.Unmarshal(event, syserror)
	log.Printf("[sys.error] %v", syserror)
	metrics.errorEvents.Inc()
}

func extendMsg(msg *string, field, value string) {
	new := field + ": " + value
	if len(*msg) > 0 {
		*msg += ", "
	}
	*msg += new
}

func handleException(event []byte) {
	exception := &base_msg.EventNetException{}
	proto.Unmarshal(event, exception)
	log.Printf("[net.exception] %v", exception)
	metrics.exceptionEvents.Inc()

	// Construct a user-friendly message to push to the system log
	time := aputil.ProtobufToTime(exception.Timestamp)
	timestamp := time.Format("2006/01/02 15:04:05")

	msg := ""
	if exception.Sender != nil {
		extendMsg(&msg, "Sender", *exception.Sender)
	}

	if exception.Protocol != nil {
		protocols := base_msg.Protocol_name
		num := int32(*exception.Protocol)
		extendMsg(&msg, "Protocol", protocols[num])
	}

	if exception.Reason != nil {
		reasons := base_msg.EventNetException_Reason_name
		num := int32(*exception.Reason)
		extendMsg(&msg, "Reason", reasons[num])
	}

	if exception.MacAddress != nil {
		mac := network.Uint64ToHWAddr(*exception.MacAddress)
		extendMsg(&msg, "hwaddr", mac.String())
	}

	if exception.Ipv4Address != nil {
		ip := network.Uint32ToIPAddr(*exception.Ipv4Address)
		extendMsg(&msg, "IP", ip.String())
	}

	if len(exception.Details) > 0 {
		details := "[" + strings.Join(exception.Details, ",") + "]"
		extendMsg(&msg, "Details", details)
	}

	if exception.Message != nil {
		extendMsg(&msg, "Message", *exception.Message)
	}

	fmt.Printf("%s Handled exception event: %s\n", timestamp, msg)
}

func handleResource(event []byte) {
	resource := &base_msg.EventNetResource{}
	proto.Unmarshal(event, resource)
	log.Printf("[net.resource] %v", resource)
	metrics.resourceEvents.Inc()
}

func handleRequest(event []byte) {
	request := &base_msg.EventNetRequest{}
	proto.Unmarshal(event, request)
	log.Printf("[net.request] %v", request)
	metrics.requestEvents.Inc()
}

func handleIdentity(event []byte) {
	identity := &base_msg.EventNetIdentity{}
	proto.Unmarshal(event, identity)
	log.Printf("[net.identity] %v", identity)
	metrics.identityEvents.Inc()
}

func openLog(path string) (*os.File, error) {
	fp, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("couldn't get absolute path: %v", err)
	}

	if err = os.MkdirAll(fp, 0755); err != nil {
		return nil, fmt.Errorf("failed to make path: %v", err)
	}

	logfile := fp + "/events.log"
	file, err := os.OpenFile(logfile, os.O_CREATE|os.O_APPEND|os.O_WRONLY,
		0600)
	if err != nil {
		return nil, fmt.Errorf("error opening log file: %v", err)
	}
	return file, nil
}

func reopenLogfile() error {
	newLog, err := openLog(logDir)
	if err != nil {
		return err
	}
	log.SetOutput(newLog)
	if logFile != nil {
		logFile.Close()
	}
	logFile = newLog
	return nil
}

func prometheusInit() {
	metrics.pingEvents = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "logd_ping_events",
		Help: "Number of Ping events logged.",
	})
	metrics.configEvents = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "logd_config_events",
		Help: "Number of Config events logged.",
	})
	metrics.entityEvents = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "logd_entity_events",
		Help: "Number of NetEntity events logged.",
	})
	metrics.errorEvents = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "logd_error_events",
		Help: "Number of SysError events logged.",
	})
	metrics.exceptionEvents = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "logd_exception_events",
		Help: "Number of NetException events logged.",
	})
	metrics.requestEvents = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "logd_request_events",
		Help: "Number of NetRequest events logged.",
	})
	metrics.resourceEvents = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "logd_resource_events",
		Help: "Number of NetResource events logged.",
	})
	metrics.identityEvents = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "logd_identity_events",
		Help: "Number of NetIdentity events logged.",
	})
	prometheus.MustRegister(metrics.pingEvents)
	prometheus.MustRegister(metrics.configEvents)
	prometheus.MustRegister(metrics.entityEvents)
	prometheus.MustRegister(metrics.errorEvents)
	prometheus.MustRegister(metrics.exceptionEvents)
	prometheus.MustRegister(metrics.requestEvents)
	prometheus.MustRegister(metrics.resourceEvents)
	prometheus.MustRegister(metrics.identityEvents)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.LOGD_DIAG_PORT, nil)
}

// send a single message to both the MCP log and the logd-specific log
func dualLog(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Printf("%s\n", msg)
	slog.Infof("%s", msg)
}

func main() {
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	plat := platform.NewPlatform()
	logDir = plat.ExpandDirPath("__APDATA__", "logd")

	flag.Parse()

	slog = aputil.NewLogger(pname)
	defer slog.Sync()

	if mcpd, err = mcp.New(pname); err != nil {
		slog.Warnf("Failed to connect to mcp")
	}

	if err = reopenLogfile(); err != nil {
		slog.Errorf("Failed to setup logging: %s\n", err)
		os.Exit(1)
	}

	prometheusInit()

	b := broker.NewBroker(slog, pname)
	b.Handle(base_def.TOPIC_PING, handlePing)
	b.Handle(base_def.TOPIC_CONFIG, handleConfig)
	b.Handle(base_def.TOPIC_ENTITY, handleEntity)
	b.Handle(base_def.TOPIC_ERROR, handleError)
	b.Handle(base_def.TOPIC_EXCEPTION, handleException)
	b.Handle(base_def.TOPIC_RESOURCE, handleResource)
	b.Handle(base_def.TOPIC_REQUEST, handleRequest)
	b.Handle(base_def.TOPIC_IDENTITY, handleIdentity)
	defer b.Fini()

	mcpd.SetState(mcp.ONLINE)

	kernelMonitorStart()

	sig := make(chan os.Signal, 3)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	for done := false; !done; {
		switch s := <-sig; s {
		case syscall.SIGHUP:
			dualLog("Signal (%v) received, reopening logs.", s)
			err = reopenLogfile()
			if err != nil {
				dualLog("Exiting.  Fatal error reopening log: %s", err)
				done = true
			}
		default:
			dualLog("Signal (%v) received, stopping", s)
			done = true
		}
	}

	kernelMonitorStop()
	slog.Infof("stopping")
}
