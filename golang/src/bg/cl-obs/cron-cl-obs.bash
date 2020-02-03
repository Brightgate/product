#!/bin/bash -p
#
# COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

# cron-driven ingest and predict.

set -o errexit
set -o pipefail

if [[ -z $GOOGLE_APPLICATION_CREDENTIALS ]]; then
	echo "must supply \$GOOGLE_APPLICATION_CREDENTIALS" 1>&2
	exit 2
fi
if [[ -z $GCP_PROJECT ]]; then
	echo "must supply \$GCP_PROJECT" 1>&2
	exit 2
fi

if [[ -z $B10E_CLREG_CLCONFIGD_CONNECTION ]]; then
	echo "must supply \$B10E_CLREG_CLCONFIGD_CONNECTION" 1>&2
	exit 2
fi

if [[ -z $REG_DBURI ]]; then
	echo "must supply \$REG_DBURI" 1>&2
	exit 2
fi

# For testing, allow an override of sites
sites=('*')
if [[ -n $SITES ]]; then
	echo "overriding site selection using \$SITES"
	sites=($SITES)
fi

function orun {
       echo "" 1>&2
       echo "###" "$@" 1>&2
       "$@"
}

# mirrors golang daemonutils:ClRoot(); trust CLROOT if set, else
# compute CLROOT relative to executable's path.
if [[ -z $CLROOT ]]; then
	dir=$(dirname "${BASH_SOURCE[0]}")
	CLROOT=$(realpath "$dir/.." )
	export CLROOT
fi
echo "CLROOT is $CLROOT"

CL_OBS=$CLROOT/bin/cl-obs
CL_REG=$CLROOT/bin/cl-reg

DATADIR=${DATADIR:-$CLROOT/var/cron-cl-obs}
if [[ ! -d $DATADIR ]]; then
	orun mkdir -p "$DATADIR"
fi

OBSERVATIONS=$DATADIR/observations.db
TRAINED_MODELS=${TRAINED_MODELS:-gs://bg-classifier-support/trained-models.db}

export CL_SRC="--project=$GCP_PROJECT"

if [[ -n $SKIP_INGEST ]]; then
	echo "Skipping ingest due to \$SKIP_INGEST"
else
	orun "$CL_OBS" ingest \
		"$CL_SRC" \
		--observations-file="$OBSERVATIONS" "${sites[@]}"
fi

# Classify sites
orun "$CL_OBS" classify \
	--persist \
	--model-file "$TRAINED_MODELS" \
	--observations-file "$OBSERVATIONS" "${sites[@]}"
if [[ $? -ne 0 ]]; then
	echo "failed during classify"
	exit 1
fi

if [[ -z $SYNC ]]; then
	echo "Set \$SYNC to enable real sync; using Dry Run instead"
	SYNC_DRYRUN="--dry-run"
fi

if [[ -n $SITES ]]; then
	for site in "${sites[@]}"; do
		orun "$CL_REG" deviceid sync $SYNC_DRYRUN -s "$site" --sqlite-src "$OBSERVATIONS"
		if [[ $? -ne 0 ]]; then
			echo "failed deviceid site $site to config trees"
			exit 1
		fi
	done
else
	orun "$CL_REG" deviceid sync $SYNC_DRYRUN -a --sqlite-src "$OBSERVATIONS"
	if [[ $? -ne 0 ]]; then
		echo "failed deviceid sync to config trees"
		exit 1
	fi
fi
exit 0
