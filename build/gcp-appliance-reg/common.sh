#!/bin/bash
#
# COPYRIGHT 2018 Brightgate Inc. All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

set -o pipefail

# Create files private by default
umask 077

pname=$(basename "$0")

function info() {
        echo "$pname: info: $*"
}

function fatal() {
        echo "$pname: fatal: $*" 1>&2
        exit 1
}

PROXYPID=
SOCKDIR="/tmp/sqlprox-$(id -un)"

function stop_cloudsql_proxy() {
	if [[ -n $PROXYPID ]]; then
		kill -HUP $PROXYPID
		wait $PROXYPID 2>/dev/null
		ret=$?
		if [[ $ret -ne 0 && $ret != 129 ]]; then
			cat "$SOCKDIR/proxy.out"
			fatal "sql proxy exited $ret; see output above."
		fi
		PROXYPID=
		rm -fr "$SOCKDIR"
		echo "stopped sql proxy"
	fi
}

function start_cloudsql_proxy() {
	local credfile=$1
	local instance=$2

	mkdir -p "$SOCKDIR"
	/opt/net.b10e/cloud-sql-proxy/bin/cloud_sql_proxy \
		-dir="$SOCKDIR" \
		-instances="$instance" \
		-credential_file="$credfile" > "$SOCKDIR/proxy.out" 2>&1 &
	local ret=$?
	PROXYPID=$!
	[[ $ret -eq 0 ]] || fatal "failed to start SQL proxy"

	sleep 3
	echo "started sql proxy"
}
