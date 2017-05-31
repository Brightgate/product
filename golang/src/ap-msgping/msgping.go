/*
 * COPYRIGHT 2017 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

package main

import (
	"log"
	"time"

	"ap_common"
)

func main() {
	var b ap_common.Broker

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("start")
	b.Init("ap-msgping")
	b.Connect()
	defer b.Disconnect()

	//  Ensure subscriber connection has time to complete
	time.Sleep(time.Second)
	b.Ping()

	log.Println("end")
}
