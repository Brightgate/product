/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

