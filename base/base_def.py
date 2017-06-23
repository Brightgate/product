#!/usr/bin/python
#
# COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

"""appliance shared constant definitions, Python 3"""

ZERO_UUID = "00000000-0000-0000-0000-000000000000"

APPLIANCE_ZMQ_URL = "tcp://127.0.0.1"

BROKER_ZMQ_PUB_URL = APPLIANCE_ZMQ_URL + ":3131"
BROKER_ZMQ_SUB_URL = APPLIANCE_ZMQ_URL + ":3132"

CONFIGD_ZMQ_REP_URL = APPLIANCE_ZMQ_URL + ":3140"

MCP_ZMQ_REP_URL = APPLIANCE_ZMQ_URL + ":5150"

TOPIC_PING = b"sys.ping"
TOPIC_MCP = b"sys.mcp"
TOPIC_CONFIG = b"sys.config"
TOPIC_ENTITY = b"net.entity"
TOPIC_REQUEST = b"net.request"
TOPIC_RESOURCE = b"net.resource"
TOPIC_EXCEPTION = b"net.exception"

BROKER_PROMETHEUS_PORT = 3200
HTTPD_PROMETHEUS_PORT = 3201
LOGD_PROMETHEUS_PORT = 3202
DNSD_PROMETHEUS_PORT = 3203
DHCPD_PROMETHEUS_PORT = 3204
