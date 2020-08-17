#!/bin/bash -pe

#
# Copyright 2019 Brightgate Inc.
#
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#


if [[ -n $GCS_KEY_FILE ]]; then
	# Try to isolate the Google Cloud operations as much as possible.  We put auth
	# tokens in a private directory via $CLOUDSDK_CONFIG (instead of
	# ~/.config/gcloud), and tell gsutil to put any state it needs to keep in its
	# own directory (instead of ~/.gsutil), communicated to it through a special
	# boto config file.
	tmpdir=$(git rev-parse --show-toplevel)/.gcloud
	mkdir -p $tmpdir
	export CLOUDSDK_CONFIG=$tmpdir/auth
	export BOTO_CONFIG=$tmpdir/.boto
	cat > $BOTO_CONFIG <<-EOF
	[GSUtil]
	state_dir = $tmpdir/gsutil-state
	EOF

	# If $GCS_ACCOUNT is set and non-empty, use it; otherwise, gcloud will pull it
	# from the key file.
	gcloud -q auth activate-service-account $GCS_ACCOUNT --key-file=$GCS_KEY_FILE
fi

shopt -s extglob
set +e

# "versioned" copy: if the directory of the target already exists, add a -1 to
# it and try again; keep bumping the version until we find a non-existent
# directory.  This is inherently racy, but we almost certainly won't actually
# be racing, and preventing the race would be more complex than the current use
# case warrants.
vcp() {
	target=${@: -1}
	# This can fail for various reasons; there's no good interface for
	# determining what the reason was, but "matched no objects" has been
	# stable since it was introduced in 2012 and in the tests since 2014.
	lsoutput=$(gsutil ls -d ${target%/*} 2>&1)
	lsret=$?
	if [[ $lsret -ne 0 ]]; then
		if echo $lsoutput | grep -q "matched no objects"; then
			exec gsutil cp "$@"
		else
			echo $lsoutput
			exit $lsret
		fi
	else
		if [[ ${target: -1} != "/" ]]; then
			file=${target##*/}
		fi
		dir=${target%/*}
		base=${dir%-+([0-9])}
		version=$(( ${dir#$base} - 1 ))
		target=$base$version/$file
		vcp ${@:1:$#-1} $target
		fi
	}

if [[ $1 == "vcp" ]]; then
	shift
	vcp $@
else
	exec gsutil "$@"
fi

