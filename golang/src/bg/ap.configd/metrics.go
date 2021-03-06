/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"fmt"

	"bg/common/cfgapi"
	"bg/common/cfgmsg"
	"bg/common/cfgtree"
)

var (
	mTree *cfgtree.PTree
)

func metricsGet(prop string) (*string, error) {
	rval, err := mTree.Get(prop)
	err = xlateError(err)

	return rval, err
}

func metricsSet(prop string, val string, add bool) error {
	var err error

	if val == "" {
		err = fmt.Errorf("no value supplied")
	} else if add {
		err = mTree.Add(prop, val, nil)
	} else {
		err = mTree.Set(prop, val, nil)
	}

	return xlateError(err)
}

func metricsDel(prop string) error {
	_, err := mTree.Delete(prop)

	return xlateError(err)
}

func metricsPropHandler(query *cfgmsg.ConfigQuery) (*string, error) {
	var rval *string
	var err error

	level := cfgapi.AccessLevel(query.Level)

	mTree.ChangesetInit()
	for _, op := range query.Ops {
		var prop, val string

		if prop, val, _, err = getParams(op); err != nil {
			break
		}

		switch op.Operation {
		case cfgmsg.ConfigOp_GET:
			if err = validateProp(prop); err == nil {
				rval, err = metricsGet(prop)
			}

		case cfgmsg.ConfigOp_CREATE, cfgmsg.ConfigOp_SET:
			if err = validatePropVal(prop, val, level); err == nil {
				add := (op.Operation == cfgmsg.ConfigOp_CREATE)
				err = metricsSet(prop, val, add)
			}

		case cfgmsg.ConfigOp_DELETE:
			if err = validatePropDel(prop, level); err == nil {
				err = metricsDel(prop)
			}

		case cfgmsg.ConfigOp_ADDVALID:
			if level < cfgapi.AccessInternal {
				err = fmt.Errorf("must be internal")
			} else {
				node, rerr := newVnode(prop)
				if rerr != nil {
					err = rerr
				} else {
					node.valType = "string"
					node.level = cfgapi.AccessInternal
				}
			}

		case cfgmsg.ConfigOp_PING:
		// no-op

		default:
			name, _ := cfgmsg.OpName(op.Operation)
			err = fmt.Errorf("%s not supported for @/metrics", name)
		}

		if err != nil {
			break
		}
	}

	if err == nil {
		mTree.ChangesetCommit()
	} else {
		mTree.ChangesetRevert()
	}

	return rval, err
}

func init() {
	mTree, _ = cfgtree.NewPTree("@/metrics/", nil)
}

