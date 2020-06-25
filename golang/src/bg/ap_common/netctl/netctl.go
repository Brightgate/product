/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package netctl

/*

#include <errno.h>
#include <unistd.h>
#include <stdlib.h>
#include <sys/socket.h>
#include <sys/ioctl.h>
#include <linux/if_vlan.h>
#include <linux/if.h>
#include <linux/sockios.h>
#include <string.h>
#include <sys/socket.h>

int
ioctlInit() {
	int fd;

	if ((fd = socket(AF_INET, SOCK_STREAM, 0)) < 0) {
		return -errno;
	}
	return fd;
}

char *
unixError(int err) {
	return strerror(err);
}

int
bridgeAdd(int fd, char *bridge) {
	if (ioctl(fd, SIOCBRADDBR, bridge) < 0) {
		return errno;
	}
	return 0;
}

int
bridgeDel(int fd, char *bridge) {
	if (ioctl(fd, SIOCBRDELBR, bridge) < 0) {
		return errno;
	}
	return 0;
}

int
bridgeAddIface(int fd, char *bridge, int iface) {
	struct ifreq args;

	strncpy(args.ifr_name, bridge, IFNAMSIZ);
	args.ifr_ifindex = iface;
	if (ioctl(fd, SIOCBRADDIF, &args) < 0) {
		return errno;
	}
	return 0;
}

int
bridgeDelIface(int fd, char *bridge, int iface) {
	struct ifreq args;

	strncpy(args.ifr_name, bridge, IFNAMSIZ);
	args.ifr_ifindex = iface;
	if (ioctl(fd, SIOCBRDELIF, &args) < 0) {
		return errno;
	}
	return 0;
}

int
vlanAdd(int fd, char *iface, int vlan) {
	struct vlan_ioctl_args args;
	int err = 0;

	strncpy(args.device1, iface, IFNAMSIZ);
	args.u.VID = vlan;
	args.cmd = ADD_VLAN_CMD;
	err = ioctl(fd, SIOCSIFVLAN, &args);

	return err < 0 ? errno : 0;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"log"
	"net"
	"syscall"
	"unsafe"

	"github.com/vishvananda/netlink"
)

const (
	linkAdd = iota
	linkDel
	linkUp
	linkDown
	linkAddrAdd
	linkFlush
)

var (
	ioctlFD C.int

	// ErrNoDevice indicates that the requested network device wasn't found
	ErrNoDevice = errors.New("no such device")
)

func unixErrStr(err C.int) string {
	return C.GoString(C.unixError(err))
}

func getIfaceIdx(iface string) (int, error) {
	i, err := net.InterfaceByName(iface)
	if err != nil {
		return -1, fmt.Errorf("looking for %s: %v", iface, err)
	}

	return i.Index, nil
}

type wireguard struct {
	netlink.LinkAttrs
}

func (w wireguard) Attrs() *netlink.LinkAttrs {
	return &w.LinkAttrs
}

func (w wireguard) Type() string {
	return "wireguard"
}

// VlanAdd -> vconfig add <iface> <vlan>
func VlanAdd(iface string, vlan int) error {
	var err error

	cstr := C.CString(iface)
	defer C.free(unsafe.Pointer(cstr))

	if len(iface) > netlink.IFNAMSIZ {
		err = fmt.Errorf("invalid interface")
	} else if x := C.vlanAdd(ioctlFD, cstr, C.int(vlan)); x != 0 {
		err = fmt.Errorf("adding vlan (%s, %d): %s", iface, vlan,
			unixErrStr(x))
	}

	return err
}

func linkOp(name string, addr *netlink.Addr, op int) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return ErrNoDevice
		}
		return fmt.Errorf("LinkByName(%s): %v", name, err)
	}

	switch op {
	case linkDel:
		if err = netlink.LinkDel(link); err != nil {
			err = fmt.Errorf("LinkDel(%s): %v", name, err)
		}
	case linkUp:
		if err = netlink.LinkSetUp(link); err != nil {
			err = fmt.Errorf("LinkSetUp(%s): %v", name, err)
		}
	case linkDown:
		if err = netlink.LinkSetDown(link); err != nil {
			err = fmt.Errorf("LinkSetDown(%s): %v", name, err)
		}
	case linkAddrAdd:
		if err = netlink.AddrAdd(link, addr); err != nil {
			err = fmt.Errorf("AddrAdd(%s): %v", name, err)
		}
	case linkFlush:
		list, _ := netlink.AddrList(link, netlink.FAMILY_ALL)
		for _, addr := range list {
			if xerr := netlink.AddrDel(link, &addr); xerr != nil {
				err = fmt.Errorf("AddrDel(%s): %v", name, err)
			}
		}
	default:
		err = fmt.Errorf("unknown link op: %d", op)
	}

	return err
}

// LinkUp -> ip link set up <name>
func LinkUp(name string) error {
	return linkOp(name, nil, linkUp)
}

// LinkDown -> ip link set down <name>
func LinkDown(name string) error {
	return linkOp(name, nil, linkDown)
}

// LinkAddWireguard -> ip link add dev <name> type wireguard
func LinkAddWireguard(name string) error {
	var err error

	link := &wireguard{netlink.LinkAttrs{Name: name, TxQLen: 1000}}

	if err = netlink.LinkAdd(link); err != nil {
		err = fmt.Errorf("LinkAdd(%s): %v", name, err)
	}
	return err
}

// LinkDelete -> ip link del <name>
func LinkDelete(name string) error {
	return linkOp(name, nil, linkDel)
}

// AddrAdd -> ip addr add <addr> dev <name>
func AddrAdd(name, addr string) error {
	ip, ipnet, err := net.ParseCIDR(addr)
	if err != nil {
		if ip, ipnet, err = net.ParseCIDR(addr + "/32"); err != nil {
			return fmt.Errorf("invalid address %s: %v", addr, err)
		}
	}

	// through trial and error, it appears that netlink is expecting the
	// IPNet struct to contain the real IP address to set, not the "network
	// number" ParseCIDR returns.
	arg := netlink.Addr{
		IPNet: &net.IPNet{IP: ip, Mask: ipnet.Mask},
	}
	return linkOp(name, &arg, linkAddrAdd)
}

// AddrFlush -> ip addr flush dev <name>
func AddrFlush(name string) error {
	return linkOp(name, nil, linkFlush)
}

// RouteAdd -> ip route add <route> dev <bridge>
func RouteAdd(route, bridge string) error {
	_, cidr, err := net.ParseCIDR(route)
	if err != nil {
		err = fmt.Errorf("invalid route %s: %v", route, err)

	} else if x, err := getIfaceIdx(bridge); err == nil {
		rt := netlink.Route{
			LinkIndex: x,
			Dst:       cidr,
		}

		if err = netlink.RouteAdd(&rt); err != nil {
			err = fmt.Errorf("RouteAdd(%s): %v", route, err)
		}
	}

	return err

}

// RouteDel -> ip route del <route>
func RouteDel(route string) error {
	_, cidr, err := net.ParseCIDR(route)
	if err != nil {
		err = fmt.Errorf("invalid route %s: %v", route, err)
	} else {
		rt := netlink.Route{
			Dst: cidr,
		}

		err = netlink.RouteDel(&rt)
		if err == syscall.ESRCH {
			err = nil
		} else if err != nil {
			err = fmt.Errorf("RouteDel(%s): %v", route, err)
		}
	}

	return err
}

// BridgeCreate -> brctl addbr <name>
func BridgeCreate(name string) error {
	cstr := C.CString(name)
	defer C.free(unsafe.Pointer(cstr))

	if err := C.bridgeAdd(ioctlFD, cstr); err != 0 {
		return fmt.Errorf("bridgeAdd(%s): %s", name, unixErrStr(err))
	}

	return nil
}

// BridgeDestroy -> brctl delbr <name>
func BridgeDestroy(name string) error {
	var err error

	cstr := C.CString(name)
	defer C.free(unsafe.Pointer(cstr))

	if cErr := C.bridgeDel(ioctlFD, cstr); cErr != 0 {
		if cErr == C.int(syscall.ENXIO) {
			err = ErrNoDevice
		} else {
			err = fmt.Errorf("bridgeDel(%s): %s", name,
				unixErrStr(cErr))
		}
	}

	return err
}

// BridgeAddIface -> brctl addif <bridge> <iface>
func BridgeAddIface(bridge, iface string) error {
	idx, err := getIfaceIdx(iface)
	if err != nil {
		return fmt.Errorf("looking for %s: %v", iface, err)
	}

	cstr := C.CString(bridge)
	defer C.free(unsafe.Pointer(cstr))

	if err := C.bridgeAddIface(ioctlFD, cstr, C.int(idx)); err != 0 {
		return fmt.Errorf("bridgeAddIface(%s, %s): %s", bridge, iface,
			unixErrStr(err))
	}

	return nil
}

// BridgeDelIface -> brctl delif <bridge> <iface>
func BridgeDelIface(bridge, iface string) error {
	idx, err := getIfaceIdx(iface)
	if err != nil {
		return fmt.Errorf("looking for %s: %v", iface, err)
	}

	cstr := C.CString(bridge)
	defer C.free(unsafe.Pointer(cstr))

	if err := C.bridgeDelIface(ioctlFD, cstr, C.int(idx)); err != 0 {
		return fmt.Errorf("bridgeDelIface(%s, %s): %s", bridge, iface,
			unixErrStr(err))
	}

	return nil
}

func init() {
	ioctlFD = C.ioctlInit()
	if ioctlFD < 0 {
		log.Fatalf("unable to perform network ioctls: %s",
			unixErrStr(ioctlFD))
	}
}
