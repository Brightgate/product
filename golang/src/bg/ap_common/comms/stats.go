/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package comms

import (
	"fmt"
	"time"
)

// CommStats contains statistics summarizing the message traffic serviced by an
// APComm server port.
type CommStats struct {
	Received int32 // messages received and queued for processing
	Dequeued int32 // messages dequeued for processing
	Executed int32 // messages processed
	Replied  int32 // messages replied to

	QueueLenCur int32   // current length of the pending message queue
	QueueLenAvg float32 // average length of the queue
	QueueLenMax int32   // maximum length of the queue

	QueueTime TimeStat // Time spent on the incoming queue
	ExecTime  TimeStat // Time spent executing message callbacks
	ReplyTime TimeStat // Time spent pushing replies to clients
}

// TimeStat is used to track a single timing statistic
type TimeStat struct {
	Cnt   int32
	Total time.Duration
	Max   time.Duration
	Avg   time.Duration
}

func (s *TimeStat) addObservation(obs time.Duration) {
	s.Cnt++
	s.Total += obs
	if obs > s.Max {
		s.Max = obs
	}
	s.Avg = s.Total / time.Duration(s.Cnt)
}

// Return a string with the average and maximum durations for this stat
func (s *TimeStat) String() string {
	avg := s.Total / time.Duration(s.Cnt)
	return fmt.Sprintf("(avg: %s  max: %s)", avg, s.Max)
}

// Stats fetchs a current copy of the stats being maintained for this server
func (c *APComm) Stats() *CommStats {
	var rval *CommStats

	s := c.stats
	if s != nil {
		c.statsLock.Lock()
		defer c.statsLock.Unlock()

		c := *s
		rval = &c
	}
	return rval
}

// String returns a summary of the communicator's accumulated statistics
func (c *APComm) String() string {
	var rval string

	s := c.stats
	if s != nil {
		c.statsLock.Lock()
		defer c.statsLock.Unlock()

		rval = fmt.Sprintf("rcvd: %d  queueTime: %s  execTime: %s  replyTime: %s",
			s.Received, s.QueueTime.String(),
			s.ExecTime.String(), s.ReplyTime.String())
	}
	return rval
}

// Update the "received messages" stats
func (c *APComm) observeRcvd(m *msg) {
	s := c.stats
	c.statsLock.Lock()
	defer c.statsLock.Unlock()

	m.recvd = time.Now()
	s.Received++

	cnt := float32(s.Received)
	oldAvg := float32(c.stats.QueueLenAvg) * (cnt - 1)
	s.QueueLenAvg = (oldAvg + float32(c.stats.QueueLenCur)) / cnt

	s.QueueLenCur++
	if s.QueueLenCur > s.QueueLenMax {
		s.QueueLenMax = s.QueueLenCur
	}
}

// Update the "dequeued messages" stats
func (c *APComm) observeDeqd(m *msg) {
	s := c.stats
	c.statsLock.Lock()
	defer c.statsLock.Unlock()

	m.dequeued = time.Now()
	s.QueueLenCur--
	s.QueueTime.addObservation(m.dequeued.Sub(m.recvd))
}

// Update the "executed messages" stats
func (c *APComm) observeExeced(m *msg) {
	s := c.stats
	c.statsLock.Lock()
	defer c.statsLock.Unlock()

	m.completed = time.Now()
	s.ExecTime.addObservation(m.completed.Sub(m.dequeued))
}

// Update the "replied messages" stats
func (c *APComm) observeReplied(m *msg) {
	s := c.stats
	c.statsLock.Lock()
	defer c.statsLock.Unlock()

	s.ReplyTime.addObservation(time.Since(m.completed))
}
