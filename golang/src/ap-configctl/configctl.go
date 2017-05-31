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
	"log"
	"time"

	"ap_common"
)

var (
	get_value = flag.Bool("get", false, "Query values")
	set_value = flag.Bool("set", false, "Set one property to the given value")
	config    *ap_common.Config
)

func main() {
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	config = ap_common.NewConfig("ap-configctl")

	//  Ensure subscriber connection has time to complete
	time.Sleep(time.Second)

	if *set_value {
		if len(flag.Args()) != 2 {
			log.Fatal("wrong set invocation")
		}

		err := config.SetProp(flag.Arg(0), flag.Arg(1))
		if err != nil {
			log.Fatalln("property set failed:", err)
		}
		log.Printf("set: %v=%v\n", flag.Arg(0), flag.Arg(1))
	} else if *get_value {
		for _, arg := range flag.Args() {
			val, err := config.GetProp(arg)
			if err != nil {
				log.Fatalln("property get failed:", err)
			}
			log.Printf("get: %v=%v\n", arg, val)
		}
	} else {
		flag.Usage()
	}
}
