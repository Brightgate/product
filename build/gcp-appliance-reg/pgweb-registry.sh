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

#
# This script is a developer aid-- it stands up a pgweb (a postgres web viewer)
# instance for the given repository, allowing you to browse the contents.
#

pdir=$(dirname "$0")
source "$pdir/common.sh"

pgweb_url="https://github.com/sosedoff/pgweb/releases/download/v0.11.0/pgweb_linux_amd64.zip"
pgweb_sum="553c9eb8c7e35c53af67f93ec5409130f62bfb3f2c0022a41732ad63e646b98d pgweb_linux_amd64.zip"

[[ -n $1 ]] || fatal "usage: $0 <gcp-cred-file>"
CRED_FILE=$1

[[ -n $REG_DBURI ]] || fatal "need to set \$REG_DBURI"

if [[ ! -f $pdir/pgweb_linux_amd64 ]]; then
	echo "downloading pgweb binary to $pdir/"
	dir=$(mktemp -d)
	(cd "$dir" &&
	    curl --progress-bar -L -O "$pgweb_url" &&
	    echo "$pgweb_sum" | sha256sum --check &&
	    unzip pgweb_linux_amd64.zip) ||
		fatal "couldn't get pgweb"
	cp $dir/pgweb_linux_amd64 $pdir/pgweb_linux_amd64
	rm -fr "$dir"
fi

start_cloudsql_proxy "$CRED_FILE" "${REG_PROJECT_ID}:${REG_REGION_ID}:${REG_CLOUDSQL_INSTANCE}"

trap stop_cloudsql_proxy EXIT
sleep 3

read -r -s -p "Postgres database password [user=postgres]: " PASSWORD

# DATABASE_URL is understood by pgweb
export DATABASE_URL="$REG_DBURI&password=$PASSWORD"

exec $pdir/pgweb_linux_amd64 --bind=0.0.0.0 $*
