//
// COPYRIGHT 2020 Brightgate Inc. All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/publiclog"
	"bg/base_msg"

	syslog "github.com/schahn/srslog"
)

// Receiver represents a destination for CEF-formatted messages.
// `syslog` is initially the only supported protocol.
type Receiver struct {
	// @/log/%receiver%/protocol": "", "syslog"
	protocol string
	// @/log/%receiver%/syslog_host"
	syslogHost string
	// "@/log/%receiver%/syslog_port"
	syslogPort string
	// @/log/%receiver%/syslog_protocol": "udp", "tcp"
	syslogProtocol string
}

const (
	defaultSyslogPort    = 514
	defaultTLSSyslogPort = 6514
)

var (
	receivers map[string]Receiver
)

// fmtNVPair trims the "Cef" prefix from the struct field name and
// renders that name and the value as a "k=v" string.
func fmtNVPair(name string, value reflect.Value) string {
	fieldName := strings.ToLower(name[3:])

	return fmt.Sprintf("%s=%v", fieldName, value)
}

// fmtCefPublicLog returns the given public log entry as a CEF-formatted
// string.
func fmtCefPublicLog(plog *base_msg.EventNetPublicLog) string {
	components := make([]string, 0)
	extension := make([]string, 0)

	components = append(components, publiclog.CefVersion)
	components = append(components, publiclog.CefDeviceVendor)
	components = append(components, plat.CefDeviceProduct)

	if *plog.EventClassId == "" {
		panic("empty event class ID in public log message")
	}

	components = append(components, *plog.EventClassId)

	plogt := reflect.ValueOf(*plog)
	plogi := reflect.Indirect(plogt).Type()

	for i := 0; i < plogi.NumField(); i++ {
		f := plogi.Field(i)
		if !strings.HasPrefix(f.Name, "Cef") {
			continue
		}

		v := plogt.Field(i)
		if v.Kind() == reflect.Ptr {
			if !v.IsNil() {
				ev := v.Elem()
				extension = append(extension, fmtNVPair(f.Name, ev))
			}
		}
	}

	components = append(components, strings.Join(extension, " "))

	return strings.Join(components, "|")
}

func sendLogToSyslog(msg string) {
	for k, r := range receivers {
		if r.protocol != "syslog" {
			// At the moment, we only support syslog as an
			// external logging protocol.
			continue
		}

		// Use the local resolver (the appliance) so
		// that we can use hostnames within the domain.
		addrs, err := aputil.LocalResolver.LookupHost(
			context.Background(), r.syslogHost)
		if err != nil {
			// Use the default resolver.
			addrs, err = net.LookupHost(r.syslogHost)
			if err != nil {
				slog.Infof("unable to resolve host %s (%d); skipping",
					r.syslogHost, k)
				continue
			}
		}
		host := addrs[0]

		port, err := strconv.Atoi(r.syslogPort)
		if err != nil {
			slog.Infof("illegal port value; using 0")
			port = 0
		}

		if port == 0 {
			port = defaultSyslogPort
		}

		syslogDest := fmt.Sprintf("%s:%d", host, port)
		wu, err := syslog.Dial(r.syslogProtocol, syslogDest, syslog.LOG_AUTH,
			"brightgate")

		if err != nil {
			slog.Infof("%s dial to receiver '%s' failed: %v", r.syslogProtocol, k, err)
		}

		wu.Notice(msg)

		wu.Close()
	}
}

func iterateReceivers() map[string]Receiver {
	nr := make(map[string]Receiver)

	receiverProps, err := config.GetProps("@/log")
	if err != nil {
		// If the @/log property subtree is not defined, then an empty
		// receivers map is fine.
		return nr
	}

	for name, receiver := range receiverProps.Children {
		r := Receiver{
			protocol:       "syslog",
			syslogHost:     "",
			syslogPort:     "",
			syslogProtocol: "udp",
		}

		for pname, pvalue := range receiver.Children {
			switch pname {
			case "protocol":
				// Valid values are "syslog".
				r.protocol = pvalue.Value
			case "syslog_protocol":
				// Valid values are "udp", "tcp".
				r.syslogProtocol = pvalue.Value
			case "syslog_host":
				r.syslogHost = pvalue.Value
			case "syslog_port":
				r.syslogPort = pvalue.Value
			default:
				slog.Warnf("unexpected receiver property '%s' = '%+v'", pname, pvalue)
			}
		}

		nr[name] = r
	}

	return nr
}

func updateReceivers() {
	receivers = iterateReceivers()
}

func configLogSettingsChanged(path []string, val string, expires *time.Time) {
	slog.Infof("change to @/log/")

	updateReceivers()
}

func cmdStart() {
	var err error

	// No brokerd means no ability to post messages; it's fatal for
	// this command.
	brokerd, err = broker.NewBroker(slog, pname)
	if err != nil {
		slog.Fatalf("broker connection unavailable: %v", err)
	}

	if brokerd != nil {
		defer brokerd.Fini()
	} else {
		slog.Fatalf("nil broker!?")
	}

	msg := "ap-publiclog test message"
	if len(flag.Args()) > 0 {
		msg = flag.Args()[0]
	}
	slog.Infof("Using test message '%s'.", msg)

	// Compose and post a message.
	err = publiclog.SendLogTestMessage(brokerd, msg)
	if err != nil {
		slog.Fatalf("log test message failed: %v", err)
	}

	// Needed even with zero queue depth.
	time.Sleep(time.Second)
}
