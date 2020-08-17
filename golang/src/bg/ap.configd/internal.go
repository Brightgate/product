/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"context"
	"time"

	"bg/common/cfgapi"
	"bg/common/cfgmsg"
)

type internalConfig struct {
	level cfgapi.AccessLevel
}

type cmdStatus struct {
	rval string
	err  error
}

func (c *cmdStatus) Status(ctx context.Context) (string, error) {
	return c.rval, c.err
}

func (c *cmdStatus) Wait(ctx context.Context) (string, error) {
	return c.Status(ctx)
}

func (c *cmdStatus) Cancel(ctx context.Context) error {
	panic("unsupported call")
}

func (c *internalConfig) HandleChange(path string, handler func([]string, string,
	*time.Time)) error {
	panic("unsupported call")
}

func (c *internalConfig) HandleDelete(path string, handler func([]string)) error {
	panic("unsupported call")
}

func (c *internalConfig) HandleExpire(path string, handler func([]string)) error {
	panic("unsupported call")
}

func (c *internalConfig) Ping(ctx context.Context) error {
	return nil
}

func (c *internalConfig) ExecuteAt(ctx context.Context, ops []cfgapi.PropertyOp,
	level cfgapi.AccessLevel) cfgapi.CmdHdl {

	rval := &cmdStatus{}

	if len(ops) != 0 {
		query, err := cfgapi.PropOpsToQuery(ops)
		if err == nil {
			query.Level = int32(level)
			response := processOneEvent(query)
			if response.Response != cfgmsg.ConfigResponse_OK {
				_, rval.err = cfgapi.ParseConfigResponse(response)
			} else if ops[0].Op == cfgapi.PropGet {
				rval.rval = response.Value
			}
		}
	}

	return rval
}

func (c *internalConfig) Execute(ctx context.Context, ops []cfgapi.PropertyOp) cfgapi.CmdHdl {
	return c.ExecuteAt(ctx, ops, c.level)
}

func (c *internalConfig) Close() {
}

// NewInternalHdl returns a handle that can be used to execute cfgapi operations
// within ap.configd.
func NewInternalHdl() *cfgapi.Handle {
	cfg := internalConfig{
		level: cfgapi.AccessInternal,
	}
	hdl := cfgapi.NewHandle(&cfg)
	return hdl
}

