//
// COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//
// appliance shared constant definitions, Go

package base_def

const (
	ZERO_UUID = "00000000-0000-0000-0000-000000000000"

	APPLIANCE_ZMQ_URL = "tcp://127.0.0.1"

	BROKER_ZMQ_PUB_URL = APPLIANCE_ZMQ_URL + ":3131"
	BROKER_ZMQ_SUB_URL = APPLIANCE_ZMQ_URL + ":3132"

	CONFIGD_ZMQ_REP_URL = APPLIANCE_ZMQ_URL + ":3140"

	MCP_ZMQ_REP_URL = APPLIANCE_ZMQ_URL + ":5150"

	TOPIC_PING      = "sys.ping"
	TOPIC_MCP       = "sys.mcp"
	TOPIC_CONFIG    = "sys.config"
	TOPIC_ENTITY    = "net.entity"
	TOPIC_REQUEST   = "net.request"
	TOPIC_RESOURCE  = "net.resource"
	TOPIC_SCAN      = "net.scan"
	TOPIC_SCAN_SSDP = "net.scan.ssdp"
	TOPIC_EXCEPTION = "net.exception"

	BROKER_PROMETHEUS_PORT     = ":3200"
	HTTPD_PROMETHEUS_PORT      = ":3201"
	LOGD_PROMETHEUS_PORT       = ":3202"
	DNSD_PROMETHEUS_PORT       = ":3203"
	DHCPD_PROMETHEUS_PORT      = ":3204"
	HOSTAPDM_PROMETHEUS_PORT   = ":3205"
	FILTERD_PROMETHEUS_PORT    = ":3206"
	CONFIGD_PROMETHEUS_PORT    = ":3207"
	SCAND_PROMETHEUS_PORT      = ":3208"
	SCAND_SSDP_PROMETHEUS_PORT = ":3209"
)
