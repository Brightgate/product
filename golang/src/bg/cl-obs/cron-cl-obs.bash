#!/bin/bash -px
#
# COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

# Typical job for cron-driven ingest and predict.

set -o errexit

CL_OBS=./proto.x86_64/cloud/opt/net.b10e/bin/cl-obs
OUI=./proto.x86_64/appliance/opt/com.brightgate/etc/identifierd/oui.txt

FACTS=./golang/src/bg/cl-obs/facts.sqlite3
OBSERVATIONS=./golang/src/bg/cl-obs/observations.db
TRAINED_MODELS=./golang/src/bg/cl-obs/trained-models.db

export GOOGLE_APPLICATION_CREDENTIALS=~/creds/staging-168518-79d693d0ae83.json

function orun {
	echo "###" "$@"
	time "$@"
}

export CL_SRC="--project staging-168518"

orun $CL_OBS ingest --cpuprofile=ingest-1.prof \
	$CL_SRC \
	--oui-file=$OUI \
		--observations-file=$OBSERVATIONS

# Re-apply facts. (XXX Should not be needed.)
sqlite3 "$OBSERVATIONS" < "$FACTS"

# Classify all known sites.
for site in $($CL_OBS site \
	$CL_SRC \
	--model-file $TRAINED_MODELS \
	--oui-file $OUI \
	--observations-file $OBSERVATIONS \
	| cut -f 1 -d ' '); do
	$CL_OBS classify \
		--persist \
		$CL_SRC \
		--model-file $TRAINED_MODELS \
		--oui-file $OUI \
		--observations-file $OBSERVATIONS \
		"$site"
	done
