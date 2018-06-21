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

if [[ -z $CRED_FILE || ! -f $CRED_FILE || -z $APPLIANCE_ID || -z $REG_PROJECT_ID || -z $REG_REGION_ID || -z $REG_REGISTRY_ID || -z $REG_DBURI ]]; then
	cat <<-EOF
		usage: $0 <credentials-file> <appliance-id> [<appliance-uuid>]
		Must also set environment variables (or source from reg file):
		    REG_PROJECT_ID=<name of gcp project>
		    REG_REGION_ID=<name of gcp region>
		    REG_REGISTRY_ID=<registry name>
		    REG_DBURI=<postgres uri>
	EOF
	exit 2
fi

if [[ -z $CLOUD_UUID ]]; then
	CLOUD_UUID=$(uuidgen -r)
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

echo "Creating $OUTPUT_DIR/"
mkdir -p "$OUTPUT_DIR" || fatal "couldn't make $OUTPUT_DIR"
cd "$OUTPUT_DIR" || fatal "couldn't cd $OUTPUT_DIR"

echo "Generating Key/Pair and Certificate for $APPLIANCE_ID"
openssl req -x509 -nodes -newkey rsa:2048 -keyout "$APPLIANCE_ID.rsa_private.pem" \
    -out "$APPLIANCE_ID.rsa_cert.pem" -subj "/CN=unused"
[[ $? -eq 0 ]] || fatal "OpenSSL failed."

# Replace the row separator (newline) with literal backslash-enn, as
# JSON cannot accomodate multiline strings.
PRIVKEY_ESCAPED=$(awk -v ORS='\\n' '{print}' "$APPLIANCE_ID.rsa_private.pem")

PUBKEY=$(< "$APPLIANCE_ID.rsa_cert.pem")
echo "-------------------------------------------------------------"
echo "Recording appliance to SQL database; you may need to give a password."

cat <<EOF | psql --single-transaction -q -d "$REG_DBURI" -v ON_ERROR_STOP=1
INSERT INTO
  appliance_id_map
    (cloud_uuid, gcp_project, gcp_region, appliance_reg, appliance_reg_id)
  VALUES
    ('$CLOUD_UUID', '$REG_PROJECT_ID', '$REG_REGION_ID', '$REG_REGISTRY_ID', '$APPLIANCE_ID');
INSERT INTO
  appliance_pubkey
    (cloud_uuid, format, key)
  VALUES
    ('$CLOUD_UUID', 'RS256_X509', '$PUBKEY');
EOF
[[ $? -eq 0 ]] || fatal "psql failed."
echo "-------------------------------------------------------------"

OUTJSON=$APPLIANCE_ID.cloud.secret.json
cat <<EOF > "$OUTJSON"
{
	"project": "$REG_PROJECT_ID",
	"region": "$REG_REGION_ID",
	"registry": "$REG_REGISTRY_ID",
	"appliance_id": "$APPLIANCE_ID",
	"private_key": "$PRIVKEY_ESCAPED"
}
EOF
[[ $? -eq 0 ]] || fatal "write $OUTJSON failed."

echo "Summary:"
cat <<EOF
	Created appliance: projects/$REG_PROJECT_ID/locations/$REG_REGION_ID/registries/$REG_REGISTRY_ID/appliances/$APPLIANCE_ID
	       Cloud UUID: $CLOUD_UUID
             Secrets file: $OUTPUT_DIR/$OUTJSON
EOF

echo "-------------------------------------------------------------"
echo "Next, provision $OUTJSON to the appliance at:" \
    "/opt/com.brightgate/etc/secret/cloud/cloud.secret.json"
