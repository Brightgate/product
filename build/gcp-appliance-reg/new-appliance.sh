#!/bin/bash
#
# COPYRIGHT 2018 Brightgate Inc. All rights reserved.
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

CRED_FILE=$1
APPLIANCE_ID=$2
CLOUD_UUID=$3

if [[ -z $CRED_FILE || ! -f $CRED_FILE || -z $APPLIANCE_ID || -z $REG_PROJECT_ID || -z $REG_REGION_ID || -z $REG_REGISTRY_ID || -z $REG_CLOUDSQL_INSTANCE || -z $REG_DBURI ]]; then
	cat <<-EOF
		usage: $0 <credentials-file> <appliance-id> [<appliance-uuid>]
		Must also set environment variables (or source from reg file):
		    REG_PROJECT_ID=<name of gcp project>
		    REG_REGION_ID=<name of gcp region>
		    REG_REGISTRY_ID=<registry name>
		    REG_CLOUDSQL_INSTANCE=<cloudsql instance name>
		    REG_DBURI=<postgres uri>
	EOF
	exit 2
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
$CL_REG app new ${CLOUD_UUID:+-u $CLOUD_UUID} -d "$OUTPUT_DIR" "$APPLIANCE_ID"
