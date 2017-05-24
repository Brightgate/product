#!/bin/bash -p
# vim:set comments=b:#:
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
red=$(tput setaf 1)
offred=$offbold


root=$PWD/proto.$(uname -m)
if [[ ! -d $root ]]; then
	echo no proto in $PWD
	exit 1
fi

pythonver=3
pythonpath=$root/usr/local/lib/python$pythonver/dist-packages
bin=$root/opt/com.brightgate/bin
etc=$root/opt/com.brightgate/etc
var=$root/opt/com.brightgate/var

function log_error {
	echo $(date +0%Y-%m-%d\ %H:%M:%S) $red{$1} $offgreen$bold$2$offbold
}

function log_privileged {
	echo $(date +0%Y-%m-%d\ %H:%M:%S) $blue{$1} $offgreen$bold$2$offbold
}

function log_info {
	echo $(date +0%Y-%m-%d\ %H:%M:%S) $green{$1} $offgreen$bold$2$offbold
}

function pyrun {
	log_info python-run $1
	PYTHONPATH=$pythonpath python$pythonver $bin/$1
}

function binrun {
	log_info binrun $1
	GOTRACEBACK=${GOTRACEBACK:-single} $bin/$1
}

# XXX caprun?
function sudobinrun {
	log_privileged run $1
	sudo -E GOTRACEBACK=${GOTRACEBACK:-single} $bin/$1
}

function sudopyrun {
	log_privileged run $1
	sudo -E PYTHONPATH=$pythonpath python$pythonver $bin/$1
}

function nyi {
	log_error $1 not yet implemented
	exit 1
}

function usage {
	cat <<EOF
Usage:	ap-run broker
	ap-run dhcpd
	ap-run dnsd
	ap-run httpd
	ap-run filterd
	ap-run hostapd
	ap-run analyzerd
	ap-run actord
	ap-run exploitd
	ap-run prometheus

	ap-run start-world
	ap-run update-world
EOF
	exit 2
}
if [[ $1 == broker ]]; then
	binrun ap.brokerd
elif [[ $1 == dhcpd ]]; then
	sudobinrun ap.dhcp4d
elif [[ $1 == dnsd ]]; then
	sudobinrun ap.dns4d
elif [[ $1 == hostapd ]]; then
	sudobinrun ap.hostapd.m
elif [[ $1 == httpd ]]; then
	binrun ap.httpd # While using port 8000.
elif [[ $1 == logd ]]; then
	binrun ap.logd
elif [[ $1 == prometheus ]]; then
	log_info direct-run prometheus
	$bin/prometheus -config.file=$etc/prometheus.yml -storage.local.path="$var/prometheus-data"
elif [[ $1 == sampled ]]; then
	nyi $1
elif [[ $1 == analyzerd ]]; then
	nyi $1
elif [[ $1 == actord ]]; then
	nyi $1
elif [[ $1 == exploitd ]]; then
	nyi $1
elif [[ $1 == "start-world" ]]; then
	sudo echo "Prepare world"
	binrun ap.brokerd &
	binrun prometheus &
	sleep 3
	binrun ap.logd &
	sudobinrun ap.hostapd.m &
	sudobinrun ap.dhcp4d &
	sudobinrun ap.dns4d &
	binrun ap.httpd # While using port 8000.
	# Wait here.
elif [[ $1 == "update-world" ]]; then
	# XXX To create an aware refresh, we would have to be able to
	# determine that the process start time precedes the mtimes of
	# any of its dependencies.  We might have this information from
	# other sources.
	nyi $1
elif [[ -z $1 ]]; then
	log_error no-command "must provide valid component or command"
	usage
else
	log_error unknown $1
	usage
fi
