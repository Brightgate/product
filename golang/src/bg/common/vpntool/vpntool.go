/*
 * COPYRIGHT 2020 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

package vpntool

import (
	"context"
	"fmt"
	"io/ioutil"

	"bg/common/cfgapi"
	"bg/common/vpn"

	"github.com/spf13/cobra"
)

var (
	config *cfgapi.Handle
	pname  string
)

// Add a new key for this user and print the associated config file to stdout.
func addKey(cmd *cobra.Command, args []string) error {
	user, _ := cmd.Flags().GetString("user")
	label, _ := cmd.Flags().GetString("label")
	ipaddr, _ := cmd.Flags().GetString("ip")
	file, _ := cmd.Flags().GetString("file")

	if user == "" {
		return fmt.Errorf("must specify a user name")
	}

	conf, err := vpn.AddKey(user, label, ipaddr)
	if err == nil {
		if file == "" {
			fmt.Printf(string(conf))
		} else {
			err = ioutil.WriteFile(file, conf, 0644)
		}
	}

	return err
}

// Remove one or more keys belonging to this user.  The key may be identified by
// ID#, label, or "all"
func removeKey(cmd *cobra.Command, args []string) error {
	var label string
	var cnt, id int
	var all, deleted bool

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
	if id, _ = cmd.Flags().GetInt("id"); id >= 0 {
		cnt++
	}
	if all, _ = cmd.Flags().GetBool("all"); all {
		cnt++
	}
	if cnt != 1 {
		return fmt.Errorf("choose one of --label, --id, or --all")
	}

	for _, key := range conf.WGConfig {
		if all || id == key.ID || (label != "" && label == key.Label) {
			if err = vpn.RemoveKey(user, key.GetMac()); err != nil {
				return err
			}

			label := ""
			if len(key.Label) > 0 {
				label = "(" + key.Label + ")"
			}
			fmt.Printf("Deleted key %d %s\n", key.ID, label)
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
	keys, err := vpn.GetKeys(name)
	if err != nil {
		return err
	}

	// Determine the widths of the 'username' and 'label' columns
	ulen := len("username")
	llen := len("label")
	for _, key := range keys {
		if l := len(key.GetUser()); l > ulen {
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
		fmt.Printf(uhdr+"  %4d  "+lhdr+"  %18s  %17s  %s\n",
			key.GetUser(), key.ID, key.Label, key.WGAssignedIP,
			key.GetMac(), key.WGPublicKey)
	}

	return nil
}

// Exec executes the bulk of the vpntool work.
func Exec(ctx context.Context, p string, hdl *cfgapi.Handle, args []string) error {
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

	if err := vpn.Init(config); err != nil {
		return err
	}

	return rootCmd.Execute()
}
