/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package wgctl

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"bg/common/cfgapi"
	"bg/common/wgconf"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// GetDevices returns a list of all instantiated WireGuard devices, including
// those not created by the Brightgate stack.
func GetDevices() ([]*wgtypes.Device, error) {
	var rval []*wgtypes.Device

	client, err := wgctrl.New()
	if err != nil {
		err = fmt.Errorf("creating wgctrl client: %v", err)
	} else {
		defer client.Close()
		if rval, err = client.Devices(); err != nil {
			err = fmt.Errorf("fetching device list: %v", err)
		}
	}

	return rval, err
}

// ClientDevUp creates a client-side WireGuard device, adds its IP address, and
// adds the appropriate routes.
func ClientDevUp(c *wgconf.Client) error {
	return devUp(c.ToEndpoint())
}

// ClientDevDown removes any routes for the associated WireGuard device, and
// then removes the device.
func ClientDevDown(c *wgconf.Client) error {
	return devDown(c.ToEndpoint())
}

func checkFields(c *wgconf.Client) error {
	errs := make([]string, 0)

	if c.Server.Key == nil {
		errs = append(errs, "missing server public key")
	}
	if c.Server.Address == "" {
		errs = append(errs, "missing server address")
	} else if c.Server.IPAddress == nil {
		errs = append(errs, "unresolvable server address")
	}
	if c.Server.ListenPort == 0 {
		errs = append(errs, "missing server port")
	}
	if c.Key == nil {
		errs = append(errs, "missing client private key")
	}
	if c.IPAddress == nil {
		errs = append(errs, "missing client assigned IP")
	}

	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, ","))
	}

	return nil
}

// InstallConfig refreshes the WireGuard device with the latest settings in the
// Client structure.
func InstallConfig(c *wgconf.Client) error {
	if err := checkFields(c); err != nil {
		return fmt.Errorf("client config incomplete: %v", err)
	}
	if !c.Enabled {
		return fmt.Errorf("client config not enabled")
	}

	endpoint := net.UDPAddr{
		IP:   c.Server.IPAddress.IP,
		Port: c.Server.ListenPort,
	}
	keepalive := 25 * time.Second

	peer := wgtypes.PeerConfig{
		PublicKey:                   *c.Server.Key,
		Endpoint:                    &endpoint,
		PersistentKeepaliveInterval: &keepalive,
		AllowedIPs:                  c.Subnets,
	}

	config := wgtypes.Config{
		PrivateKey:   c.Key,
		ReplacePeers: true,
		Peers:        []wgtypes.PeerConfig{peer},
	}

	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("creating wgctrl client: %v", err)
	}
	defer client.Close()

	if err = client.ConfigureDevice(c.Devname, config); err != nil {
		return fmt.Errorf("configuring %s: %v", c.Devname, err)
	}

	return nil
}

// HandleSettingChange accepts a property name and new value, and updates the
// Client structure accordingly.  An error is returned if the new value is
// invalid for the specified property, or if the property name is unrecognized.
func HandleSettingChange(c *wgconf.Client, prop, newval string) error {
	var err error

	switch prop {
	case "server_address":
		err = c.Server.SetRemoteAddress(newval)
	case "server_port":
		err = c.Server.SetListenPort(newval)
	case "server_public":
		err = c.Server.SetKey(newval)
	case "client_address":
		err = c.SetIPAddress(newval)
	case "client_private":
		err = c.SetKey(newval)
	case "subnets":
		err = c.SetSubnets(newval)
	case "dns_domain", "dns_server":
		// no-op
	default:
		err = fmt.Errorf("unrecognized property: %s", prop)
	}
	return err
}

// GetClient retrieves a single client's properties from the config tree and
// uses them to construct a Client structure, which can be used to create and
// manage client-side WireGuard devices.
func GetClient(config *cfgapi.Handle, idx int) (*wgconf.Client, error) {
	props := []string{"server_address", "server_port", "server_public",
		"client_address", "client_private", "subnets"}

	c := &wgconf.Client{}
	c.Devname = "wgc" + strconv.Itoa(idx)

	path := "@/network/vpn/client/" + strconv.Itoa(idx) + "/wg/"
	root, err := config.GetProps(path)
	if err == cfgapi.ErrNoProp {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("fetching %s: %v", path, err)
	}
	if len(root.Children) == 0 {
		return nil, fmt.Errorf("%s is incomplete", props)
	}

	for _, prop := range props {
		p, ok := root.Children[prop]
		if !ok {
			return nil, fmt.Errorf("missing property: %s", prop)
		}
		if err = HandleSettingChange(c, prop, p.Value); err != nil {
			return nil, err
		}
	}

	path = "@/policy/site/vpn/client/" + strconv.Itoa(idx) + "/enabled"
	if x, _ := config.GetProp(path); strings.EqualFold(x, "true") {
		c.SetEnabled()
	} else {
		c.SetDisabled()
	}

	return c, nil
}

