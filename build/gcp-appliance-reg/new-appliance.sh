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

pdir=$(dirname "$0")
source "$pdir/common.sh"

OUTPUT_DIR="$pdir/output_secrets"

function usage() {
	cat <<-EOF
		usage: $0 [-u <appliance-uuid>] [-m <mac-addr>] [-h <hw-serial>] -s <site-uuid>|null -c <credentials-file> <appliance-id>
		Must also set environment variables (or source from reg file):
		    REG_PROJECT_ID=<name of gcp project>
		    REG_REGION_ID=<name of gcp region>
		    REG_REGISTRY_ID=<registry name>
		    REG_CLOUDSQL_INSTANCE=<cloudsql instance name>
		    REG_DBURI=<postgres uri>
	EOF
	exit 2
}

while getopts c:h:m:s:u: FLAG; do
	case $FLAG in
		c)
			CRED_FILE=$OPTARG
			;;
		s)
			SITE_UUID=$OPTARG
			;;
		u)
			APPLIANCE_UUID=$OPTARG
			;;
		h)
			HW_SERIAL=$OPTARG
			;;
		m)
			MAC_ADDR=$OPTARG
			;;
		*)
			usage
			;;
	esac
done
shift $((OPTIND-1))

APPLIANCE_ID=$1

if [[ -z $CRED_FILE || ! -f $CRED_FILE || -z $APPLIANCE_ID || -z $SITE_UUID || -z $REG_PROJECT_ID || -z $REG_REGION_ID || -z $REG_REGISTRY_ID || -z $REG_CLOUDSQL_INSTANCE || -z $REG_DBURI ]]; then
	usage
fi

GCP_ACCT=$(gcloud auth list --filter=status:ACTIVE --format="value(account)")
[[ -n $GCP_ACCT ]] || fatal "gcloud not initialized?  Couldn't get account name"
SVC_ACCT=

function at_exit() {
	stop_cloudsql_proxy
	[[ -n $GCP_ACCT ]] && gcloud config set account "$GCP_ACCT"
	[[ -n $SVC_ACCT ]] && gcloud auth revoke "$SVC_ACCT"
}
trap at_exit EXIT

gcloud --project="$REG_PROJECT_ID" auth activate-service-account --key-file="$CRED_FILE"
[[ $? -eq 0 ]] || fatal "gcloud auth activate-service-account failed"
SVC_ACCT=$(gcloud auth list --filter=status:ACTIVE --format="value(account)")

# spin up a proxy using the credentials we've been given
start_cloudsql_proxy "$CRED_FILE" "${REG_PROJECT_ID}:${REG_REGION_ID}:${REG_CLOUDSQL_INSTANCE}"

CL_REG=$(git rev-parse --show-toplevel)/proto.$(uname -m)/cloud/opt/net.b10e/bin/cl-reg
$CL_REG app new \
	${APPLIANCE_UUID:+--uuid $APPLIANCE_UUID} \
	${HW_SERIAL:+--hw-serial $HW_SERIAL} \
	${MAC_ADDR:+--mac-address $MAC_ADDR} \
	-d "$OUTPUT_DIR" \
	"$APPLIANCE_ID" "$SITE_UUID"
