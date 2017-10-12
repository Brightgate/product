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

"""generate shared constant definitions"""

import datetime
import getopt
import sys

from enum import Enum

class Statement(Enum):
    COMMENT = 0
    FOOTER = 1
    HEADER = 2
    LIST = 3
    MODCOM = 4
    PACKAGE = 5
    SECTION = 6
    SIMPLE_STR = 7
    SIMPLE_NUM = 8
    SIMPLE_PORT = 9

def noop(v):
    return ""

def py3_header(v):
    return ("#!/usr/bin/python\n")

def py3_comment(v):
    if v[0] is None:
        return ("\n")

    return ("# %s\n" % " ".join(v))

def py3_simple_string(v):
    py3symbols.append(v[0])
    return ("%s = b\"%s\"\n" % (v[0], v[1]))

def py3_simple_number(v):
    py3symbols.append(v[0])
    return ("%s = %s\n" % (v[0], v[1]))

def py3_simple_netport(v):
    py3symbols.append(v[0])
    return ("%s = %s\n" % (v[0], v[1]))

def py3_list_assignment(v):
    rhs = ""
    for val in v[1:]:
        if val in ["+"]:
            rhs += " " + val + " "
        elif val.isnumeric():
            rhs += val
        elif val in py3symbols:
            rhs += val
        else:
            # String value case.
            rhs += "\"" + val + "\""

    return ("%s = %s\n" % (v[0], rhs))

py3ops = {
    Statement.COMMENT: py3_comment,
    Statement.FOOTER: noop,
    Statement.HEADER: py3_header,
    Statement.LIST: py3_list_assignment,
    Statement.MODCOM: py3_comment,
    Statement.PACKAGE: noop,
    Statement.SECTION: noop,
    Statement.SIMPLE_STR: py3_simple_string,
    Statement.SIMPLE_NUM: py3_simple_number,
    Statement.SIMPLE_PORT: py3_simple_number,
}

def golang_header(v):
    return ("")

def golang_comment(v):
    if v[0] is None:
        return ("\n")

    return ("// %s\n" % " ".join(v))

def golang_package(v):
    return ("package %s\n" % " ".join(v))

def golang_section(v):
    return ("%s\n" % " ".join(v))

def golang_simple_string(v):
    golangsymbols.append(v[0])
    return ("%s = \"%s\"\n" % (v[0], v[1]))

def golang_simple_number(v):
    golangsymbols.append(v[0])
    return ("%s = %s\n" % (v[0], v[1]))

def golang_simple_netport(v):
    golangsymbols.append(v[0])
    return ("%s = \":%s\"\n" % (v[0], v[1]))

def golang_list_assignment(v):
    rhs = ""
    for val in v[1:]:
        if val in ["+"]:
            rhs += val
        elif val.isnumeric():
            rhs += val
        elif val in golangsymbols:
            rhs += val
        else:
            rhs += "\"" + val + "\""

    return ("%s = %s\n" % (v[0], rhs))

golangops = {
    Statement.COMMENT: golang_comment,
    Statement.FOOTER: noop,
    Statement.HEADER: golang_header,
    Statement.LIST: golang_list_assignment,
    Statement.MODCOM: golang_comment,
    Statement.PACKAGE: golang_package,
    Statement.SECTION: golang_section,
    Statement.SIMPLE_STR: golang_simple_string,
    Statement.SIMPLE_NUM: golang_simple_number,
    Statement.SIMPLE_PORT: golang_simple_netport,
}


assignments = [
    [Statement.HEADER, None],
    [Statement.COMMENT, ""],
    [Statement.COMMENT, "COPYRIGHT %d Brightgate Inc.  All rights reserved." % datetime.date.today().year],
    [Statement.COMMENT, ""],
    [Statement.COMMENT, "This copyright notice is Copyright Management Information under 17 USC 1202"],
    [Statement.COMMENT, "and is included to protect this work and deter copyright infringement."],
    [Statement.COMMENT, "Removal or alteration of this Copyright Management Information without the"],
    [Statement.COMMENT, "express written permission of Brightgate Inc is prohibited, and any"],
    [Statement.COMMENT, "such unauthorized removal or alteration will be a violation of federal law."],
    [Statement.COMMENT, ""],
    [Statement.MODCOM, "Brightgate shared constant definitions"],
    [Statement.COMMENT, ""],
    [Statement.PACKAGE, "base_def"],
    [Statement.SECTION, "const ("],
    [Statement.SIMPLE_STR, "ZERO_UUID", "00000000-0000-0000-0000-000000000000"],

    [Statement.COMMENT, "Appliance definitions"],

    [Statement.COMMENT, "Security rings"],
    [Statement.SIMPLE_STR, "RING_UNENROLLED", "unenrolled"],
    [Statement.SIMPLE_STR, "RING_SETUP", "setup"],
    [Statement.SIMPLE_STR, "RING_CORE", "core"],
    [Statement.SIMPLE_STR, "RING_STANDARD", "standard"],
    [Statement.SIMPLE_STR, "RING_DEVICES", "devices"],
    [Statement.SIMPLE_STR, "RING_GUEST", "guest"],
    [Statement.SIMPLE_STR, "RING_QUARANTINE", "quarantine"],
    [Statement.SIMPLE_STR, "RING_WIRED", "wired"],

    [Statement.COMMENT, "Message bus topics"],
    [Statement.SIMPLE_STR, "TOPIC_PING", "sys.ping"],
    [Statement.SIMPLE_STR, "TOPIC_MCP", "sys.mcp"],
    [Statement.SIMPLE_STR, "TOPIC_CONFIG", "sys.config"],
    [Statement.SIMPLE_STR, "TOPIC_ENTITY", "net.entity"],
    [Statement.SIMPLE_STR, "TOPIC_REQUEST", "net.request"],
    [Statement.SIMPLE_STR, "TOPIC_RESOURCE", "net.resource"],
    [Statement.SIMPLE_STR, "TOPIC_SCAN", "net.scan"],
    [Statement.SIMPLE_STR, "TOPIC_LISTEN", "net.listen"],
    [Statement.SIMPLE_STR, "TOPIC_EXCEPTION", "net.exception"],
    [Statement.SIMPLE_STR, "TOPIC_IDENTITY",  "net.identity"],
    [Statement.SIMPLE_STR, "TOPIC_OPTIONS",  "net.options"],

    [Statement.COMMENT, "Prometheus client HTTP ports"],
    [Statement.SIMPLE_PORT, "BROKER_PROMETHEUS_PORT", 3200],
    [Statement.SIMPLE_PORT, "HTTPD_PROMETHEUS_PORT", 3201],
    [Statement.SIMPLE_PORT, "LOGD_PROMETHEUS_PORT", 3202],
    [Statement.SIMPLE_PORT, "DNSD_PROMETHEUS_PORT", 3203],
    [Statement.SIMPLE_PORT, "DHCPD_PROMETHEUS_PORT", 3204],
    [Statement.SIMPLE_PORT, "HOSTAPDM_PROMETHEUS_PORT", 3205],
    [Statement.SIMPLE_PORT, "CONFIGD_PROMETHEUS_PORT", 3207],
    [Statement.SIMPLE_PORT, "WATCHD_PROMETHEUS_PORT", 3208],
    [Statement.SIMPLE_PORT, "RELAYD_PROMETHEUS_PORT", 3209],

    [Statement.COMMENT, "ZeroMQ definitions"],
    [Statement.SIMPLE_STR, "APPLIANCE_ZMQ_URL", "tcp://127.0.0.1"],
    [Statement.LIST, "BROKER_ZMQ_PUB_URL", "APPLIANCE_ZMQ_URL", "+", ":3131"],
    [Statement.LIST, "BROKER_ZMQ_SUB_URL", "APPLIANCE_ZMQ_URL", "+", ":3132"],
    [Statement.LIST, "CONFIGD_ZMQ_REP_URL", "APPLIANCE_ZMQ_URL", "+", ":3140"],
    [Statement.LIST, "WATCHD_ZMQ_REP_URL", "APPLIANCE_ZMQ_URL", "+", ":3141"],
    [Statement.LIST, "MCP_ZMQ_REP_URL", "APPLIANCE_ZMQ_URL", "+", ":5150"],
    [Statement.SIMPLE_NUM, "LOCAL_ZMQ_SEND_TIMEOUT", 10],
    [Statement.SIMPLE_NUM, "LOCAL_ZMQ_RECEIVE_TIMEOUT", 20],
    [Statement.COMMENT, None],
    [Statement.COMMENT, "Cloud definitions"],
    [Statement.SIMPLE_PORT, "CLRPCD_LISTEND_PROMETHEUS_PORT", 3300],
    [Statement.SIMPLE_STR, "CL_SVC_URL", "https://svc0.b10e.net:443"],
    [Statement.SIMPLE_STR, "CL_SVC_RPC", "svc0.b10e.net:4430"],
    [Statement.SIMPLE_PORT, "CLRPCD_PROMETHEUS_PORT", 3600],
    [Statement.SIMPLE_PORT, "CLRPCD_HTTP_PORT", 4000],
    [Statement.LIST, "CLRPCD_URL", "CL_SVC_URL", "+", "/rpc"],
    [Statement.SECTION, ")"],
    [Statement.FOOTER, None],
]

py3symbols = []
py3contents = ""

golangsymbols = []
golangcontents = ""

def content(ops, aments):
    contents = ""

    for a in aments:
        op = a[0]
        try:
            contents += ops[op](a[1:])
        except:
            print(a)
            raise

    return contents

if __name__ == "__main__":
    opts, pargs = getopt.getopt(sys.argv[1:], "", longopts=["go", "python3"])

    for opt, arg in opts:
        if opt == "--go":
            ops = golangops
        elif opt == "--python3":
            ops = py3ops
        else:
            print(opt, arg, file=sys.stderr)
            sys.exit(2)

print(content(ops, assignments))
