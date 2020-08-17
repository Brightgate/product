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

	"bg/ap_common/netctl"
	"bg/common/wgconf"
)

func devUp(e *wgconf.Endpoint) error {
	_ = netctl.LinkDelete(e.Devname)

	if err := netctl.LinkAddWireguard(e.Devname); err != nil {
		return fmt.Errorf("creating %s: %v", e.Devname, err)
	}

	if err := netctl.AddrFlush(e.Devname); err != nil {
		return fmt.Errorf("flushing old addresses from %s: %v",
			e.Devname, err)
	}

	addr := e.IPAddress.IP.String()
	if err := netctl.AddrAdd(e.Devname, addr); err != nil {
		return fmt.Errorf("addrAdd: %v", err)
	}

	if err := netctl.LinkUp(e.Devname); err != nil {
		return fmt.Errorf("linkUp %s: %v", e.Devname, err)
	}

	for _, subnet := range e.Subnets {
		dst := subnet.String()

		if err := netctl.RouteAdd(dst, e.Devname); err != nil {
			return fmt.Errorf("routeAdd(%v, %s) %v", dst,
				e.Devname, err)
		}
	}

	return nil
}

func devDown(e *wgconf.Endpoint) error {
	if err := netctl.LinkDown(e.Devname); err != nil {
		fmt.Printf("linkDown %s failed: %v", e.Devname, err)
	}

	for _, subnet := range e.Subnets {
		dst := subnet.String()

		if err := netctl.RouteDel(dst); err != nil {
			fmt.Printf("routeAdd(%v, %s) failed: %v",
				dst, e.Devname, err)
		}
	}

	err := netctl.LinkDelete(e.Devname)
	if err == netctl.ErrNoDevice {
		err = nil
	} else if err != nil {
		err = fmt.Errorf("LinkDelete(%s) failed: %v", e.Devname, err)
	}

	return err
}

