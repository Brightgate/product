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
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/wgctl"
	"bg/common/cfgapi"
	"bg/common/wgconf"

	"github.com/spf13/cobra"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var (
	wgVerbose = false
)

func wgPropBase(idx int) string {
	return "@/network/vpn/client/" + strconv.Itoa(idx) + "/wg/"
}

func wgPolicyBase(idx int) string {
	return "@/policy/site/vpn/client/" + strconv.Itoa(idx)
}

func wgDebug(format string, a ...interface{}) {
	if wgVerbose {
		fmt.Printf(format, a...)
	}
}

func wgUp(cmd *cobra.Command, args []string) error {
	clientIdx, _ := cmd.Flags().GetInt("idx")

	c, err := wgctl.GetClient(config, clientIdx)
	if err != nil {
		err = fmt.Errorf("initializing client %d: %v", clientIdx, err)

	} else if err = wgctl.ClientDevUp(c); err != nil {
		err = fmt.Errorf("plumbing WireGuard device %s: %v",
			c.Devname, err)

	} else if err = wgctl.InstallConfig(c); err != nil {
		err = fmt.Errorf("configuuring WireGuard device %s: %v",
			c.Devname, err)
	}

	return err
}

func wgDown(cmd *cobra.Command, args []string) error {
	clientIdx, _ := cmd.Flags().GetInt("idx")

	c, err := wgctl.GetClient(config, clientIdx)
	if err != nil {
		err = fmt.Errorf("initializing client %d: %v", clientIdx, err)
	} else {
		err = wgctl.ClientDevDown(c)
	}

	return err
}

func wgImport(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString("file")
	if file == "" {
		return fmt.Errorf("must specify a config file")
	}

	idx, err := cmd.Flags().GetInt("idx")
	if err != nil {
		return err
	}

	r, err := os.Open(file)
	if err != nil {
		return err
	}
	defer r.Close()

	keys, err := wgconf.ParseClientConfig(r)
	if err == nil {
		base := wgPropBase(idx)
		props := make(map[string]string)
		for key, val := range keys {
			props[base+key] = val
		}
		props[wgPolicyBase(idx)+"/enabled"] = "true"
		err = config.CreateProps(props, nil)
	}
	return err
}

func wgList(cmd *cobra.Command, args []string) error {
	all, err := config.GetProps("@/network/vpn/client")
	if err != nil {
		return err
	}

	fmt.Printf("%6s\t%20s\t%s\n", "id", "client address", "server address")
	for id, root := range all.Children {
		wg, _ := root.GetChild("wg")
		if wg == nil {
			// skip any non-WireGuard configs
			continue
		}
		addr, _ := wg.GetChildString("server_address")
		port, _ := wg.GetChildInt("server_port")
		client, _ := wg.GetChildString("client_address")
		if addr == "" || client == "" || port == 0 {
			fmt.Printf("%6s <incomplete configuration>\n", id)
		} else {
			fmt.Printf("%6s\t%20s\t%s:%d\n", id, client, addr, port)
		}
	}
	return err
}

func toSize(bytes int) string {
	var unit string

	if bytes < 1000 {
		return fmt.Sprintf("%dB", bytes)
	}
	f := float64(bytes)

	for _, unit = range []string{"KB", "MB", "GB", "TB"} {
		if f = f / 1000; f < 1000 {
			break
		}
	}

	return fmt.Sprintf("%1.1f%s", f, unit)
}

func allowedIPs(set []net.IPNet) string {
	if len(set) == 0 {
		return "none"
	}

	all := make([]string, 0)
	for _, ip := range set {
		all = append(all, ip.String())
	}

	return strings.Join(all, ",")
}

func wgShow(cmd *cobra.Command, args []string) error {
	var nullKey wgtypes.Key

	devs, err := wgctl.GetDevices()
	if err == nil {
		for _, d := range devs {
			if d.PublicKey == nullKey {
				continue
			}

			fmt.Printf("interface: %s   public key: %s   port: %d\n",
				d.Name, d.PublicKey.String(), d.ListenPort)
			for _, p := range d.Peers {
				var when string

				if p.PublicKey == nullKey {
					continue
				}
				if t := p.LastHandshakeTime; !t.IsZero() {
					when = t.Format(time.Stamp)
				} else {
					when = "never"
				}
				fmt.Printf("    peer: %s\n", p.PublicKey)
				if p.Endpoint != nil {
					fmt.Printf("        endpt: %v\n", p.Endpoint)
				}
				fmt.Printf("        allowedIPs: %v\n",
					allowedIPs(p.AllowedIPs))
				fmt.Printf("        last handshake: %s\n", when)

				fmt.Printf("        tx: %d  rx: %d\n",
					p.TransmitBytes, p.ReceiveBytes)
			}
			fmt.Printf("\n")
		}
	}

	return err
}

func wgMain() {
	var err error

	config, err = apcfg.NewConfigd(nil, pname, cfgapi.AccessInternal)
	if err != nil {
		fmt.Printf("failed: %v\n", err)
		os.Exit(1)
	}

	rootCmd := &cobra.Command{
		Use: pname,
	}
	rootCmd.PersistentFlags().BoolVar(&wgVerbose, "v", false, "verbose")

	upCmd := &cobra.Command{
		Use:           "up",
		Short:         "open a WireGuard connection",
		RunE:          wgUp,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	upCmd.Flags().IntP("idx", "i", 0, "client index to bring up")
	rootCmd.AddCommand(upCmd)

	downCmd := &cobra.Command{
		Use:           "down",
		Short:         "close a WireGuard connection",
		RunE:          wgDown,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	downCmd.Flags().IntP("idx", "i", 0, "client index to bring down")
	rootCmd.AddCommand(downCmd)

	importCmd := &cobra.Command{
		Use:           "import",
		Short:         "import a WireGuard config file",
		RunE:          wgImport,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	importCmd.Flags().StringP("file", "f", "", "file to import")
	importCmd.Flags().IntP("idx", "i", 0, "client index to assign")
	rootCmd.AddCommand(importCmd)

	showCmd := &cobra.Command{
		Use:           "show",
		Short:         "show instantiated WireGuard connections",
		RunE:          wgShow,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.AddCommand(showCmd)

	listCmd := &cobra.Command{
		Use:           "list",
		Short:         "list configured WireGuard connections",
		RunE:          wgList,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.AddCommand(listCmd)

	if err = rootCmd.Execute(); err != nil {
		fmt.Printf("failed: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	addTool("ap-wg", wgMain)
}

