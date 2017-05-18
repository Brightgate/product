#!/bin/bash
#
# COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

# Bootstrap the network for a Raspberry Pi in AP gateway mode.

LAN=192.168.136

ip addr add ${LAN}.1 dev wlan0

# Allow traffic initiated from VPN to access "the world"
iptables -I FORWARD -i wlan0 -o eth0 \
	-s ${LAN}.0/24 -m conntrack --ctstate NEW -j ACCEPT

# Allow established traffic to pass back and forth
iptables -I FORWARD -m conntrack --ctstate RELATED,ESTABLISHED \
	-j ACCEPT

# Notice that -I is used, so when listing it (iptables -vxnL) it
# will be reversed.  This is intentional in this demonstration.

# Masquerade traffic from VPN to "the world" -- done in the nat table
iptables -t nat -I POSTROUTING -o eth0 \
	-s ${LAN}.0/24 -j MASQUERADE

route add -net ${LAN}.0 netmask 255.255.255.0 dev wlan0

sysctl -w net.ipv4.ip_forward=1
