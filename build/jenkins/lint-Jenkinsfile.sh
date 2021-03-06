#!/bin/bash

#
# Linter for jenkins *declarative* pipeline DSL files.  Note that this will not
# work for scripted pipeline DSL files.
#
if [[ -z $1 ]]; then
	echo "Usage: $0 <file-to-lint>"
	exit 2
fi

JENKINS_URL=https://build0.b10e.net/
# get magic token
JENKINS_CRUMB=$(curl -s "$JENKINS_URL/crumbIssuer/api/xml?xpath=concat(//crumbRequestField,\":\",//crumb)")

exec curl -X POST -H $JENKINS_CRUMB \
    -F "jenkinsfile=<$1" \
    $JENKINS_URL/pipeline-model-converter/validate
