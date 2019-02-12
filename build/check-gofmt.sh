#!/bin/bash
#
# COPYRIGHT 2019 Brightgate Inc. All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

GOFMT=${GOFMT:-gofmt}
GIT=${GIT:-git}

cd "$($GIT rev-parse --show-toplevel)" || exit 1

if ! readarray -t gofiles < <($GIT ls-files -- '*.go'); then
	echo "Couldn't get list of git managed go files" 1>&2
	exit 1
fi
[[ ${#gofiles[@]} -eq 0 ]] && exit 0

if ! readarray -t fmtfiles < <($GOFMT -l -e "${gofiles[@]}"); then
	echo "gofmt failed" 1>&2
	exit 1
fi
[[ ${#fmtfiles[@]} -eq 0 ]] && exit 0

echo "Some files failed gofmt's checks:" 1>&2
printf "  %s\n" "${fmtfiles[@]}" 1>&2
exit 1
