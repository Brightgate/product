#!/bin/sh /etc/rc.common
# Copyright (C) 2006-2015 OpenWrt.org

START=15
USE_PROCD=1
PROG=/usr/sbin/chronyd
CONFIGFILE=/etc/chrony/bg-chrony.base.conf

start_service() {
	# Mount /data, if not already mounted.
	/bin/df -T /data | /usr/bin/tail -1 | /bin/grep -q f2fs
	if [ $? != 0 ]; then
		/usr/bin/mount /data
		/usr/bin/pkill -HUP rsyslogd
	fi

	. /lib/functions/network.sh

	procd_open_instance
	procd_set_param command $PROG -n -f $CONFIGFILE
	procd_close_instance

	# chronyd doesn't create and set perms on the directory for its
	# driftfile like it does for some of its other configurable paths, and
	# the directory isn't delivered by any package, so we make it here.
	mkdir -p /data/chrony
	chown chrony:chrony /data/chrony

	# If the Brightgate stack already configured chrony to point at
	# specific servers, make sure we continue to point at them even if the
	# config file was redelivered.  Otherwise, this will happen when the
	# configuration happens.
	if [ -f /data/chrony/bg-chrony.client ]; then
		echo "include /data/chrony/bg-chrony.client" > /etc/chrony/bg-chrony.client
	fi
}
