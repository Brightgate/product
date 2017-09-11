/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"log"
)

//
// Capture information from the old @/dhcp/leases and migrate to the
// correct @/client/<macaddr> record.  We migrate the expiration time, but allow
// the modification time to float.
//
// "Name": "leases",
// "Modified": "2017-08-08T12:43:56.412636349-07:00",
// "Children": [
//     {
//         "Name": "192.168.136.43",
//         "Value": "b8:27:eb:79:27:22",
//         "Modified": "2017-08-08T12:43:56.264843257-07:00",
//         "Expires": "2017-08-08T12:53:56.263742945-07:00"
//     },

func upgradeV2() error {

	leases := cfg_property_parse("@/dhcp/leases", false)
	if leases == nil {
		log.Printf("V1 config file missing @/dhcp/leases")
		return nil
	}

	for _, lease := range leases.Children {
		var class_node, ipv4_node, dns_node *pnode

		ipv4 := lease.Name
		macaddr := lease.Value
		log.Printf("Migrating the lease for %s / %s\n", ipv4, macaddr)

		client := cfg_property_parse("@/clients/"+macaddr, true)
		for _, node := range client.Children {
			switch node.Name {
			case "class":
				class_node = node
			case "ipv4":
				ipv4_node = node
			case "dns":
				dns_node = node
			}
		}

		// Create the class property if it doesn't exist.  Otherwise,
		// leave it alone.
		if class_node == nil {
			class_node = propertyAdd(client, "class")
			class_node.Value = "unclassified"
		}

		// Create the ipv4 property if necessary.  Migrate the value
		// from the old lease into the client structure.
		if ipv4_node == nil {
			ipv4_node = propertyAdd(client, "ipv4")
		}
		ipv4_node.Value = ipv4
		ipv4_node.Expires = lease.Expires

		// The "dns" property is now called "dhcp_name"
		if dns_node != nil {
			dns_node.Name = "dhcp_name"
		}
	}

	// Delete the obsolete @/dhcp/leases tree
	propertyDelete("@/dhcp/leases")

	return nil
}

func init() {
	addUpgradeHook(2, upgradeV2)
}
