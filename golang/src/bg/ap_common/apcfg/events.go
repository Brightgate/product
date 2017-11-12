/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package apcfg

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"bg/ap_common/aputil"

	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
)

type changeMatch struct {
	match   *regexp.Regexp
	handler func([]string, string, *time.Time)
}

type delexpMatch struct {
	match   *regexp.Regexp
	handler func([]string)
}

// Opaque type representing a connection to ap.configd
func (c *APConfig) configEvent(raw []byte) {
	event := &base_msg.EventConfig{}
	proto.Unmarshal(raw, event)

	// Ignore messages without an explicit type
	if event.Type == nil {
		return
	}

	etype := *event.Type
	property := *event.Property
	path := strings.Split(property[2:], "/")
	value := *event.NewValue

	if etype == base_msg.EventConfig_CHANGE {
		expires := aputil.ProtobufToTime(event.Expires)
		for _, m := range c.changeHandlers {
			if m.match.MatchString(property) {
				m.handler(path, value, expires)
			}
		}
	} else if etype == base_msg.EventConfig_DELETE {
		for _, m := range c.deleteHandlers {
			if m.match.MatchString(property) {
				m.handler(path)
			}
		}
	} else if etype == base_msg.EventConfig_EXPIRE {
		for _, m := range c.expireHandlers {
			if m.match.MatchString(property) {
				m.handler(path)
			}
		}
	}
}

func (c *APConfig) handleCommon(path string) (re *regexp.Regexp, err error) {
	if c.broker == nil {
		err = fmt.Errorf("cannot subscribe to events without a broker")
	} else {
		re, err = regexp.Compile(path)

		if err == nil && !c.handling {
			c.broker.Handle(base_def.TOPIC_CONFIG, c.configEvent)
			c.handling = true
		}
	}

	return
}

// HandleChange registers a callback function for property change events
func (c *APConfig) HandleChange(path string, handler func([]string, string,
	*time.Time)) error {
	re, err := c.handleCommon(path)
	if err == nil {
		match := changeMatch{
			match:   re,
			handler: handler,
		}
		c.changeHandlers = append(c.changeHandlers, match)
	}
	return err
}

// HandleDelete registers a callback function for property delete events
func (c *APConfig) HandleDelete(path string, handler func([]string)) error {
	re, err := c.handleCommon(path)
	if err == nil {
		match := delexpMatch{
			match:   re,
			handler: handler,
		}
		c.deleteHandlers = append(c.deleteHandlers, match)
	}
	return err
}

// HandleExpire registers a callback function for property expiration events
func (c *APConfig) HandleExpire(path string, handler func([]string)) error {
	re, err := c.handleCommon(path)
	if err == nil {
		match := delexpMatch{
			match:   re,
			handler: handler,
		}
		c.expireHandlers = append(c.expireHandlers, match)
	}
	return err
}
