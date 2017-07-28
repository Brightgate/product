/*
 * COPYRIGHT 2017 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

/*
 * ap-configctl [-get | -set] property_or_value
 */

package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"ap_common/apcfg"
)

var (
	get_value = flag.Bool("get", false, "Query values")
	set_value = flag.Bool("set", false, "Set one property to the given value")
	add_prop  = flag.Bool("add", false, "Add new property")
	del_prop  = flag.Bool("del", false, "Delete a property")
	apcfgd    *apcfg.APConfig
)

func main() {
	flag.Parse()

	apcfgd = apcfg.NewConfig("ap-configctl")

	//  Ensure subscriber connection has time to complete
	time.Sleep(time.Millisecond * 50)

	var expires *time.Time

	prop := flag.Arg(0)
	if len(prop) == 0 {
		fmt.Printf("No property specified\n")
		os.Exit(1)
	}

	if *set_value || *add_prop {
		var op string
		var f func(string, string, *time.Time) error

		if *set_value {
			op = "set"
			f = apcfgd.SetProp
		} else {
			op = "create"
			f = apcfgd.CreateProp
		}

		val := flag.Arg(1)
		if len(val) == 0 {
			fmt.Printf("No value specified for %s\n", op)
			os.Exit(1)
		}

		duration := flag.Arg(2)
		if len(duration) > 0 {
			seconds, _ := strconv.Atoi(duration)
			dur := time.Duration(seconds) * time.Second
			tmp := time.Now().Add(dur)
			expires = &tmp
		}

		err := f(prop, val, expires)
		if err != nil {
			fmt.Printf("property %s failed: %v\n", op, err)
			os.Exit(1)
		}
		fmt.Printf("%s: %v=%v\n", op, prop, val)
	} else if *get_value {
		for _, arg := range flag.Args() {
			root, err := apcfgd.GetProps(arg)
			if err != nil {
				fmt.Printf("property get failed: %v\n", err)
				os.Exit(1)
			}
			root.DumpTree()
		}
	} else if *del_prop {
		err := apcfgd.DeleteProp(prop)
		if err != nil {
			fmt.Printf("property get failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("del: %s\n", prop)
	} else {
		flag.Usage()
	}
}
