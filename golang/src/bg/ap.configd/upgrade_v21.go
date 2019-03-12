/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

// Rename a handful of properties to snake_case from other formats.
func upgradeV21() error {
	changes := map[string]string{
		"@/httpd/cookie-aes-key":     "@/httpd/cookie_aes_key",
		"@/httpd/cookie-hmac-key":    "@/httpd/cookie_hmac_key",
		"@/network/radiusAuthSecret": "@/network/radius_auth_secret",
	}
	for from, to := range changes {
		if node, err := propTree.GetNode(from); err == nil {
			slog.Infof("Moving %s -> %s", from, to)
			if err = node.Move(to); err != nil {
				return err
			}
		}
	}

	if users, _ := propTree.GetNode("@/users"); users != nil {
		for user := range users.Children {
			f := func(s string) string {
				return "@/users/" + user + "/" + s
			}
			changes := map[string]string{
				f("displayName"):      f("display_name"),
				f("selfProvisioning"): f("self_provisioning"),
				f("telephoneNumber"):  f("telephone_number"),
				f("userMD4Password"):  f("user_md4_password"),
				f("userPassword"):     f("user_password"),
			}
			for from, to := range changes {
				if node, err := propTree.GetNode(from); err == nil {
					slog.Infof("Moving %s -> %s", from, to)
					if err = node.Move(to); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func init() {
	addUpgradeHook(21, upgradeV21)
}
