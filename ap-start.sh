#!/bin/sh
#
# COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

# XXX Need a better mechanism to achieve this.  It appears that
# wpa_supplicant is started by dhcpcd on Raspbian.
pkill wpa_supplicant
sleep 1
pkill -9 wpa_supplicant
sleep 1

/opt/com.brightgate/bin/ap-ctl start all
