/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package cfgmsg

import "fmt"

// OpName converts a ConfigOp_Operation value into a human-friendly string.
func OpName(op ConfigOp_Operation) (string, error) {
	var err error

	name, ok := ConfigOp_Operation_name[int32(op)]
	if !ok {
		err = fmt.Errorf("unknown operation: %d", int32(op))
	}

	return name, err
}
