/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// When running on the same node as cl.configd (the likely scenario for
// testing), the following environment variables should be set:
//
//       export B10E_CLCONFIGD_CONNECTION=127.0.0.1:4431
//       export B10E_CLCONFIGD_DISABLE_TLS=true

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	rpc "bg/cloud_rpc"
	"bg/common/cfgtree"
	"bg/common/grpcutils"

	"github.com/tomazk/envcfg"

	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"
)

const pname = "cl-configctl"

type configd struct {
	uuid   string
	conn   *grpc.ClientConn
	client rpc.ConfigFrontEndClient
}

type cmdDesc struct {
	minArgs int
	maxArgs int
	fn      func(*configd, rpc.CfgPropOps_AccessLevel, []string) error
	usage   string
}

var cmds = map[string]cmdDesc{
	"ping": {0, 0, doPing, "ping"},
	"get":  {1, 1, doGet, "get <prop>"},
	"set":  {2, 3, doSet, "set <prop> <value [duration]>"},
	"add":  {2, 3, doAdd, "add <prop> <value [duration]>"},
	"del":  {1, 1, doDel, "del <prop>"},
}

var accessLevels = map[string]rpc.CfgPropOps_AccessLevel{
	"internal":  rpc.CfgPropOps_INTERNAL,
	"developer": rpc.CfgPropOps_DEVELOPER,
	"service":   rpc.CfgPropOps_SERVICE,
	"admin":     rpc.CfgPropOps_ADMIN,
	"user":      rpc.CfgPropOps_USER,
}

var environ struct {
	// XXX: this will eventually be a postgres connection, and we will look
	// up the per-appliance cl.configd connection via the database
	ConfigdConnection string `envcfg:"B10E_CLCONFIGD_CONNECTION"`
	DisableTLS        bool   `envcfg:"B10E_CLCONFIGD_DISABLE_TLS"`
}

var (
	level   = flag.String("l", "user", "change configd access level")
	uuid    = flag.String("a", "", "uuid of appliance to configure")
	timeout = flag.Duration("t", 10*time.Second, "timeout")
)

// Establish a connection to the configd grpc server and instantiate a
// FrontEnd client.
func grpcConnect(uuid, url string, tlsEnabled bool) (*configd, error) {
	if uuid == "" {
		return nil, fmt.Errorf("no appliance uuid provided")
	}

	conn, err := grpcutils.NewClientConn(url, tlsEnabled, pname)
	if err != nil {
		return nil, fmt.Errorf("grpc connection () to '%s' failed: %v",
			url, err)
	}
	client := rpc.NewConfigFrontEndClient(conn)

	return &configd{
		uuid:   uuid,
		conn:   conn,
		client: client,
	}, nil
}

// Send a single Ping to configd just to verify that a round-trip grpc call can
// succeed.
func doPing(c *configd, level rpc.CfgPropOps_AccessLevel, args []string) error {
	deadline := time.Now().Add(*timeout)
	ctx, ctxcancel := context.WithDeadline(context.Background(), deadline)
	defer ctxcancel()

	pingOp := &rpc.CfgFrontEndPing{
		Time: ptypes.TimestampNow(),
	}

	r, err := c.client.Ping(ctx, pingOp)
	if err == nil {
		log.Printf("%s\n", r.Payload)
	}
	return err
}

// Send a CfgPropOps message to configd.  On success, the Payload is returned to
// the caller.  Failed operations (as indicated by the Response field) are
// converted into Golang errors.
func exec(c *configd, level rpc.CfgPropOps_AccessLevel,
	op *rpc.CfgPropOps_CfgPropOp) (string, error) {

	var rval string

	deadline := time.Now().Add(*timeout)
	ctx, ctxcancel := context.WithDeadline(context.Background(), deadline)
	defer ctxcancel()

	ops := []*rpc.CfgPropOps_CfgPropOp{op}

	cmd := rpc.CfgPropOps{
		Time:      ptypes.TimestampNow(),
		CloudUuid: c.uuid,
		Level:     level,
		Ops:       ops,
	}

	r, err := c.client.Submit(ctx, &cmd)
	if err == nil {
		if r.Response == rpc.CfgPropResponse_OK {
			rval = r.Payload
		} else {
			err = fmt.Errorf("%s", r.Errmsg)
		}
	}

	return rval, err
}

func dumpTree(indent string, node *cfgtree.PNode) {
	e := ""
	if node.Expires != nil {
		e = node.Expires.Format("2006-01-02T15:04:05")
	}
	fmt.Printf("%s%s: %s  %s\n", indent, node.Name(), node.Value, e)
	for _, child := range node.Children {
		dumpTree(indent+"  ", child)
	}
}

// Execute a GET operation.  On success the returned data is formatted and
// printed to stdout
func doGet(c *configd, level rpc.CfgPropOps_AccessLevel, args []string) error {
	getOp := rpc.CfgPropOps_CfgPropOp{
		Operation: rpc.CfgPropOps_CfgPropOp_GET,
		Property:  args[0],
	}

	data, err := exec(c, level, &getOp)
	if err == nil {
		t, rerr := cfgtree.NewPTree(args[0], []byte(data))
		if rerr != nil {
			err = fmt.Errorf("unable to parse results: %v", rerr)
		} else {
			dumpTree("", t.Root())
		}
	}
	return err
}

// Execute either a SET or CREATE.
func doAddSet(c *configd, level rpc.CfgPropOps_AccessLevel,
	opcode rpc.CfgPropOps_CfgPropOp_Operation, args []string) error {

	var err error

	op := rpc.CfgPropOps_CfgPropOp{
		Operation: opcode,
		Property:  args[0],
		Value:     args[1],
	}

	if len(args) == 3 {
		dur, terr := time.ParseDuration(args[2])
		if terr != nil {
			err = fmt.Errorf("bad duration '%s': %v", args[2], err)
		} else {
			tmp := time.Now().Add(dur)
			op.Expires, _ = ptypes.TimestampProto(tmp)
		}
	}

	if err == nil {
		_, err = exec(c, level, &op)
	}

	if err == nil {
		fmt.Printf("ok\n")
	}
	return err
}

func doAdd(c *configd, level rpc.CfgPropOps_AccessLevel, args []string) error {
	return doAddSet(c, level, rpc.CfgPropOps_CfgPropOp_CREATE, args)
}

func doSet(c *configd, level rpc.CfgPropOps_AccessLevel, args []string) error {
	return doAddSet(c, level, rpc.CfgPropOps_CfgPropOp_SET, args)
}

// Execute a property DELETE
func doDel(c *configd, level rpc.CfgPropOps_AccessLevel, args []string) error {
	var err error

	op := rpc.CfgPropOps_CfgPropOp{
		Operation: rpc.CfgPropOps_CfgPropOp_DELETE,
		Property:  args[0],
	}

	_, err = exec(c, level, &op)
	if err == nil {
		fmt.Printf("ok\n")
	}
	return err
}

func usage(cmd string) {
	if d, ok := cmds[cmd]; ok {
		fmt.Printf("usage: %s [-l <level> ] -a <appliance> %s\n",
			pname, d.usage)
	} else {
		fmt.Printf("usage: %s [-l <level> ] -a <appliance> <cmd>\n", pname)
		for _, d := range cmds {
			fmt.Printf("    %s\n", d.usage)
		}
	}
	os.Exit(1)
}

func main() {
	var err error
	var cmd *cmdDesc

	flag.Parse()

	err = envcfg.Unmarshal(&environ)
	if err != nil {
		log.Fatalf("Environment Error: %s", err)
	}
	if environ.ConfigdConnection == "" {
		log.Fatalf("B10E_CLCONFIGD_CONNECTION must be set")
	}

	l, ok := accessLevels[*level]
	if !ok {
		fmt.Printf("no such access level: %s\n", *level)
		os.Exit(1)
	}
	args := flag.Args()

	if len(args) > 0 {
		if d, ok := cmds[args[0]]; ok {
			cmd = &d
		}
	}
	if cmd == nil {
		usage("")
	} else if len(args)-1 < cmd.minArgs || len(args)-1 > cmd.maxArgs {
		usage(args[0])
	}

	cfg, err := grpcConnect(*uuid, environ.ConfigdConnection,
		!environ.DisableTLS)
	if err != nil {
		log.Fatalf("grpc connection failure: %s", err)
	}

	if err = cmd.fn(cfg, l, args[1:]); err != nil {
		err = fmt.Errorf("'%s' failed: %v", args[0], err)
	}

	cfg.conn.Close()

	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
}
