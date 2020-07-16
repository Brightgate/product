/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package cfgapi

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Expired returns true if the property has an expiration time which has already
// passed.
func (n *PropertyNode) Expired() bool {
	if n.Expires == nil {
		return false
	}
	if n.Expires.After(time.Now()) {
		return false
	}

	return true
}

// GetStringSlice splits a node's value into comma-separated list, which
// is returned as a slice of strings
func (n *PropertyNode) GetStringSlice() []string {
	if n.Value == "" {
		return make([]string, 0)
	}

	return strings.Split(n.Value, ",")
}

// GetString returns the node's value as a string
func (n *PropertyNode) GetString() string {
	return n.Value
}

// GetIntSet splits a node's value into comma-separated list, which
// is returned as a set of integers.  If any value within the list is not a
// valid integer, it is excluded from the set and an error is returned.  The
// returned set will contain all valid integers.
func (n *PropertyNode) GetIntSet() (map[int]bool, error) {
	var err error

	set := make(map[int]bool)

	for _, val := range n.GetStringSlice() {
		intVal, serr := strconv.Atoi(val)
		if serr == nil {
			set[intVal] = true
		} else if err == nil {
			err = serr
		}
	}

	return set, err
}

// GetInt returns the node's value as an integer.  An error is returned if the
// value cannot be translated into an integer.
func (n *PropertyNode) GetInt() (int, error) {
	var rval int
	var err error

	if rval, err = strconv.Atoi(n.Value); err != nil {
		err = fmt.Errorf("malformed int property: %s", n.Value)
	}

	return rval, err
}

// GetUint returns the node's value as an unsigned integer.  An error is
// returned if the value cannot be translated into an integer.
func (n *PropertyNode) GetUint() (uint64, error) {
	var rval uint64
	var err error

	if rval, err = strconv.ParseUint(n.Value, 10, 64); err != nil {
		err = fmt.Errorf("malformed uint property: %s", n.Value)
	}
	return rval, err
}

// GetFloat64 returns the node's value as a float64.  An error is returned if
// the value cannot be translated into a float64.
func (n *PropertyNode) GetFloat64() (float64, error) {
	var rval float64
	var err error

	if rval, err = strconv.ParseFloat(n.Value, 64); err != nil {
		err = fmt.Errorf("malformed float64 property: %s", n.Value)
	}

	return rval, err
}

// GetBool returns the node's value as a bool.  An error is returned if the
// value cannot be translated into a bool.
func (n *PropertyNode) GetBool() (bool, error) {
	var rval bool
	var err error

	if rval, err = strconv.ParseBool(n.Value); err != nil {
		err = fmt.Errorf("malformed bool property: %s", n.Value)
	}
	return rval, err
}

// GetTime returns the node's value as a time.Time.  An error is returned if
// the value cannot be translated into a time.Time.
func (n *PropertyNode) GetTime() (*time.Time, error) {
	var rval *time.Time

	t, err := time.Parse(time.RFC3339, n.Value)
	if err != nil {
		err = fmt.Errorf("malformed time property: %s", n.Value)
	} else if !t.IsZero() {
		rval = &t
	}

	return rval, err
}

// GetIPv4 returns the node's value as net.IP.  An error is returned if the
// value cannot be translated into a net.IP.
func (n *PropertyNode) GetIPv4() (*net.IP, error) {
	if ip := net.ParseIP(n.Value); ip != nil {
		return &ip, nil
	}
	return nil, fmt.Errorf("Invalid ipv4 address")
}

// GetChild returns the PropertyNode for the node's child with the given name.
func (n *PropertyNode) GetChild(name string) (*PropertyNode, error) {
	var child *PropertyNode
	var err error

	if n == nil {
		err = ErrNoProp
	} else if child = n.Children[name]; child == nil {
		err = ErrNoProp
	} else if child.Expired() {
		err = ErrExpired
		child = nil
	}

	return child, err
}

// GetChildStringSlice looks for the 'name' child and returns its value as a
// slice of strings.
func (n *PropertyNode) GetChildStringSlice(name string) ([]string, error) {
	rval := make([]string, 0)

	c, err := n.GetChild(name)
	if err == nil {
		rval = c.GetStringSlice()
	}
	return rval, err
}

// GetChildString looks for the 'name' child and returns its value as a string.
func (n *PropertyNode) GetChildString(name string) (string, error) {
	var rval string

	c, err := n.GetChild(name)
	if err == nil {
		rval = c.Value
	}
	return rval, err
}

// GetChildIntSet looks for the 'name' child and returns its value as a set of
// integers.
func (n *PropertyNode) GetChildIntSet(name string) (map[int]bool, error) {
	var rval map[int]bool

	c, err := n.GetChild(name)
	if err == nil {
		rval, err = c.GetIntSet()
	}

	return rval, err
}

// GetChildInt looks for the 'name' child and returns its value as an integer.
func (n *PropertyNode) GetChildInt(name string) (int, error) {
	var rval int

	c, err := n.GetChild(name)
	if err == nil {
		rval, err = c.GetInt()
	}

	return rval, err
}

// GetChildUint looks for the 'name' child and returns its value as an unsigned
// integer.
func (n *PropertyNode) GetChildUint(name string) (uint64, error) {
	var rval uint64

	c, err := n.GetChild(name)
	if err == nil {
		rval, err = c.GetUint()
	}

	return rval, err
}

// GetChildFloat64 looks for the 'name' child and returns its value as a
// float64.
func (n *PropertyNode) GetChildFloat64(name string) (float64, error) {
	var rval float64

	c, err := n.GetChild(name)
	if err == nil {
		rval, err = c.GetFloat64()
	}

	return rval, err
}

// GetChildBool looks for the 'name' child and returns its value as a bool.
func (n *PropertyNode) GetChildBool(name string) (bool, error) {
	var rval bool

	c, err := n.GetChild(name)
	if err == nil {
		rval, err = c.GetBool()
	}

	return rval, err
}

// GetChildTime looks for the 'name' child and returns its value as a
// time.Time.
func (n *PropertyNode) GetChildTime(name string) (*time.Time, error) {
	var rval *time.Time

	c, err := n.GetChild(name)
	if err == nil {
		rval, err = c.GetTime()
	}

	return rval, err
}

// GetChildIPv4 looks for the 'name' child and returns its value as a net.IP.
func (n *PropertyNode) GetChildIPv4(name string) (*net.IP, error) {
	var rval *net.IP
	var err error

	c, err := n.GetChild(name)
	if err == nil {
		rval, err = c.GetIPv4()
	}

	return rval, err
}
