#!/bin/bash -p

#
# COPYRIGHT 2017 Brightgate Inc. All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

PATH=/usr/bin:/usr/sbin:/bin
export PATH

pname=$(basename "$0")
cfgfile="$1"

function info() {
	echo "$pname: info: $*"
}

function fatal() {
	echo "$pname: fatal: $*" 1>&2
	exit 1
}

[[ -f "$cfgfile" ]] || fatal "must specify a multistrap config file"

SYSROOT_NAME=$(awk -F= '/^directory=/ {print $2}' < "$cfgfile")
info "SYSROOT_NAME=$SYSROOT_NAME  (Based on $cfgfile)"

[[ -x /usr/sbin/multistrap ]] || fatal "multistrap package must be installed"

[[ -d $SYSROOT_NAME ]] && fatal "looks like $SYSROOT_NAME already exists"

/usr/sbin/multistrap -f "$cfgfile" || fatal "multistrap failed!"

info "removing extraneous stuff from sysroot"

RMDIRLIST=(bin sbin man *perl* *python* var locale doc zoneinfo udev systemd)

for pattern in "${RMDIRLIST[@]}"; do
	info "remove directories matching $pattern"
	find "$SYSROOT_NAME" -name "$pattern" -type d | while read -r x; do
		rm -fr "$x"
	done
done
info "remove non-header-files from usr/share"
find "$SYSROOT_NAME/usr/share" -type f ! -name '*.h' -print0 | xargs -0 --no-run-if-empty rm
info "remove etc"
rm -fr "${SYSROOT_NAME:??}/etc"

#
# aaarrrggghhh
#
info "Adding tensorflow"
tmpdir=$(mktemp --directory)
/opt/net.b10e/bin/arc download F603 --as "$tmpdir/libtensorflow-raspberrypi.tar.gz" || \
	die "tensorflow download failed"
mkdir -p "$SYSROOT_NAME/usr/local/lib"
tar --to-stdout -x -f "$tmpdir/libtensorflow-raspberrypi.tar.gz" \
	 raspberrypi/libtensorflow.so > "$SYSROOT_NAME/usr/local/lib/libtensorflow.so" || \
	die "tar extract failed"
chmod a+rx "$SYSROOT_NAME/usr/local/lib/libtensorflow.so"
rm -fr "$tmpdir"

SIZE=$(du -hs "$SYSROOT_NAME")
info "Final sysroot size: $SIZE"

exit 0
