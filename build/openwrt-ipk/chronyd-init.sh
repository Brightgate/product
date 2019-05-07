#!/bin/sh /etc/rc.common
# Copyright (C) 2006-2015 OpenWrt.org

START=15
USE_PROCD=1
PROG=/usr/sbin/chronyd
CONFIGFILE=/var/etc/chrony.conf

start_service() {
	. /lib/functions/network.sh

	procd_open_instance
	procd_set_param command $PROG -n -f $CONFIGFILE
	procd_close_instance

	# chronyd won't start if the config file doesn't exist
	mkdir -p $(dirname $CONFIGFILE)
	touch $CONFIGFILE

	# chronyd doesn't create and set perms on the directory for its
	# driftfile like it does for some of its other configurable paths, and
	# the directory isn't delivered by any package, so we make it here.
	mkdir -p /data/chrony
	chown chrony:chrony /data/chrony
}
