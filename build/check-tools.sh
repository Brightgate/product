#!/bin/bash

NEEDED=

for TOOL in "$@"; do
	ii=$(dpkg-query -W --showformat='${db:Status-Abbrev}' "$TOOL" | sed 's/ //g')
	if [[ $ii != "ii" ]]; then
		echo "$0: Missing tools prerequisite package: $TOOL" 1>&2
		NEEDED+="$TOOL";
	fi
done

if [[ -n $NEEDED ]]; then
	echo "$0: To fix this problem: sudo apt-get install $NEEDED"
	exit 1
fi

dpkg --verify "$@"
if [[ $? -ne 0 ]]; then
	echo "$0: Failed package integrity check"
	exit 1
fi

exit 0
