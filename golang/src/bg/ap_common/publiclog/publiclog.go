//
// COPYRIGHT 2020 Brightgate Inc. All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or  alteration will be a violation of federal law.
//

package publiclog

import (
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
)

const (
	// CefVersion represents the Common Event Format version with
	// which our messages comply.
	CefVersion = "CEF:0"
	// CefDeviceVendor is the vendor string we use within CEF
	// messages.
	CefDeviceVendor = "Brightgate"
)

func sendPublicLog(brokerd *broker.Broker, msg *base_msg.EventNetPublicLog) error {
	topic := base_def.TOPIC_PUBLIC_LOG

	if *msg.EventClassId == "" {
		panic("empty event class ID in public log message")
	}

	msg.Timestamp = aputil.NowToProtobuf()
	msg.Sender = proto.String(brokerd.Name)
	msg.Debug = proto.String("-")

	err := brokerd.Publish(msg, topic)
	if err != nil {
		return errors.Wrapf(err, "couldn't publish %s (%v)", topic, msg)
	}

	return nil
}

// SendLogTestMessage submits a test message to the public logging
// subsystem.
func SendLogTestMessage(brokerd *broker.Broker, msg string) error {
	l := base_msg.EventNetPublicLog{}

	l.EventClassId = proto.String(base_def.CEF_TEST)
	l.CefReason = proto.String("logging test message")
	l.CefMsg = proto.String(msg)

	return sendPublicLog(brokerd, &l)
}

// SendLogVulnDetected submits a message reporting that a vulnerability
// has been detected on the specified device to the public logging
// subsystem.
func SendLogVulnDetected(brokerd *broker.Broker, mac string, ipv4 string) error {
	l := base_msg.EventNetPublicLog{}

	l.EventClassId = proto.String(base_def.CEF_VULN_DETECTED)
	l.CefReason = proto.String("device vulnerability detected")
	l.CefSmac = proto.String(mac)
	l.CefSrc = proto.String(ipv4)

	return sendPublicLog(brokerd, &l)
}

// SendLogDeviceQuarantine submits a message reporting that a device has
// been placed in the quarantine trust group to the public logging
// subsystem.
func SendLogDeviceQuarantine(brokerd *broker.Broker, mac string) error {
	l := base_msg.EventNetPublicLog{}

	l.EventClassId = proto.String(base_def.CEF_DEVICE_QUARANTINE)
	l.CefReason = proto.String("device placed in quarantine")
	l.CefSmac = proto.String(mac)

	return sendPublicLog(brokerd, &l)
}

// SendLogDeviceUnenrolled submits a message reporting that a device has
// been placed in the unenrolled trust group to the public logging
// subsystem.
func SendLogDeviceUnenrolled(brokerd *broker.Broker, mac string) error {
	l := base_msg.EventNetPublicLog{}

	l.EventClassId = proto.String(base_def.CEF_DEVICE_UNENROLLED)
	l.CefReason = proto.String("device placed in unenrolled")
	l.CefSmac = proto.String(mac)

	return sendPublicLog(brokerd, &l)
}

// SendLogLoginEAPSuccess submits a message reporting successful user
// authentication to a Wi-Fi network via EAP to the public logging
// subsystem.
func SendLogLoginEAPSuccess(brokerd *broker.Broker, mac string, username string) error {
	l := base_msg.EventNetPublicLog{}

	l.EventClassId = proto.String(base_def.CEF_LOGIN_EAP_SUCCESS)
	l.CefReason = proto.String("successful EAP login")
	l.CefSmac = proto.String(mac)
	l.CefSuser = proto.String(username)

	return sendPublicLog(brokerd, &l)
}

// SendLogLoginRepeatedFailure submits a message reporting that a client
// has attempted repeated login failures to the public logging
// subsystem.
func SendLogLoginRepeatedFailure(brokerd *broker.Broker, mac string, username string) error {
	l := base_msg.EventNetPublicLog{}

	l.EventClassId = proto.String(base_def.CEF_LOGIN_FAILURE)
	l.CefReason = proto.String("repeated login failure")
	l.CefSmac = proto.String(mac)
	l.CefSuser = proto.String(username)

	return sendPublicLog(brokerd, &l)
}
