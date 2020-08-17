/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

