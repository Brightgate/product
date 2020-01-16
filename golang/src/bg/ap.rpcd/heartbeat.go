/*
 * COPYRIGHT 2020 Brightgate Inc. All rights reserved.
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
	"fmt"
	"sync"
	"time"

	"bg/cloud_rpc"

	"github.com/golang/protobuf/ptypes"
)

func publishHeartbeat(ctx context.Context, tclient cloud_rpc.EventClient) error {
	bootTime, err := ptypes.TimestampProto(nodeBootTime)
	if err != nil {
		return fmt.Errorf("couldn't encode linux boot time: %v", err)
	}

	heartbeat := &cloud_rpc.Heartbeat{
		BootTime:   bootTime,
		RecordTime: ptypes.TimestampNow(),
	}

	err = publishEvent(ctx, tclient, "heartbeat", heartbeat)
	rpcHealthUpdate(err == nil)
	if err == nil {
		metrics.heartbeatsSucceeded.Inc()
	} else {
		metrics.heartbeatsFailed.Inc()
	}

	return err
}

func heartbeatLoop(ctx context.Context, tclient cloud_rpc.EventClient,
	wg *sync.WaitGroup, doneChan chan bool) {
	var done bool

	ticker := time.NewTicker(time.Minute * 7)
	defer ticker.Stop()
	for !done {
		if err := publishHeartbeat(ctx, tclient); err != nil {
			slog.Errorf("Failed heartbeat: %s", err)
		}
		select {
		case done = <-doneChan:
		case <-ticker.C:
		}
	}
	slog.Infof("heartbeat loop done")
	wg.Done()
}
