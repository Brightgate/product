#!/bin/bash -px
#
# Copyright 2020 Brightgate Inc.
#
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#


# Run through complete operations of cl-obs.
#
# A typical run, using cloud data:
#
# $ make
# $ export GOOGLE_APPLICATION_CREDENTIALS=/path/to/creds
# $ bash golang/src/bg/test-cl-obs.bash cloud ingest

set -o errexit

CL_OBS=./proto.x86_64/cloud/opt/net.b10e/bin/cl-obs

FACTS=./golang/src/bg/cl-obs/facts.sqlite3
OBSERVATIONS=./golang/src/bg/cl-obs/observations.db
TRAINED_MODELS=./golang/src/bg/cl-obs/trained-models.db

function orun {
	echo "###" "$@"
	time "$@"
}

FILES_SRC=${FILES_SRC:-/home/stephen/staging-spools/svc-1/spool}
CLOUD_SRC=${CLOUD_SRC:-staging-168518}

if [ "$1" == "files" ]; then
	export CL_SRC="--dir=$FILES_SRC"
	OBSERVATIONS=./golang/src/bg/cl-obs/observations-files.db
	TRAINED_MODELS=./golang/src/bg/cl-obs/trained-models-files.db
elif [ "$1" == "cloud" ]; then
	export CL_SRC="--project=$CLOUD_SRC"

	if [ -z "$GOOGLE_APPLICATION_CREDENTIALS" ]; then
		echo "must define GOOGLE_APPLICATION_CREDENTIALS for cloud runs"
		exit 2
	fi
else
	echo "need input source ('cloud' or 'files')"
	exit 2
fi
echo "### CL_SRC is '$CL_SRC'"

shift

if [ "$1" == "ingest" ]; then
	$CL_OBS ingest --cpuprofile=ingest-1.prof \
		"$CL_SRC" \
		--observations-file=$OBSERVATIONS

	shift
fi

if [ "$1" == "detailed" ]; then
	detailed=1
	echo "running detailed tests"
fi

# Re-apply facts
sqlite3 "$OBSERVATIONS" < "$FACTS"

# Extract options

orun $CL_OBS extract --dhcp \
	"$CL_SRC" \
	--observations-file $OBSERVATIONS
if [ -n "$detailed" ]; then
	orun $CL_OBS extract --dns  \
		"$CL_SRC" \
		--observations-file $OBSERVATIONS
	orun $CL_OBS extract --mfg  \
		"$CL_SRC" \
		--observations-file $OBSERVATIONS
	orun $CL_OBS extract --device  \
		"$CL_SRC" \
		--observations-file $OBSERVATIONS
fi

# Site options

orun $CL_OBS site \
	"$CL_SRC" \
	--observations-file $OBSERVATIONS
orun $CL_OBS site --verbose \
	"$CL_SRC" \
	--observations-file $OBSERVATIONS

orun $CL_OBS ls \
	"$CL_SRC" \
	--observations-file $OBSERVATIONS \
	"00:11:d9:95:3d:b2"
orun $CL_OBS ls \
	--redundant \
	"$CL_SRC" \
	--observations-file $OBSERVATIONS \
	"00:11:d9:95:3d:b2"

# Train and review
#   Output to the combined models file.
orun $CL_OBS train --cpuprofile=train-0.prof \
	"$CL_SRC" \
	--model-file $TRAINED_MODELS \
	--observations-file $OBSERVATIONS

#   Review the training set for validity and redundancy.
orun $CL_OBS review \
	"$CL_SRC" \
	--model-file $TRAINED_MODELS \
	--observations-file $OBSERVATIONS

# Classify
#   Use the combined models file.
orun $CL_OBS classify \
	--model-file $TRAINED_MODELS \
	--observations-file $OBSERVATIONS \
	cc4f2549-5e64-4710-b63b-64ab5558aeee

# Classify all known sites.
$CL_OBS classify \
	--persist \
	--model-file $TRAINED_MODELS \
	--observations-file $OBSERVATIONS \
	'*'

# Site, with predictions.
orun $CL_OBS site \
	--verbose \
	"$CL_SRC" \
	--model-file $TRAINED_MODELS \
	--observations-file $OBSERVATIONS \
	dogfood-mt7623

orun $CL_OBS site \
	--verbose \
	"$CL_SRC" \
	--model-file $TRAINED_MODELS \
	--observations-file $OBSERVATIONS \
	stephen-osage

