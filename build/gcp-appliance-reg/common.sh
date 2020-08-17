#!/bin/bash
#
# Copyright 2018 Brightgate Inc.
#
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

