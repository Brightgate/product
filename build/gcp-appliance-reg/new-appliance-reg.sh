#!/bin/bash
#
# Copyright 2018 Brightgate Inc.
#
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#


pdir=$(dirname "$0")
source "$pdir/common.sh"

CRED_FILE=$1
CLOUDSQL_INST=$2
PROJECT_ID=$3
REGION_ID=$4
REGISTRY_ID=$5

if [[ -z $CRED_FILE || ! -f $CRED_FILE || -z $CLOUDSQL_INST || -z $PROJECT_ID || -z $REGION_ID || -z $REGISTRY_ID ]]; then
	echo "usage: $0 <credentials-file> <cloudsql-instance> <project-id> <region-id> <registry-id>"
	exit 2
fi

function at_exit() {
	stop_cloudsql_proxy
}
trap at_exit EXIT

read -r -s -p "Postgres database password [user=postgres]: " PASSWORD
echo
[[ -n $PASSWORD ]] || fatal "must specify a password"

DBNAME="appliance-reg_${REGISTRY_ID}"
DBURI="postgres:///${DBNAME}?host=${SOCKDIR}/${PROJECT_ID}:${REGION_ID}:${CLOUDSQL_INST}&user=postgres"
DBURI_SECRET="${DBURI}&password=${PASSWORD}"

# spin up a proxy using the credentials we've been given
start_cloudsql_proxy "$CRED_FILE" "${PROJECT_ID}:${REGION_ID}:${CLOUDSQL_INST}"

EVCHANNELNAME="appliance-reg-${REGION_ID}-${REGISTRY_ID}-events"

GITROOT="$(git rev-parse --show-toplevel)"
SCHEMA_DIR="$GITROOT/golang/src/bg/cloud_models/appliancedb/schema"
SCHEMAS=$(LC_ALL=C cd "$SCHEMA_DIR" && ls -1 schema*.sql)
[[ -n $SCHEMAS ]] || fatal "No schema files found in $SCHEMA_DIR"

echo "-------------------------------------------------------------"
echo "Creating pubsub topic and database"

# We launch a subshell to use an exit trap.  This allows us to restore
# the old account after we're done using the service account.
(
	SVCACCT=$(gcloud auth list --filter=status:ACTIVE --format="value(account)")
	[[ -n $SVCACCT ]] || fatal "gcloud not initialized?  Couldn't get account name"

	gcloud auth activate-service-account --project="$PROJECT_ID" --key-file="$CRED_FILE"
	[[ $? -eq 0 ]] || fatal "failed to activate service account"

	NEWSVCACCT=$(gcloud auth list --filter=status:ACTIVE --format="value(account)")
	trap 'gcloud config set account $SVCACCT; gcloud auth revoke $NEWSVCACCT' EXIT

	gcloud beta pubsub topics create "$EVCHANNELNAME"
	[[ $? -eq 0 ]] || fatal "failed to create $EVCHANNELNAME"

	gcloud sql databases create "$DBNAME" --instance="$CLOUDSQL_INST"
	[[ $? -eq 0 ]] || fatal "failed to create $CLOUDSQL_INST -> $DBNAME"
)
[[ $? -eq 0 ]] || exit 1

echo "-------------------------------------------------------------"
echo "Loading schema files into database"
for schema in $SCHEMAS; do
	echo "Loading: $schema"
	psql -q -d "$DBURI_SECRET" -v ON_ERROR_STOP=1 -f "$SCHEMA_DIR/$schema"
	[[ $? -eq 0 ]] || fatal "psql failed!"
done

echo "-------------------------------------------------------------"
FILENAME="appliance-reg-${PROJECT_ID}-${REGION_ID}-${REGISTRY_ID}"
SHELLFILE="${FILENAME}.sh"
echo "Writing registry info to $SHELLFILE"

cat <<EOF > "$SHELLFILE"
REG_PROJECT_ID='$PROJECT_ID'; export REG_PROJECT_ID
REG_REGION_ID='$REGION_ID'; export REG_REGION_ID
REG_REGISTRY_ID='$REGISTRY_ID'; export REG_REGISTRY_ID
REG_CLOUDSQL_INSTANCE='$CLOUDSQL_INST'; export REG_CLOUDSQL_INSTANCE
REG_DBURI='$DBURI'; export REG_DBURI
REG_PUBSUB_EVENTS='$EVCHANNELNAME'; export REG_PUBSUB_EVENTS
EOF
[[ $? -eq 0 ]] || fatal "failed to write $SHELLFILE"

JSONFILE="${FILENAME}.json"
echo "Writing registry info to $JSONFILE"

cat <<EOF > "$JSONFILE"
{
    "project": "$PROJECT_ID",
    "region": "$REGION_ID",
    "registry": "$REGISTRY_ID",
    "cloudsql_instance": "$CLOUDSQL_INST",
    "dburi": "$DBURI",
    "pubsub": {
        "events": "$EVCHANNELNAME"
    }
}
EOF
[[ $? -eq 0 ]] || fatal "failed to write $JSONFILE"

echo "-------------------------------------------------------------"
# strip newlines for presentation
SCHEMAS=${SCHEMAS//$'\n'/ }
cat <<EOF
Created pubsub topic: projects/$PROJECT_ID/topics/$EVCHANNELNAME
    Created registry: projects/$PROJECT_ID/locations/$REGION_ID/registries/$REGISTRY_ID
   CloudSQL instance: $CLOUDSQL_INST
        Registry URI: $DBURI
      Loaded Schemas: $SCHEMAS
  Shell include file: $SHELLFILE
      JSON info file: $JSONFILE
EOF

echo "-------------------------------------------------------------"

