/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package clcfg

import (
	"context"
	"log"
	"regexp"
	"strings"
	"time"

	rpc "bg/cloud_rpc"
	"bg/common/cfgapi"

	"github.com/golang/protobuf/ptypes"
)

type changeMatch struct {
	match   *regexp.Regexp
	handler func([]string, string, *time.Time)
}

type delexpMatch struct {
	match   *regexp.Regexp
	handler func([]string)
}

type monitorState struct {
	changeHandlers []changeMatch
	deleteHandlers []delexpMatch

	ctx        context.Context
	cancelFunc context.CancelFunc
}

func (c *Configd) monitorClose() {
	c.Lock()
	defer c.Unlock()
	if c.monState != nil {
		c.monState.cancelFunc()
		c.monState = nil
	}
}

func (c *Configd) monitor() {
	defer c.monitorClose()

	monitorCmd := &rpc.CfgFrontEndMonitor{
		Time:     ptypes.TimestampNow(),
		SiteUUID: c.uuid,
	}

	stream, err := c.client.Monitor(c.monState.ctx, monitorCmd)
	if err != nil {
		log.Printf("Failed to make MonitorStream: %v", err)
		return
	}

	m := c.monState
	for {
		resp, rerr := stream.Recv()
		if rerr != nil {
			log.Printf("lost connection to cl.configd: %v\n", err)
			break
		}

		if resp.Response == rpc.CfgFrontEndUpdate_FAILED {
			log.Printf("FetchStream() failed: %s\n", resp.Errmsg)
		}

		for _, u := range resp.Updates {
			prop := u.GetProperty()
			path := strings.Split(prop, "/")
			switch u.Type {
			case rpc.CfgUpdate_UPDATE:
				for _, h := range m.changeHandlers {
					if h.match.MatchString(prop) {
						var exp *time.Time

						val := u.GetValue()
						if e := u.GetExpires(); e != nil {
							tmp, _ := ptypes.Timestamp(e)
							exp = &tmp
						}
						h.handler(path, val, exp)
					}
				}
			case rpc.CfgUpdate_DELETE:
				for _, h := range m.deleteHandlers {
					if h.match.MatchString(prop) {
						h.handler(path)

					}
				}
			}
		}
	}
}

func (c *Configd) handleCommon(path string) (re *regexp.Regexp, err error) {
	re, err = regexp.Compile(path)
	if err == nil && c.monState == nil {

		ctx, cancelFunc := context.WithCancel(context.Background())
		c.monState = &monitorState{
			changeHandlers: make([]changeMatch, 0),
			deleteHandlers: make([]delexpMatch, 0),
			ctx:            ctx,
			cancelFunc:     cancelFunc,
		}
		go c.monitor()
	}
	return re, err
}

// HandleChange registers a callback function for property change events
func (c *Configd) HandleChange(path string, handler func([]string, string,
	*time.Time)) error {

	c.Lock()
	defer c.Unlock()

	re, err := c.handleCommon(path)
	if err == nil {
		m := c.monState

		match := changeMatch{
			match:   re,
			handler: handler,
		}
		m.changeHandlers = append(m.changeHandlers, match)
	}
	return err
}

// HandleDelete registers a callback function for property delete events
func (c *Configd) HandleDelete(path string, handler func([]string)) error {
	re, err := c.handleCommon(path)
	if err == nil {
		m := c.monState
		match := delexpMatch{
			match:   re,
			handler: handler,
		}
		m.deleteHandlers = append(m.deleteHandlers, match)
	}
	return err
}

// HandleExpire is not supported in the cloud
func (c *Configd) HandleExpire(path string, handler func([]string)) error {
	return cfgapi.ErrNotSupp
}
