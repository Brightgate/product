#!/bin/bash
#
# Copyright 2019 Brightgate Inc.
#
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

