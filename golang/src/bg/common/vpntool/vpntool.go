/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package vpntool

import (
	"context"
	"fmt"
	"io/ioutil"
	"strings"

	"bg/common/cfgapi"
	"bg/common/wgconf"
	"bg/common/wgsite"

	"github.com/spf13/cobra"
)

var (
	config      *cfgapi.Handle
	siteHdl     *wgsite.Site
	pname       string
	keyFilePath string
)

// Add a new key for this user and print the associated config file to stdout.
func addKey(cmd *cobra.Command, args []string) error {
	user, _ := cmd.Flags().GetString("user")
	label, _ := cmd.Flags().GetString("label")
	ipaddr, _ := cmd.Flags().GetString("ip")
	file, _ := cmd.Flags().GetString("file")

	ctx := context.Background()

	if user == "" {
		return fmt.Errorf("must specify a user name")
	}

	res, err := siteHdl.AddKey(ctx, user, label, ipaddr)
	if err == nil {
		if file == "" {
			fmt.Printf(string(res.ConfData))
		} else {
			err = ioutil.WriteFile(file, res.ConfData, 0644)
		}
	}

	return err
}

func matches(key *wgconf.UserConf, id int, mac, label, public string) bool {
	match := true

	if id > 0 && id != key.ID {
		match = false
	}
	if mac != "" && !strings.EqualFold(mac, key.Mac) {
		match = false
	}
	if label != "" && !strings.EqualFold(label, key.Label) {
		match = false
	}
	if public != "" && !strings.EqualFold(public, key.Key.String()) {
		match = false
	}

	return match
}

// Remove one or more keys belonging to this user.  The key may be identified by
// ID#, label, mac address, public key, or "all"
func removeKey(cmd *cobra.Command, args []string) error {
	var label, mac, public string
	var cnt, id int
	var all, deleted bool

	ctx := context.Background()

	user, _ := cmd.Flags().GetString("user")
	if user == "" {
		return fmt.Errorf("must specify a user name")
	}
	conf, err := config.GetUser(user)
	if err != nil {
		return err
	}

	if label, _ = cmd.Flags().GetString("label"); label != "" {
		cnt++
	}
	if mac, _ = cmd.Flags().GetString("mac"); mac != "" {
		cnt++
	}
	if public, _ = cmd.Flags().GetString("public"); public != "" {
		cnt++
	}
	if id, _ = cmd.Flags().GetInt("id"); id >= 0 {
		cnt++
	}
	if all, _ = cmd.Flags().GetBool("all"); all {
		cnt++
	}
	if cnt < 1 {
		return fmt.Errorf("must specify at least one of " +
			"--label, --id, --mac, --public, or --all")
	}

	for _, key := range conf.WGConfig {
		if all || matches(key, id, mac, label, public) {
			err = siteHdl.RemoveKey(ctx, user, key.Mac,
				key.Key.String())
			if err != nil {
				return err
			}

			fmt.Printf("Deleted key %4d %18s %17v %32s %s\n",
				key.ID, key.Mac, key.IPAddress, key.Key,
				key.Label)
			deleted = true
		}
	}

	if !deleted {
		return fmt.Errorf("no matching key found")
	}
	return nil
}

// list all of the configured VPN keys
func listKeys(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("user")
	keys, err := siteHdl.GetKeys(name)
	if err != nil {
		return err
	}

	// Determine the widths of the 'username' and 'label' columns
	ulen := len("username")
	llen := len("label")
	for _, key := range keys {
		if l := len(key.User); l > ulen {
			ulen = l
		}
		if l := len(key.Label); l > llen {
			llen = l
		}
	}

	uhdr := fmt.Sprintf("%%%ds", ulen)
	lhdr := fmt.Sprintf("%%%ds", llen)

	fmt.Printf(uhdr+"  %4s  "+lhdr+"  %18s  %17s  %s\n",
		"username", "ID", "label", "assigned IP", "accounting mac",
		"public key")

	for _, key := range keys {
		fmt.Printf(uhdr+"  %4d  "+lhdr+"  %18s  %17s  %v\n",
			key.User, key.ID, key.Label, key.IPAddress,
			key.Mac, key.Key)
	}

	return nil
}

func checkServer(cmd *cobra.Command, args []string) error {
	warnings := siteHdl.SanityCheck(keyFilePath)
	if len(warnings) == 0 {
		fmt.Printf("ok\n")
	} else {
		fmt.Printf("Potential problems:\n")
		for _, w := range warnings {
			fmt.Printf("    %s\n", w)
		}
	}

	return nil
}

// SetKeyFile lets the caller provide the path to the locally stored server
// private key.  This file will only be available on the appliance - not in the
// cloud.
func SetKeyFile(path string) {
	keyFilePath = path
}

// Exec executes the bulk of the vpntool work.
func Exec(ctx context.Context, p string, hdl *cfgapi.Handle, args []string) error {
	var err error

	config = hdl
	pname = p

	rootCmd := &cobra.Command{
		Use: pname,
	}
	rootCmd.SetArgs(args)

	addCmd := &cobra.Command{
		Use:           "add",
		Short:         "add new vpn key",
		RunE:          addKey,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	addCmd.Flags().StringP("user", "u", "", "user")
	addCmd.Flags().String("ip", "", "assigned ip address")
	addCmd.Flags().StringP("label", "l", "", "user-friendly label")
	addCmd.Flags().StringP("file", "f", "", "file to write config into")
	rootCmd.AddCommand(addCmd)

	removeCmd := &cobra.Command{
		Use:           "remove",
		Short:         "remove a user's vpn key(s)",
		RunE:          removeKey,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	removeCmd.Flags().StringP("user", "u", "", "user")
	removeCmd.Flags().IntP("id", "i", -1, "remove a user's key by index number")
	removeCmd.Flags().StringP("label", "l", "", "remove a user's key by label")
	removeCmd.Flags().StringP("mac", "m", "", "mac address")
	removeCmd.Flags().StringP("public", "p", "", "public key")
	removeCmd.Flags().Bool("all", false, "remove all of a user's keys")
	rootCmd.AddCommand(removeCmd)

	listCmd := &cobra.Command{
		Use:           "list",
		Short:         "list all vpn keys",
		RunE:          listKeys,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	listCmd.Flags().StringP("user", "u", "", "list keys for a specific user")
	rootCmd.AddCommand(listCmd)

	checkCmd := &cobra.Command{
		Use:           "check",
		Short:         "sanity check the VPN server configuration",
		RunE:          checkServer,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.AddCommand(checkCmd)

	if siteHdl, err = wgsite.NewSite(config); err == nil {
		err = rootCmd.Execute()
	}
	return err
}

