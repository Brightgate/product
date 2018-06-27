/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"context"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/cloud_rpc"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
)

var bootTime *timestamp.Timestamp

func publishHeartbeat(ctx context.Context, tclient cloud_rpc.EventClient) error {
	heartbeat := &cloud_rpc.Heartbeat{
		BootTime:   bootTime,
		RecordTime: ptypes.TimestampNow(),
	}

	return publishEvent(ctx, tclient, "heartbeat", heartbeat)
}

func heartbeatLoop(ctx context.Context, tclient cloud_rpc.EventClient, wg *sync.WaitGroup, doneChan chan bool) {
	var done bool
	var err error

	bootTime, err = ptypes.TimestampProto(aputil.LinuxBootTime())
	if err != nil {
		slogger.Fatalf("couldn't get linux boot time")
	}

	ticker := time.NewTicker(time.Minute * 7)
	for !done {
		err := publishHeartbeat(ctx, tclient)
		if err != nil {
			slogger.Errorf("Failed heartbeat: %s", err)
		}
		select {
		case done = <-doneChan:
		case <-ticker.C:
		}
	}
	wg.Done()
}
