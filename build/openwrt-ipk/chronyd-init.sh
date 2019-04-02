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

	# This is where we put the drift file
	mkdir -p /var/lib/chrony
}
