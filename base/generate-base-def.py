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
    """Enumeration of statement kinds."""
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
    [Statement.SIMPLE_NUM, "EXIT_OK", 0],
    [Statement.SIMPLE_NUM, "EXIT_ERROR", 1],
    [Statement.SIMPLE_NUM, "EXIT_USAGE", 2],
    [Statement.SIMPLE_NUM, "RADIUS_SECRET_SIZE", 8],
    [Statement.SIMPLE_NUM, "HTTPD_HMAC_SIZE", 32],
    [Statement.SIMPLE_NUM, "HTTPD_AES_SIZE", 32],
    [Statement.SIMPLE_STR, "GATEWAY_CLIENT_DOMAIN", "brightgate.net"],
    [Statement.SIMPLE_NUM, "BEARER_JWT_EXPIRY_SECS", 60 * 60],

    [Statement.COMMENT, "Appliance operating modes"],
    [Statement.SIMPLE_STR, "MODE_GATEWAY", "gateway"],
    [Statement.SIMPLE_STR, "MODE_CORE", "core"],
    [Statement.SIMPLE_STR, "MODE_SATELLITE", "satellite"],
    [Statement.SIMPLE_STR, "MODE_HTTP_DEV", "http-dev"],

    [Statement.COMMENT, "Security rings"],
    [Statement.SIMPLE_STR, "RING_UNENROLLED", "unenrolled"],
    [Statement.SIMPLE_STR, "RING_CORE", "core"],
    [Statement.SIMPLE_STR, "RING_STANDARD", "standard"],
    [Statement.SIMPLE_STR, "RING_DEVICES", "devices"],
    [Statement.SIMPLE_STR, "RING_GUEST", "guest"],
    [Statement.SIMPLE_STR, "RING_QUARANTINE", "quarantine"],
    [Statement.SIMPLE_STR, "RING_WAN", "wan"],
    [Statement.SIMPLE_STR, "RING_INTERNAL", "internal"],

    [Statement.COMMENT, "Message bus topics"],
    [Statement.SIMPLE_STR, "TOPIC_PING", "sys.ping"],
    [Statement.SIMPLE_STR, "TOPIC_MCP", "sys.mcp"],
    [Statement.SIMPLE_STR, "TOPIC_CONFIG", "sys.config"],
    [Statement.SIMPLE_STR, "TOPIC_ERROR", "sys.error"],
    [Statement.SIMPLE_STR, "TOPIC_ENTITY", "net.entity"],
    [Statement.SIMPLE_STR, "TOPIC_REQUEST", "net.request"],
    [Statement.SIMPLE_STR, "TOPIC_RESOURCE", "net.resource"],
    [Statement.SIMPLE_STR, "TOPIC_SCAN", "net.scan"],
    [Statement.SIMPLE_STR, "TOPIC_LISTEN", "net.listen"],
    [Statement.SIMPLE_STR, "TOPIC_EXCEPTION", "net.exception"],
    [Statement.SIMPLE_STR, "TOPIC_IDENTITY",  "net.identity"],
    [Statement.SIMPLE_STR, "TOPIC_UPDATE",  "net.update"],
    [Statement.SIMPLE_STR, "TOPIC_OPTIONS",  "net.options"],

    [Statement.COMMENT, "Diagnostic client HTTP ports"],
    [Statement.SIMPLE_PORT, "BROKERD_DIAG_PORT", 3200],
    [Statement.SIMPLE_PORT, "HTTPD_DIAG_PORT", 3201],
    [Statement.SIMPLE_PORT, "LOGD_DIAG_PORT", 3202],
    [Statement.SIMPLE_PORT, "NETWORKD_DIAG_PORT", 3205],
    [Statement.SIMPLE_PORT, "USERAUTHD_DIAG_PORT", 3206],
    [Statement.SIMPLE_PORT, "CONFIGD_DIAG_PORT", 3207],
    [Statement.SIMPLE_PORT, "WATCHD_DIAG_PORT", 3208],
    [Statement.SIMPLE_PORT, "RPCD_DIAG_PORT", 3210],
    [Statement.SIMPLE_PORT, "SERVICED_DIAG_PORT", 3209],
    [Statement.SIMPLE_PORT, "MCP_DIAG_PORT", 3211],

    [Statement.COMMENT, "ZeroMQ definitions"],
    [Statement.SIMPLE_STR, "INCOMING_ZMQ_URL", "tcp://*"],
    [Statement.SIMPLE_STR, "LOCAL_ZMQ_URL", "tcp://127.0.0.1"],
    [Statement.SIMPLE_STR, "GATEWAY_ZMQ_URL", "tcp://gateway"],
    [Statement.SIMPLE_PORT, "BROKER_ZMQ_PUB_PORT", 3131],
    [Statement.SIMPLE_PORT, "BROKER_ZMQ_SUB_PORT", 3132],
    [Statement.SIMPLE_PORT, "CONFIGD_ZMQ_REP_PORT", 3133],
    [Statement.SIMPLE_PORT, "WATCHD_ZMQ_REP_PORT", 3134],
    [Statement.SIMPLE_PORT, "MCP_ZMQ_REP_PORT", 3135],
    [Statement.COMMENT, None],

    [Statement.SIMPLE_PORT, "CLRPCD_DIAG_PORT", 3600],
    [Statement.SIMPLE_PORT, "CLEVENTD_DIAG_PORT", 3601],
    [Statement.SIMPLE_PORT, "CLCONFIGD_DIAG_PORT", 3602],

    [Statement.SIMPLE_PORT, "CLRPCD_GRPC_PORT", 443],
    [Statement.SIMPLE_PORT, "CLCONFIGD_GRPC_PORT", 4431],

    [Statement.COMMENT, "API related definitions"],
    [Statement.SIMPLE_STR, "API_URL", "https://api.brightgate.com"],
    [Statement.LIST, "API_PROTOBUF_URL", "API_URL", "+", "/protobuf"],

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
