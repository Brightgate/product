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

CRED_FILE=$1
PROJECT_ID=$2
REGISTRY_ID=$3

# In the GCP IoT Core beta, us-central1 is one of only three supported regions,
# and the only one in the US.  For now we hard code it.
REGION=us-central1

if [[ -z $CRED_FILE || ! -f $CRED_FILE || -z $REGISTRY_ID || -z $PROJECT_ID ]]; then
	echo "usage: $0 <credentials-file> <project-id> <registry-id>"
	exit 2
fi

gcloud auth activate-service-account --project="$PROJECT_ID" --key-file="$CRED_FILE"
SERVICE_ACCT=cloud-iot@system.gserviceaccount.com

EVENTS=iot-$REGISTRY_ID-events
gcloud beta pubsub topics create --quiet "$EVENTS"
gcloud beta pubsub topics add-iam-policy-binding "$EVENTS" \
	--member=serviceAccount:"$SERVICE_ACCT" --role=roles/pubsub.publisher

STATE="iot-$REGISTRY_ID-state"
gcloud beta pubsub topics create --quiet "$STATE"
gcloud beta pubsub topics add-iam-policy-binding "$STATE" \
	--member=serviceAccount:"$SERVICE_ACCT" --role=roles/pubsub.publisher

gcloud beta iot registries create "$REGISTRY_ID" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --event-pubsub-topic="$EVENTS" \
    --state-pubsub-topic="$STATE"

echo "---------- $REGISTRY_ID -------------------------------------"
gcloud beta iot registries describe "$REGISTRY_ID" --region "$REGION"
