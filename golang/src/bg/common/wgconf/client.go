/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package wgconf

import (
	"sync"
)

// Client contains the information needed for a WireGuard client to connect to
// the associated server, authenticate to it, and manipulate the local routing
// tables to forward the correct traffic to the server.
type Client struct {
	Server Server
	Endpoint
	sync.Mutex
}

// ToEndpoint extracts the local endpoint portion of a Client structure
func (c *Client) ToEndpoint() *Endpoint {
	return &c.Endpoint
}
