#!/bin/bash

declare -a NEEDED
INSTALL=

while getopts "i" opt; do
	case $opt in
		i) INSTALL=1;;
		*) echo "Usage: $0 [-i] pkgname..."; exit 2;;
	esac
done
shift $((OPTIND -1))

for TOOL in "$@"; do
	ii=$(dpkg-query -W --showformat='${db:Status-Abbrev}' "$TOOL" | sed 's/ //g')
	if [[ $ii != "ii" ]]; then
		echo "$0: Missing tools prerequisite package: $TOOL" 1>&2
		NEEDED+=($TOOL)
	fi
done

if [[ -n ${NEEDED[@]} ]]; then
	if [[ -n $INSTALL ]]; then
		sudo apt-get install "${NEEDED[@]}"
		if [[ $? -ne 0 ]]; then
			echo "$0: Failed to install packages."
			exit 1
		fi
	else
		echo "$0: To fix this problem: sudo apt-get install" "${NEEDED[@]}"
		exit 1
	fi
else
	echo "All required packages already installed."
fi

dpkg --verify "$@"
if [[ $? -ne 0 ]]; then
	echo "$0: Failed package integrity check"
	exit 1
fi

exit 0
