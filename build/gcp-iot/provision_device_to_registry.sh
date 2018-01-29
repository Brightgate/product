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

# Create files private by default
umask 077

OUTPUT_DIR=output_secrets

CRED_FILE=$1
PROJECT_ID=$2
REGISTRY_ID=$3
DEVICE_NAME=$4

REGION=us-central1

if [[ -z $CRED_FILE || ! -f $CRED_FILE || -z $REGISTRY_ID || -z $PROJECT_ID || -z $DEVICE_NAME ]]; then
	echo "usage: $0 <credentials-file> <project-id> <registry-id> <device-name>"
	exit 2
fi

DEVICE_UUID=$(uuidgen -r)

gcloud --project=$PROJECT_ID auth activate-service-account --key-file=$CRED_FILE

# gcloud doesn't have exit-code based tests.  argh.
exists=$(gcloud --project=$PROJECT_ID beta iot devices list \
	--device-ids="$DEVICE_NAME" \
	--registry="$REGISTRY_ID" \
	 --region="$REGION" \
	 --format='[no-heading](id)')
if [[ "$exists" == "$DEVICE_NAME" ]]; then
	echo "Looks like device $DEVICE_NAME already exists!"
	exit 1
fi

echo "Creating $OUTPUT_DIR/"
mkdir -p $OUTPUT_DIR || exit 1
cd $OUTPUT_DIR || exit 1

echo "Generating Key/Pair and Certificate for $DEVICE_NAME"
openssl req -x509 -nodes -newkey rsa:2048 -keyout "$DEVICE_NAME.rsa_private.pem" \
    -out "$DEVICE_NAME.rsa_cert.pem" -subj "/CN=unused"
ret=$?
if [[ $ret -ne 0 ]]; then
	echo "OpenSSL failed.  Exited $ret"
	exit 1
fi
# Replace the row separator (newline) with literal backslash-enn, as
# JSON cannot accomodate multiline strings.
PEMESCAPED=$(awk 1 ORS='\\n' "$DEVICE_NAME.rsa_private.pem")

echo "Adding $DEVICE_NAME to registry $REGISTRY_ID in region $REGION"
# XXX can add --public-key expiration time here later.
gcloud --project=$PROJECT_ID beta iot devices create "$DEVICE_NAME" \
	--region="$REGION" --registry="$REGISTRY_ID" \
	--public-key path="$DEVICE_NAME.rsa_cert.pem,type=RSA_X509_PEM" \
	--metadata=net_b10e_iot_cloud_uuid="$DEVICE_UUID"

echo "---------- $REGISTRY_ID -------------------------------------"
gcloud --project=$PROJECT_ID beta iot devices describe "$DEVICE_NAME" --registry "$REGISTRY_ID" --region "$REGION"

echo "-------------------------------------------------------------"
echo
cat <<EOF > $DEVICE_NAME.iotcore.secret.json
{
	"project": "$PROJECT_ID",
	"region": "$REGION",
	"registry": "$REGISTRY_ID",
	"device_id": "$DEVICE_NAME",
	"private_key": "$PEMESCAPED"
}
EOF

echo "All set.  Now provision $OUTPUT_DIR/$DEVICE_NAME.iotcore.secret.json" \
    "to the appliance:" \
    "/opt/com.brightgate/etc/secret/iotcore/iotcore.secret.json"
