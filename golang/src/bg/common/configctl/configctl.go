/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

package configctl

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bg/base_def"
	"bg/common/cfgapi"
	"bg/common/cfgtree"
	"bg/common/deviceid"

	"github.com/satori/uuid"
)

var (
	configd    *cfgapi.Handle
	pname      string
	nameToNode map[string]string
	nodeToName map[string]string
)

func timeStringShort(t time.Time) string {
	return t.Format("2006-01-02T15:04")
}

func timeString(t time.Time) string {
	return t.Format("2006-01-02T15:04:05")
}

func now() string {
	return timeString(time.Now())
}

func getAliases() {
	nameToNode = make(map[string]string)
	nodeToName = make(map[string]string)

	nodes, _ := configd.GetProps("@/nodes")
	for uuid, node := range nodes.Children {
		if a, ok := node.Children["name"]; ok {
			nameToNode[a.Value] = uuid
			nodeToName[uuid] = a.Value
		}
	}
}

// Convert @/nodes/<name>/property to @/nodes/<uuid>/property
func nodeAlias(in string) string {
	p := strings.Split(in, "/")
	if len(p) < 3 || p[1] != "nodes" {
		return in
	}

	name := p[2]
	if _, err := uuid.FromString(name); err == nil {
		return in
	}

	if nameToNode == nil {
		getAliases()
	}

	out := in
	if uuid, ok := nameToNode[name]; ok {
		p[2] = uuid
		out = strings.Join(p, "/")
	}

	return out
}

func getRings(cmd string, args []string) error {
	if len(args) != 0 {
		usage(cmd)
	}

	rings := configd.GetRings()
	clients := configd.GetClients()

	// Build a list of ring names, and sort them by the vlan ID of the
	// corresponding ring config.
	names := make([]string, 0)
	for r := range rings {
		names = append(names, r)
	}
	sort.Slice(names,
		func(i, j int) bool {
			ringI := rings[names[i]]
			ringJ := rings[names[j]]
			return ringI.Vlan < ringJ.Vlan
		})

	cnt := make(map[string]int)
	for _, c := range clients {
		cnt[c.Ring]++
	}

	fmt.Printf("%-10s %-5s %-4s %-9s %-18s %-7s\n",
		"ring", "vap", "vlan", "interface", "subnet", "clients")
	for _, name := range names {
		var vlan string

		ring := rings[name]
		if ring.Vlan >= 0 {
			vlan = strconv.Itoa(int(ring.Vlan))
		} else {
			vlan = "-"
		}

		fmt.Printf("%-10s %-5s %-4s %-9s %-18s %7d\n",
			name, ring.VirtualAP, vlan, ring.Bridge, ring.Subnet,
			cnt[name])
	}
	return nil
}

type statsPair struct {
	bytesRcvd uint64
	bytesSent uint64
}

type perClient struct {
	name string
	data map[string]*statsPair
}

func (sp *statsPair) String() string {
	if sp == nil {
		return fmt.Sprintf("%10s %10s", "bytesSent", "bytesRcvd")
	}
	return fmt.Sprintf("%10d %10d", sp.bytesSent, sp.bytesRcvd)
}

// Return a string of 'width' length, with 'text' in the center
func strCenter(text string, width int) string {
	left := (width - len(text)) / 2
	right := left
	if left+right+len(text) < width {
		left++
	}
	leftFmt := fmt.Sprintf("%%%-ds", left)
	rightFmt := fmt.Sprintf("%%%ds", right)
	return fmt.Sprintf(leftFmt+"%s"+rightFmt, "", text, "")
}

func printStats(s *perClient) {
	if s == nil {
		fmt.Printf("%-17s %21s   %21s   %21s   %21s\n",
			"name", strCenter("day", 21), strCenter("hour", 21),
			strCenter("minute", 21), strCenter("second", 21))
		s = &perClient{}
	}
	fmt.Printf("%17v %21v   %21v   %21v   %21v\n", s.name,
		s.data["day"], s.data["hour"],
		s.data["minute"], s.data["second"])
}

func getVal(data *cfgapi.PropertyNode, field string) (uint64, error) {
	var val uint64
	var err error

	if node, ok := data.Children[field]; ok {
		v := node.Value
		if val, err = strconv.ParseUint(v, 10, 64); err != nil {
			err = fmt.Errorf("bad %s (%s): %v", field, v, err)
		}
	} else {
		err = fmt.Errorf("missing %s", field)
	}

	return val, err
}

func buildStatsPair(data *cfgapi.PropertyNode) (*statsPair, error) {
	var p statsPair
	var err error

	if data == nil {
		err = fmt.Errorf("missing data")
	} else {
		v, verr := getVal(data, "bytes_rcvd")
		if verr == nil {
			p.bytesRcvd = v
		} else {
			err = verr
		}
		v, verr = getVal(data, "bytes_sent")
		if verr == nil {
			p.bytesSent = v
		} else {
			err = verr
		}
	}

	return &p, err
}

func buildStats(name string, data *cfgapi.PropertyNode) (*perClient, error) {
	var err error

	c := perClient{
		name: name,
		data: make(map[string]*statsPair),
	}

	for _, u := range []string{"day", "hour", "minute", "second"} {
		p, perr := buildStatsPair(data.Children[u])
		if perr != nil {
			err = perr
		} else {
			c.data[u] = p
		}
	}

	return &c, err
}

func fetchStats(mac string) ([]*perClient, error) {
	var c map[string]*cfgapi.PropertyNode
	var node *cfgapi.PropertyNode
	var err error

	rval := make([]*perClient, 0)
	if mac == "" {
		node, err = configd.GetProps("@/metrics/clients")
		if err == nil {
			c = node.Children
		}
	} else {
		node, err = configd.GetProps("@/metrics/clients/" + mac)
		if err == nil {
			c = make(map[string]*cfgapi.PropertyNode)
			c[mac] = node
		}
	}

	// Build a sorted list of the mac addresses, so the
	// output is in a predictable order
	macs := make([]string, 0)
	for mac := range c {
		macs = append(macs, mac)
	}
	sort.Strings(macs)

	for _, mac := range macs {
		s, serr := buildStats(mac, c[mac])
		if serr == nil {
			rval = append(rval, s)
		} else {
			err = serr
		}
	}

	return rval, err
}

func stats(cmd string, args []string) error {
	var mac string

	flags := flag.NewFlagSet("stats", flag.ContinueOnError)
	allClients := flags.Bool("a", false, "show all clients")
	period := flags.Int("p", 0, "repeat period (seconds)")

	if err := flags.Parse(args); err != nil {
		usage(cmd)
	}
	args = flags.Args()

	// Either -a or a single mac address must be provided.
	if !*allClients {
		if len(args) != 1 {
			usage(cmd)
		}
		mac = args[0]

	} else if len(args) != 0 {
		usage(cmd)
	}

	showHdr := true
	for {
		stats, err := fetchStats(mac)
		if err != nil && len(stats) == 0 {
			if *period != 0 {
				fmt.Printf("%v", err)
				showHdr = true
			}
		} else {
			if showHdr {
				printStats(nil)
				showHdr = false
			}
			for _, client := range stats {
				printStats(client)
			}
		}

		if *period == 0 {
			return err
		}
		if len(stats) > 1 {
			fmt.Printf("\n")
		}

		time.Sleep(time.Second * time.Duration(*period))
	}
}

func printClient(mac string, client *cfgapi.ClientInfo, verbose bool) {
	name := "-"
	if client.DNSName != "" {
		name = client.DNSName
	} else if client.DHCPName != "" {
		name = client.DHCPName
	}

	ring := "-"
	if client.Ring != "" {
		// If a satellite node has an assigned name, display that rather
		// than the uuid
		if client.Ring == base_def.RING_INTERNAL {
			if alias, ok := nodeToName[name]; ok {
				name = alias
			}
		}

		ring = client.Ring
	}

	ipv4 := "-"
	exp := "-"
	if client.IPv4 != nil {
		ipv4 = client.IPv4.String()
		if client.Expires != nil {
			exp = timeStringShort(*client.Expires)
		} else {
			exp = "static"
		}
	}

	// Don't confuse the user with a device ID unless the confidence
	// is better than even.
	identString := ""
	if client.Confidence >= 0.5 {
		device, err := deviceid.GetDeviceByPath(configd,
			"@/devices/"+client.Identity)
		if err == nil {
			identString = fmt.Sprintf("%s %s", device.Vendor, device.ProductName)
		} else {
			identString = client.Identity
		}
	}

	// If the confidence is less than almost certain (as defined by
	// Words of Estimative Probability), prepend the device ID with
	// a question mark.
	confidenceMarker := ""
	if client.Confidence < 0.87 {
		confidenceMarker = "? "
	}

	if verbose {
		fmt.Printf("%-17s %-16s %-10s %-8v %-15s %-16s %s%-9s\n",
			mac, name, ring, client.Wireless, ipv4, exp,
			confidenceMarker, identString)
	} else {
		fmt.Printf("%-17s %-16s %-10s %-8v %-15s %-16s\n",
			mac, name, ring, client.Wireless, ipv4, exp)
	}
}

func getClients(cmd string, args []string) error {
	flags := flag.NewFlagSet("clients", flag.ContinueOnError)
	allClients := flags.Bool("a", false, "show all clients")
	verbose := flags.Bool("v", false, "verbose output")

	if err := flags.Parse(args); err != nil {
		usage(cmd)
	}

	getAliases()

	// Build a list of client mac addresses, and sort them
	clients := configd.GetClients()
	macs := make([]string, 0)
	for mac := range clients {
		macs = append(macs, mac)
	}
	sort.Strings(macs)

	if *verbose {
		fmt.Printf("%-17s %-16s %-10s %8s %-15s %-16s %9s\n",
			"macaddr", "name", "ring", "wireless", "ip addr",
			"expiration", "device id")
	} else {
		fmt.Printf("%-17s %-16s %-10s %8s %-15s %-16s\n",
			"macaddr", "name", "ring", "wireless", "ip addr",
			"expiration")
	}

	for _, mac := range macs {
		client := clients[mac]
		if client.IsActive() || *allClients {
			printClient(mac, client, *verbose)
		}
	}

	return nil
}

func getFormatted(cmd string, args []string) error {
	switch args[0] {
	case "clients":
		return getClients(cmd, args[1:])
	case "rings":
		return getRings(cmd, args[1:])
	default:
		return fmt.Errorf("unrecognized property: %s", args[0])
	}
}

func printDev(d *deviceid.Device) {
	fmt.Printf("  Type: %s\n", d.Devtype)
	fmt.Printf("  Vendor: %s\n", d.Vendor)
	fmt.Printf("  Product: %s\n", d.ProductName)
	if d.ProductVersion != "" {
		fmt.Printf("  Version: %s\n", d.ProductVersion)
	}
	if len(d.UDPPorts) > 0 {
		fmt.Printf("  UDP Ports: %v\n", d.UDPPorts)
	}
	if len(d.InboundPorts) > 0 {
		fmt.Printf("  TCP Inbound: %v\n", d.InboundPorts)
	}
	if len(d.OutboundPorts) > 0 {
		fmt.Printf("  TCP Outbound: %v\n", d.OutboundPorts)
	}
	if len(d.DNS) > 0 {
		fmt.Printf("  DNS Allowed: %v\n", d.OutboundPorts)
	}
}

func getProp(cmd string, args []string) error {
	var err error
	var root *cfgapi.PropertyNode

	if len(args) < 1 {
		usage(cmd)
	}

	prop := args[0]
	if !strings.HasPrefix(prop, "@") {
		return getFormatted(cmd, args)
	}

	if len(args) > 1 {
		usage(cmd)
	}

	if strings.HasPrefix(prop, "@/devices") {
		var d *deviceid.Device
		if d, err = deviceid.GetDeviceByPath(configd, prop); err == nil {
			printDev(d)
		}
	} else {
		prop = nodeAlias(prop)
		if root, err = configd.GetProps(prop); err == nil {
			nodes := strings.Split(strings.Trim(prop, "/"), "/")
			label := nodes[len(nodes)-1]
			root.DumpTree(os.Stdout, label)
		} else {
			err = fmt.Errorf("get failed: %v", err)
		}
	}

	return err
}

func hdlExpire(path []string) {
	fmt.Printf("%s Expired: %s\n", now(), strings.Join(path, "/"))
}

func hdlDelete(path []string) {
	fmt.Printf("%s Deleted: %s\n", now(), strings.Join(path, "/"))
}

func hdlUpdate(path []string, val string, exp *time.Time) {
	var at string

	if exp != nil {
		at = "  expires at: " + timeString(*exp)
	}
	fmt.Printf("%s Updated: %s -> %s%s\n", now(), strings.Join(path, "/"), val, at)

}

func replace(cmd string, args []string) error {
	var data []byte
	var err error
	var src string

	if len(args) != 1 {
		usage(cmd)
	}

	if args[0] == "-" {
		src = "stdin"
		data, err = ioutil.ReadAll(os.Stdin)
	} else {
		src = args[0]
		data, err = ioutil.ReadFile(src)
	}

	fmt.Printf("imported from %s\n", src)
	if err != nil {
		return fmt.Errorf("error reading from %s: %v", src, err)
	}

	tree, err := cfgtree.NewPTree("@/", data)
	if err != nil {
		return fmt.Errorf("importing tree from %s: %v", src, err)
	}

	if err = configd.Replace(tree.Export(false)); err != nil {
		return fmt.Errorf("replacing existing config tree: %v", err)
	}

	return nil
}

func export(cmd string, args []string) error {
	if len(args) != 0 {
		usage(cmd)
	}

	ops := []cfgapi.PropertyOp{
		{
			Op:   cfgapi.PropGet,
			Name: "@",
		},
	}
	data, err := configd.Execute(nil, ops).Wait(nil)

	if err != nil {
		err = fmt.Errorf("fetching tree: %v", err)
	} else {
		tree, err := cfgtree.NewPTree("@/", []byte(data))
		if err != nil {
			err = fmt.Errorf("rebuilding tree: %v", err)
		} else {
			data := tree.Export(true)
			fmt.Printf("%s\n", string(data))
		}
	}

	return err
}

func monProp(cmd string, args []string) error {
	if len(args) != 1 {
		usage(cmd)
	}

	prop := args[0]
	if !strings.HasPrefix(prop, "@") {
		return fmt.Errorf("invalid property path: %s", prop)
	}

	prop = nodeAlias(prop)
	fmt.Printf("monitoring %s\n", prop)
	if err := configd.HandleChange(prop, hdlUpdate); err != nil {
		return err
	}
	if err := configd.HandleDelete(prop, hdlDelete); err != nil {
		return err
	}
	if pname != "cl-configctl" {
		// Expiration events are only available on the client
		if err := configd.HandleExpire(prop, hdlExpire); err != nil {
			return err
		}
	}

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	return nil
}

func makeDelProp(cmd string, args []string) *cfgapi.PropertyOp {
	if len(args) != 1 {
		usage(cmd)
	}

	prop := nodeAlias(args[0])
	op := cfgapi.PropertyOp{
		Op:   cfgapi.PropDelete,
		Name: prop,
	}
	return &op
}

func makeSetProp(cmd string, args []string) *cfgapi.PropertyOp {
	if len(args) < 2 || len(args) > 3 {
		usage(cmd)
	}

	prop := nodeAlias(args[0])
	op := cfgapi.PropertyOp{
		Name:  prop,
		Value: args[1],
	}

	if len(args) == 3 {
		seconds, _ := strconv.Atoi(args[2])
		dur := time.Duration(seconds) * time.Second
		tmp := time.Now().Add(dur)
		op.Expires = &tmp
	}

	if cmd == "set" {
		op.Op = cfgapi.PropSet
	} else {
		op.Op = cfgapi.PropCreate
	}
	return &op
}

func makeOp(cmd string, args []string) *cfgapi.PropertyOp {
	switch cmd {
	case "set":
		return makeSetProp(cmd, args)
	case "add":
		return makeSetProp(cmd, args)
	case "del":
		return makeDelProp(cmd, args)
	case "get":
		fmt.Printf("'get' must be a standalone command\n")
		os.Exit(1)
	default:
		usage("")
	}
	return nil
}

func makeOps(args []string) []cfgapi.PropertyOp {
	ops := make([]cfgapi.PropertyOp, 0)

	var cmd string
	var cmdArgs []string
	for _, f := range args {
		if cmd == "" {
			cmd = f
			cmdArgs = make([]string, 0)
		} else if f != "," {
			cmdArgs = append(cmdArgs, f)
		} else {
			ops = append(ops, *makeOp(cmd, cmdArgs))
			cmd = ""
		}
	}

	if cmd != "" {
		ops = append(ops, *makeOp(cmd, cmdArgs))
	}
	return ops
}

var usages = map[string]string{
	"ping":    "",
	"set":     "<prop> <value [duration]>",
	"add":     "<prop> <value [duration]>",
	"get":     "<prop> | clients [-a] [-v] | rings",
	"del":     "<prop>",
	"mon":     "<prop>",
	"replace": "<file | ->",
	"stats": "[-a] [-p <period (seconds)] [<mac>]  - " +
		"either -a or a mac address must be provided",
	"export": "",
}

func usage(cmd string) {
	if u, ok := usages[cmd]; ok {
		fmt.Printf("usage: %s %s %s\n", pname, cmd, u)
	} else {
		fmt.Printf("usage: %s <cmd> <args> [, <cmd> <args> ]\n", pname)
		fmt.Printf("  commands:\n")
		for c, u := range usages {
			fmt.Printf("    %s %s\n", c, u)
		}

		fmt.Printf("\n  options:\n")
		flag.PrintDefaults()
	}

	os.Exit(1)
}

// Exec executes the bulk of the configctl work.
func Exec(ctx context.Context, p string, hdl *cfgapi.Handle, args []string) error {
	var err error

	configd = hdl
	pname = p

	if len(args) < 1 {
		usage("")
	}

	switch args[0] {
	case "get":
		err = getProp("get", args[1:])
	case "mon":
		err = monProp("mon", args[1:])
	case "ping":
		if err = hdl.Ping(ctx); err == nil {
			fmt.Printf("ok\n")
		}
	case "replace":
		err = replace("replace", args[1:])
	case "export":
		err = export("export", args[1:])
	case "stats":
		err = stats("stats", args[1:])
	default:
		ops := makeOps(args)
		_, err = configd.Execute(ctx, ops).Wait(ctx)
		if err == nil {
			fmt.Println("ok")
		}
	}

	return err
}
