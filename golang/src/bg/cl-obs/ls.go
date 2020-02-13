//
// COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"

	"bg/base_msg"
	"bg/cl-obs/sentence"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func printDHCPOptions(w io.Writer, do []*base_msg.DHCPOptions) {
	var params []byte
	var vendor []byte

	for o := range do {
		if len(do[o].ParamReqList) > 0 {
			params = do[o].ParamReqList
			break
		}
	}
	for o := range do {
		if len(do[o].VendorClassId) > 0 {
			vendor = do[o].VendorClassId
			break
		}
	}

	fmt.Fprintf(w, "  [DHCP] options = %v %v\n", params, string(vendor))
}

func printNetEntity(w io.Writer, ne *base_msg.EventNetEntity) {
	fmt.Fprintf(w, "  [Entity] %v\n", ne)
}

func printNetRequests(w io.Writer, nr []*base_msg.EventNetRequest) {
	for i := range nr {
		fmt.Fprintf(w, "  [Requests] %d %v\n", i, nr[i])
	}
}

func printNetScans(w io.Writer, ns []*base_msg.EventNetScan) {
	for i := range ns {
		fmt.Fprintf(w, "  [Scans] %d %v\n", i, ns[i])
	}
}

func printNetListens(w io.Writer, nl []*base_msg.EventListen) {
	for i := range nl {
		fmt.Fprintf(w, "  [Listens] %d %v\n", i, nl[i])
	}
}

func printDeviceInfo(w io.Writer, B *backdrop, di *base_msg.DeviceInfo, dmac string, detailed bool) {
	dns := "-"
	if di.DnsName != nil {
		dns = *di.DnsName
	}

	dhcpn := "-"
	if di.DhcpName != nil {
		dhcpn = *di.DhcpName
	}

	hw, err := net.ParseMAC(dmac)
	if err != nil {
		fmt.Fprintf(w, "** couldn't parse MAC '%s': %v\n", dmac, err)
		return
	}

	fmt.Fprintf(w, "%18s %26s %26s %4d\n", hw.String(), dns, dhcpn, 0)

	if hw.String() != "" {
		fmt.Fprintln(w, getMfgFromMAC(B, hw.String()))
	}

	if detailed {
		fmt.Fprintln(w, "{{")
		printDHCPOptions(w, di.Options)
		printNetEntity(w, di.Entity)
		printNetRequests(w, di.Request)
		printNetScans(w, di.Scan)
		printNetListens(w, di.Listen)
		fmt.Fprintln(w, "}}")
	}
}

func getContentStatus(di *base_msg.DeviceInfo) string {
	entityPresent := "-"
	dhcpPresent := "-"
	dnsRecordsPresent := "-"
	networkScanPresent := "-"
	listenPresent := "-"

	if di.Entity != nil {
		entityPresent = "E"
	}

	if len(di.Options) > 0 {
		dhcpPresent = "D"
	}

	if len(di.Request) > 0 {
		dnsRecordsPresent = "N"
	}

	if len(di.Scan) > 0 {
		networkScanPresent = "S"
	}

	if len(di.Listen) > 0 {
		networkScanPresent = "L"
	}

	return fmt.Sprintf("%s%s%s%s%s", entityPresent, dhcpPresent,
		dnsRecordsPresent, networkScanPresent, listenPresent)
}

func lsByUUID(u string, details bool) error {
	var seen map[string]int

	rows, err := _B.db.Queryx("SELECT * FROM inventory WHERE site_uuid = ? ORDER BY inventory_date DESC;", u)
	if err != nil {
		return errors.Wrap(err, "inventory Queryx error")
	}

	seen = make(map[string]int)

	for rows.Next() {
		var ri RecordedInventory

		err = rows.StructScan(&ri)
		if err != nil {
			slog.Errorf("struct scan failed : %v", err)
			continue
		}

		di, err := _B.store.ReadTuple(context.Background(), ri.Tuple())
		if err != nil {
			slog.Errorf("couldn't get DeviceInfo %s: %v", ri.Tuple(), err)
			continue
		}

		content := getContentStatus(di)
		if content == "----" && !details {
			continue
		}

		seen[ri.DeviceMAC] = seen[ri.DeviceMAC] + 1
		if seen[ri.DeviceMAC] > 1 {
			continue
		}

		fmt.Printf("-- %v %v\n",
			ri.DeviceMAC, getMfgFromMAC(&_B, ri.DeviceMAC))

		fmt.Printf("insert or replace into training (dgroup_id, site_uuid, device_mac, unix_timestamp) values (0, \"%s\", \"%s\", \"%s\");\n", ri.SiteUUID, ri.DeviceMAC, ri.UnixTimestamp)

		// Display deviceInfo if verbose.
		if details {
			printDeviceInfo(os.Stdout, &_B, di, ri.DeviceMAC, true)
		}

	}

	rows.Close()

	return nil
}

func lsByMac(m string, details bool, redundant bool) error {
	rows, err := _B.db.Queryx("SELECT * FROM inventory WHERE device_mac = ? ORDER BY inventory_date DESC;", m)

	if err != nil {
		return errors.Wrap(err, "inventory Queryx error")
	}

	sent := sentence.New()

	if !redundant {
		fmt.Printf("[omitting redundant inventory records; use --redundant to see them]\n")
	}
	for rows.Next() {
		var ri RecordedInventory

		err = rows.StructScan(&ri)
		if err != nil {
			slog.Errorf("struct scan failed : %v", err)
			continue
		}

		localSent := sentence.NewFromString(ri.BayesSentence)

		dupe := sent.AddString(ri.BayesSentence)
		if !redundant && dupe {
			continue
		}

		di, err := _B.store.ReadTuple(context.Background(), ri.Tuple())
		if err != nil {
			slog.Errorf("couldn't get DeviceInfo %s: %v", ri.Tuple(), err)
			continue
		}

		content := getContentStatus(di)

		fmt.Printf("-- %v %v %v %v\n",
			ri.DeviceMAC,
			getMfgFromMAC(&_B, ri.DeviceMAC),
			ri.InventoryDate.String(),
			content)

		fmt.Printf("insert or replace into training (dgroup_id, site_uuid, device_mac, unix_timestamp) values (0, \"%s\", \"%s\", \"%s\");\n", ri.SiteUUID, ri.DeviceMAC, ri.UnixTimestamp)
		// Display deviceInfo if verbose.
		if details {
			printDeviceInfo(os.Stdout, &_B, di, ri.DeviceMAC, true)
		}
		fmt.Printf("--    Record's sentence: %s\n", localSent)
		fmt.Printf("-- Accumulated sentence: %s\n\n", sent)
	}

	rows.Close()

	return nil
}

func lsSub(cmd *cobra.Command, args []string) error {
	// Each argument to the ls subcommand is a MAC address or site UUID/Name
	redundant, _ := cmd.Flags().GetBool("redundant")
	verbose, _ := cmd.Flags().GetBool("verbose")

	for _, arg := range args {
		// is it a mac?
		if _, err := net.ParseMAC(arg); err == nil {
			err := lsByMac(arg, verbose, redundant)
			if err != nil {
				return err
			}
			continue
		}

		// else try to run the site matcher on it
		sites, err := matchSites(&_B, arg)
		if err != nil {
			return errors.Wrapf(err, "couldn't find a site name or UUID matching %s", arg)
		}
		for _, site := range sites {
			if err := lsByUUID(site.SiteUUID, verbose); err != nil {
				slog.Errorf("error listing %s: %v", site.SiteUUID, err)
			}
		}
	}
	return nil
}
