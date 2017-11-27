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

	leases := propertySearch("@/dhcp/leases")
	if leases == nil {
		log.Printf("V1 config file missing @/dhcp/leases")
		return nil
	}

	for _, lease := range leases.Children {
		var classNode, ipv4Node, dnsNode *pnode

		ipv4 := lease.Name
		macaddr := lease.Value
		log.Printf("Migrating the lease for %s / %s\n", ipv4, macaddr)

		client := propertyInsert("@/clients/" + macaddr)
		for _, node := range client.Children {
			switch node.Name {
			case "class":
				classNode = node
			case "ipv4":
				ipv4Node = node
			case "dns":
				dnsNode = node
			}
		}

		// Create the class property if it doesn't exist.  Otherwise,
		// leave it alone.
		if classNode == nil {
			classNode = propertyAdd(client, "class")
			classNode.Value = "unclassified"
		}

		// Create the ipv4 property if necessary.  Migrate the value
		// from the old lease into the client structure.
		if ipv4Node == nil {
			ipv4Node = propertyAdd(client, "ipv4")
		}
		ipv4Node.Value = ipv4
		ipv4Node.Expires = lease.Expires

		// The "dns" property is now called "dhcp_name"
		if dnsNode != nil {
			dnsNode.Name = "dhcp_name"
		}
	}

	// Delete the obsolete @/dhcp/leases tree
	propertyDelete("@/dhcp/leases")

	return nil
}

func init() {
	addUpgradeHook(2, upgradeV2)
}
