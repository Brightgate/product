#!/bin/bash -p

#
# Copyright 2019 Brightgate Inc.
#
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#


# Environment
# -----------
#
# DISTRO
#   'openwrt' or 'debian'.  debian will allow sysroot construction via a
#   multistrap(1) invocation.  openwrt requires that the sysroot and toolchain
#   archives from the rWRT build be present for upload.
#
# GCS_KEY_FILE
#   Path to a GCP JSON key file for the relevant service account.
#
# SYSROOT_SUM
#   For Debian, the sum is calculated from the SHA-256 hash of the installed
#   packages.  For OpenWrt, the sum is the Git hash of the rWRT repository that
#   produced the sysroot.

PATH=/usr/bin:/usr/sbin:/bin:/sbin
export PATH

pname=$(basename "$0")

function info() {
	echo "$pname: info: $*"
}

function fatal() {
	echo "$pname: fatal: $*" 1>&2
	exit 1
}

while getopts f: arg; do
	case "$arg" in
		"f") cfgfile="$OPTARG" ;;
	esac
done
shift $(($OPTIND - 1))

cmd=$1; shift

OPTIND=1
while getopts d:u arg; do
	case "$arg" in
		"u") upload="yes" ;;
		"d") dir="$OPTARG"  ;;
	esac
done
shift $(($OPTIND - 1))

[[ -n "$DISTRO" ]] || fatal "must specify a target distribution using DISTRO"
if [[ "$DISTRO" == "debian" ]]; then
	[[ -n "$cfgfile" ]] || fatal "must specify a multistrap config file with 'debian' distro"
	[[ -f "$cfgfile" ]] || fatal "multistrap config file $cfgfile does not exist"
else
	cfgfile=$DISTRO.multistrap
fi

# The sysroot blob is in the same directory as the configuration file
BLOB_NAME_PREFIX=$(basename $cfgfile)
BLOB_NAME_PREFIX=$(dirname $cfgfile)/sysroot.${BLOB_NAME_PREFIX%.multistrap}
BLOB_NAME_PREFIX=${BLOB_NAME_PREFIX#./}
BLOB_NAME=$BLOB_NAME_PREFIX.${SYSROOT_SUM:-UnknownSysrootSum}.tar.gz
SYSROOT_BUCKET_NAME=peppy-breaker-161717-sysroot
GCS_WRAPPER=$(git rev-parse --show-toplevel)/build/gcs-wrapper.sh

function cmd_help() {
	cat <<EOF
Usage: build-multistrap-sysroot.sh [-f multistrap_cfg] command [args]
	help
	name
	download
	unpack
	upload
	build

DISTRO {openwrt, debian} and SYSROOT_SUM environment variables control
execution.
EOF
	exit 2
}

function cmd_name() {
	echo $BLOB_NAME
	exit
}

function cmd_download() {
	$GCS_WRAPPER cp gs://$SYSROOT_BUCKET_NAME/$BLOB_NAME . || \
		fatal "could not download gs://$SYSROOT_BUCKET_NAME/$BLOB_NAME"

	if [[ "$DISTRO" == "openwrt" ]]; then
		TOOLCHAIN_BLOB_NAME=${BLOB_NAME/sysroot/toolchain}
		$GCS_WRAPPER cp gs://$SYSROOT_BUCKET_NAME/$TOOLCHAIN_BLOB_NAME . || \
			fatal "could not download gs://$SYSROOT_BUCKET_NAME/$TOOLCHAIN_BLOB_NAME"
	fi

	exit
}

function cmd_unpack() {
	# If we're told where to put the sysroot, make sure the directory
	# exists and then extract the contents there.  If we're not told, just
	# extract to the current directory without stripping the containing
	# directory.
	mkdir -p ${dir:-.}
	tar -ax ${dir:+-C $dir --strip-components=1} -f $BLOB_NAME || \
		fatal "could not untar $BLOB_NAME"

	if [[ "$DISTRO" == "openwrt" ]]; then
		echo unpacking toolchain
		TOOLCHAIN_BLOB_NAME=${BLOB_NAME/sysroot/toolchain}
		mkdir -p ${dir:-.}/../toolchain.$DISTRO
		tar -ax ${dir:+-C $dir/../toolchain.$DISTRO} -f $TOOLCHAIN_BLOB_NAME || \
			fatal "could not untar $TOOLCHAIN_BLOB_NAME"
	fi

	exit
}

# This is the just the logic of the upload, factored out so it can be used in
# two different contexts.
function upload() {
	info "Uploading sysroot as $BLOB_NAME"
	$GCS_WRAPPER cp -n $BLOB_NAME gs://$SYSROOT_BUCKET_NAME || \
		fatal "could not upload gs://$SYSROOT_BUCKET_NAME/$BLOB_NAME"
	if [[ "$DISTRO" == "openwrt" ]]; then
		TOOLCHAIN_BLOB_NAME=${BLOB_NAME/sysroot/toolchain}
		$GCS_WRAPPER cp -n $TOOLCHAIN_BLOB_NAME gs://$SYSROOT_BUCKET_NAME/ || \
			fatal "could not upload gs://$SYSROOT_BUCKET_NAME/$TOOLCHAIN_BLOB_NAME"
	fi
}

function cmd_upload() {
	upload
	exit
}

function cmd_build() {
	if [[ "$DISTRO" != "debian" ]]; then
		fatal "build only supported for 'debian' distro"
	fi

	# Build is just the rest of the script.
	:
}

cmd_$cmd

[[ $PWD == $(realpath $(dirname $0)) ]] || \
	fatal "must build the sysroot in $(realpath $(dirname $0))"

SYSROOT_NAME=$(awk -F= '/^directory=/ {print $2}' < "$cfgfile")
info "SYSROOT_NAME=$SYSROOT_NAME  (Based on $cfgfile)"

[[ -x /usr/sbin/multistrap ]] || fatal "multistrap package must be installed"

[[ -d $SYSROOT_NAME ]] && fatal "looks like $SYSROOT_NAME already exists"

/usr/sbin/multistrap -f "$cfgfile" || fatal "multistrap failed!"

if [[ "$DISTRO" == "debian" ]]; then
	PFRING=pfring-dev_7.4.0_$ARCH.deb
	$GCS_WRAPPER cp gs://$SYSROOT_BUCKET_NAME/$PFRING . || \
		fatal "could not download gs://$SYSROOT_BUCKET_NAME/$PFRING"

	fakeroot dpkg -i --force-architecture --root=$SYSROOT_NAME $PFRING || fatal "can't install $PFRING"
fi

NEW_SUM=$(egrep "^(Package|Version):" $SYSROOT_NAME/var/lib/dpkg/status | sha256sum | awk '{print $1}')
if [[ $NEW_SUM != $SYSROOT_SUM ]]; then
	info "New sysroot checksum: $NEW_SUM"
fi

info "removing extraneous stuff from sysroot"

RMDIRLIST=(bin sbin man *perl* *python* locale doc zoneinfo udev systemd)

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
info "remove unnecessary but large files from var"
# Keep .list, .shlibs, and .symbols files around because they're used by dpkg-shlibdeps
find $SYSROOT_NAME/var/lib/dpkg/info -type f \
    \! \( -name '*.list' -o -name '*.shlibs' -o -name '*.symbols' \) -exec rm \{\} +

touch $SYSROOT_NAME/.$NEW_SUM

BLOB_NAME=$BLOB_NAME_PREFIX.$NEW_SUM.tar.gz
tar -ca --owner=root --group=root -f $BLOB_NAME $SYSROOT_NAME

SIZE=$(du -hs "$SYSROOT_NAME" | awk '{print $1}')
COMPRESSED_SIZE=$(du -hs "$BLOB_NAME" | awk '{print $1}')
info "Final sysroot size: $SIZE ($COMPRESSED_SIZE compressed)"

# This will attempt to upload a blob that's already in the store if we haven't
# updated SYSROOT_SUM to match in the top-level Makefile.  The upload won't
# proceed because of the -n flag if the blob is already there.
if [[ $upload == "yes" && $NEW_SUM != $SYSROOT_SUM ]]; then
	upload || exit
	info "Update SYSROOT_SUM to $NEW_SUM in the top-level Makefile to use the new sysroot."
fi

exit 0

