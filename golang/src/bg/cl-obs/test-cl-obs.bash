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

# Run through complete operations of cl-obs.
#
# $ make
# $ bash golang/src/bg/test-cl-obs.bash

set -o errexit

CL_OBS=./proto.x86_64/cloud/opt/net.b10e/bin/cl-obs
OUI=./proto.x86_64/appliance/opt/com.brightgate/etc/identifierd/oui.txt

FACTS=./golang/src/bg/cl-obs/facts.sqlite3
OBSERVATIONS=./golang/src/bg/cl-obs/observations.db
TRAINED_MODELS=./golang/src/bg/cl-obs/trained-models.db

function orun {
	echo "###" "$@"
	time "$@"
}

# Ingest
if [[ "$1" == "ingest" ]]; then
	$CL_OBS ingest --cpuprofile=ingest-1.prof \
		--dir ~/staging-spools/svc-1/spool \
		--oui-file=$OUI \
		--observations-file=$OBSERVATIONS
fi

# Re-apply facts
sqlite3 "$OBSERVATIONS" < "$FACTS"

# Extract options

orun $CL_OBS extract --dhcp \
	--dir ~/staging-spools/svc-1/spool \
	--oui-file=$OUI \
	--observations-file $OBSERVATIONS
orun $CL_OBS extract --dns  \
	--dir ~/staging-spools/svc-1/spool \
	--oui-file $OUI \
	--observations-file $OBSERVATIONS
orun $CL_OBS extract --mfg  \
	--dir ~/staging-spools/svc-1/spool \
	--oui-file $OUI \
	--observations-file $OBSERVATIONS
orun $CL_OBS extract --device  \
	--dir ~/staging-spools/svc-1/spool \
	--oui-file $OUI \
	--observations-file $OBSERVATIONS

# Site options

orun $CL_OBS site \
	--dir ~/staging-spools/svc-1/spool \
	--oui-file $OUI \
	--observations-file $OBSERVATIONS
orun $CL_OBS site --verbose \
	--dir ~/staging-spools/svc-1/spool \
	--oui-file $OUI \
	--observations-file $OBSERVATIONS

# Device options
#   Prints sentence for every record in inventory.

# Workbench options

orun $CL_OBS ls \
	--dir ~/staging-spools/svc-1/spool \
	--oui-file $OUI \
	--observations-file $OBSERVATIONS \
	"00:11:d9:95:3d:b2"

orun $CL_OBS cat \
	--dir ~/staging-spools/svc-1/spool \
	--oui-file $OUI \
	--observations-file $OBSERVATIONS \
	"00:11:d9:95:3d:b2"

#$CL_OBS device --verbose  \
#	--dir ~/staging-spools/svc-1/spool \
#	--oui-file $OUI \
#	--observations-file $OBSERVATIONS

# Train and review
#   Output to the combined models file.
orun $CL_OBS train --cpuprofile=train-0.prof \
	--dir ~/staging-spools/svc-1/spool \
	--model-file $TRAINED_MODELS \
	--oui-file $OUI \
	--observations-file $OBSERVATIONS

orun $CL_OBS review \
	--model-file $TRAINED_MODELS \
	--dir ~/staging-spools/svc-1/spool \
	--oui-file $OUI \
	--observations-file $OBSERVATIONS

# Classify
#   Use the combined models file.
orun $CL_OBS classify \
	--dir ~/staging-spools/svc-1/spool \
	--model-file $TRAINED_MODELS \
	--oui-file $OUI \
	--observations-file $OBSERVATIONS \
	cc4f2549-5e64-4710-b63b-64ab5558aeee

# Classify all known sites.
for site in $($CL_OBS site \
	--dir ~/staging-spools/svc-1/spool \
	--model-file $TRAINED_MODELS \
	--oui-file $OUI \
	--observations-file $OBSERVATIONS \
	| cut -f 1 -d ' '); do
	$CL_OBS classify \
		--persist \
		--dir ~/staging-spools/svc-1/spool \
		--model-file $TRAINED_MODELS \
		--oui-file $OUI \
		--observations-file $OBSERVATIONS \
		"$site"
	done

# Site, with predictions.
orun $CL_OBS site \
	--verbose \
	--dir ~/staging-spools/svc-1/spool \
	--model-file $TRAINED_MODELS \
	--oui-file $OUI \
	--observations-file $OBSERVATIONS \
	--match stephen-osage
