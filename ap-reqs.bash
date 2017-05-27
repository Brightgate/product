#!/bin/bash -p
#
# COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

italic=$(tput smso)
offitalic=$(tput rmso)
bold=$(tput bold)
offbold=$(tput sgr0)
green=$(tput setaf 2)
offgreen=$offbold
blue=$(tput setaf 4)
offblue=$offbold

root=$PWD/proto

promver=1.5.0

function log_error {
	echo $(date +0%Y-%m-%d\ %H:%M:%S) $red{$1} $offgreen$bold$2$offbold
}

function log_privileged {
	echo $(date +0%Y-%m-%d\ %H:%M:%S) $blue{$1} $offgreen$bold$2$offbold
}

function log_info {
	echo $(date +0%Y-%m-%d\ %H:%M:%S) $green{$1} $offgreen$bold$2$offbold
}

function get_prometheus {
	if [[ ! -d 3 ]]; then
		mkdir 3
	fi

	if [[ ! -f 3/prometheus-$promver.linux-amd64.tar.gz ]]; then
		log_info get_prometheus "curl-retrieve prometheus $promver"

		( cd 3 && curl -LO https://github.com/prometheus/prometheus/releases/download/v$promver/prometheus-$promver.linux-amd64.tar.gz )
	fi

	log_info tar get_prometheus "unarchive prometheus $promver"
	( cd 3 && tar xf prometheus-$promver.linux-amd64.tar.gz )

}

get_prometheus
