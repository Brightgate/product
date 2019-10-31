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
	"regexp"
	"strings"
	"syscall"
	"time"

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

func extendMsg(msg *string, field string, value *string, def string) {
	var v, space string

	if len(*msg) > 0 {
		space = " "
	}

	if value != nil && *value != "" {
		v = *value
	} else if def != "" {
		v = def
	} else {
		return
	}

	*msg += space + field + ": " + v
}

func tstring(pb *base_msg.Timestamp) string {
	var rval string

	if t := aputil.ProtobufToTime(pb); t != nil {
		rval = t.Format(time.RFC3339)
	} else {
		rval = "missing timestamp"
	}
	return rval
}

func handlePing(event []byte) {
	var msg string

	ping := &base_msg.EventPing{}
	proto.Unmarshal(event, ping)

	extendMsg(&msg, "from", ping.Sender, "?")
	log.Printf("%s [sys.ping]\t%s", tstring(ping.Timestamp), msg)
	metrics.pingEvents.Inc()
}

func handleConfig(event []byte) {
	var msg string

	config := &base_msg.EventConfig{}
	proto.Unmarshal(event, config)

	extendMsg(&msg, "from", config.Sender, "?")
	t := "missing"
	if config.Type != nil {
		switch *config.Type {
		case base_msg.EventConfig_CHANGE:
			t = "CHANGE"
		case base_msg.EventConfig_DELETE:
			t = "DELETE"
		case base_msg.EventConfig_EXPIRE:
			t = "EXPIRE"
		default:
			t = "invalid"
		}
	}
	extendMsg(&msg, "type", &t, "")
	extendMsg(&msg, "prop", config.Property, "")
	extendMsg(&msg, "val", config.NewValue, "")

	log.Printf("%s [sys.config]\t%s", tstring(config.Timestamp), msg)
	metrics.configEvents.Inc()
}

func handleEntity(event []byte) {
	var msg string

	entity := &base_msg.EventNetEntity{}
	proto.Unmarshal(event, entity)

	extendMsg(&msg, "from", entity.Sender, "?")

	if entity.MacAddress != nil {
		m := network.Uint64ToHWAddr(*entity.MacAddress)
		msg += " mac: " + m.String()
	}
	if entity.Ipv4Address != nil {
		i := network.Uint32ToIPAddr(*entity.Ipv4Address)
		msg += " ipv4: " + i.String()
	}
	extendMsg(&msg, "ring", entity.Ring, "")
	extendMsg(&msg, "hostname", entity.Hostname, "")
	extendMsg(&msg, "node", entity.Node, "")
	extendMsg(&msg, "band", entity.Band, "")
	extendMsg(&msg, "vap", entity.VirtualAP, "")
	extendMsg(&msg, "wifi_sig", entity.WifiSignature, "")
	if entity.Disconnect != nil {
		msg += fmt.Sprintf(" disconnect: %v", *entity.Disconnect)
	}
	extendMsg(&msg, "username", entity.Username, "")

	log.Printf("%s [net.entity]\t%s", tstring(entity.Timestamp), msg)
	metrics.entityEvents.Inc()
}

func handleError(event []byte) {
	var msg string

	syserror := &base_msg.EventSysError{}
	proto.Unmarshal(event, syserror)
	extendMsg(&msg, "from", syserror.Sender, "?")
	reason := "missing"
	if syserror.Reason != nil {
		switch *syserror.Reason {
		case base_msg.EventSysError_RENEWED_SSL_CERTIFICATE:
			reason = "RENEWED_SSL_CERTIFICATE"
		case base_msg.EventSysError_DAEMON_CRASH_REQUESTED:
			reason = "DAEMON_CRASH_REQUESTED"
		default:
			reason = "unknown"
		}
	}
	extendMsg(&msg, "reason", &reason, "")
	extendMsg(&msg, "message", syserror.Message, "")

	log.Printf("%s [sys.error]\t%s", tstring(syserror.Timestamp), msg)

	metrics.errorEvents.Inc()
}

func handleException(event []byte) {
	var msg string

	exception := &base_msg.EventNetException{}
	proto.Unmarshal(event, exception)
	metrics.exceptionEvents.Inc()

	extendMsg(&msg, "Sender", exception.Sender, "?")
	if exception.Protocol != nil {
		protocols := base_msg.Protocol_name
		num := int32(*exception.Protocol)
		msg += " protocol: " + protocols[num]
	}

	if exception.Reason != nil {
		reasons := base_msg.EventNetException_Reason_name
		num := int32(*exception.Reason)
		msg += " reason: " + reasons[num]
	}

	if exception.MacAddress != nil {
		mac := network.Uint64ToHWAddr(*exception.MacAddress)
		msg += " hwaddr: " + mac.String()
	}

	if exception.Ipv4Address != nil {
		ip := network.Uint32ToIPAddr(*exception.Ipv4Address)
		msg += " ipv4: " + ip.String()
	}

	if len(exception.Details) > 0 {
		msg += " details: [" +
			strings.Join(exception.Details, ",") + "]"
	}

	extendMsg(&msg, "message", exception.Message, "")

	log.Printf("%s [net.exception]\t%s", tstring(exception.Timestamp), msg)
}

func handleResource(event []byte) {
	var msg string

	resource := &base_msg.EventNetResource{}
	proto.Unmarshal(event, resource)

	extendMsg(&msg, "from", resource.Sender, "?")
	a := "missing"
	if resource.Action != nil {
		switch *resource.Action {
		case base_msg.EventNetResource_RELEASED:
			a = "RELEASED"
		case base_msg.EventNetResource_PROVISIONED:
			a = "PROVISIONED"
		case base_msg.EventNetResource_CLAIMED:
			a = "CLAIMED"
		case base_msg.EventNetResource_COLLISION:
			a = "COLLISION"
		default:
			a = "invalid"
		}
	}
	extendMsg(&msg, "action", &a, "")
	if resource.Ipv4Address != nil {
		i := network.Uint32ToIPAddr(*resource.Ipv4Address)
		msg += " ipv4: " + i.String()
	}
	extendMsg(&msg, "hostname", resource.Hostname, "")

	log.Printf("%s [net.resource]\t%s", tstring(resource.Timestamp), msg)
	metrics.resourceEvents.Inc()
}

var (
	// match: <name>\t<class>\t<type>:
	//     www.google.com.	IN	AAAA
	questionRE = regexp.MustCompile(`;(\S+)\s+(\S+)\s+(\S+)`)

	// match: <name>\t<TTL>\t<class>\t<type>\t<value>
	//     rpc0.b10e.net.	179	IN	A	34.83.242.232
	//     ssl.foo.com.	21384	IN	CNAME	ssl-foo.l.google.com.
	answerRE = regexp.MustCompile(`(\S+)\s+(\d+)\s+(\S+)\s+(\S+)`)
)

func parseDNSRequest(requests []string) string {
	var msg string

	for _, r := range requests {
		if f := questionRE.FindStringSubmatch(r); f != nil {
			msg += "(" + strings.Join(f[1:4], ",") + ")"
		} else {
			msg += "(unparseable: " + r + ")"
		}
	}
	return msg
}

func parseDNSResponse(responses []string) string {
	var msg string

	for _, r := range responses {
		var body string

		if f := answerRE.FindStringSubmatch(r); f != nil {
			// <name> <TTL> <A|AAAA|CNAME|etc> <address>
			body = strings.Join(f[1:5], ",")
		} else {
			body = r
		}
		msg += "(" + body + ")"
	}

	return msg
}

func handleRequest(event []byte) {
	var msg, pmsg, protocol string

	request := &base_msg.EventNetRequest{}
	proto.Unmarshal(event, request)
	extendMsg(&msg, "from", request.Sender, "?")
	extendMsg(&msg, "for", request.Requestor, "?")

	if request.Protocol != nil {
		switch *request.Protocol {
		// DNS is currently the only protocol being evented
		case base_msg.Protocol_DNS:
			q := parseDNSRequest(request.Request)
			a := parseDNSResponse(request.Response)
			extendMsg(&pmsg, "q", &q, "")
			extendMsg(&pmsg, "a", &a, "")

		case base_msg.Protocol_DHCP:
			protocol = "DHCP"

		case base_msg.Protocol_IP:
			protocol = "IP"

		default:
			protocol = "invalid"
		}
	}

	extendMsg(&msg, "protocol", &protocol, "")
	if pmsg != "" {
		msg += " " + pmsg
	} else {
		if request.Request != nil {
			for _, r := range request.Request {
				extendMsg(&msg, "request", &r, "")
			}
		}
		if request.Response != nil {
			for _, r := range request.Response {
				extendMsg(&msg, "response", &r, "")
			}
		}
	}

	log.Printf("%s [net.request]\t%s", tstring(request.Timestamp), msg)
	metrics.requestEvents.Inc()
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

	b, err := broker.NewBroker(slog, pname)
	if err != nil {
		slog.Fatal(err)
	}
	b.Handle(base_def.TOPIC_PING, handlePing)
	b.Handle(base_def.TOPIC_CONFIG, handleConfig)
	b.Handle(base_def.TOPIC_ENTITY, handleEntity)
	b.Handle(base_def.TOPIC_ERROR, handleError)
	b.Handle(base_def.TOPIC_EXCEPTION, handleException)
	b.Handle(base_def.TOPIC_RESOURCE, handleResource)
	b.Handle(base_def.TOPIC_REQUEST, handleRequest)
	defer b.Fini()

	mcpd.SetState(mcp.ONLINE)

	kernelMonitorStart()
	aputil.ReportInit(slog, pname)

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
