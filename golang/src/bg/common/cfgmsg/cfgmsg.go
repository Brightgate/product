/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

