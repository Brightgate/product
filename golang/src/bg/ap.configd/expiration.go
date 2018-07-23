/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"container/heap"
	"log"
	"sync"
	"time"
)

var (
	expirationHeap  pnodeQueue
	expirationTimer *time.Timer
	expirationLock  sync.Mutex
)

/*******************************************************************
 *
 * Implement the functions required by the container/heap interface
 */
type pnodeQueue []*pnode

func (q pnodeQueue) Len() int { return len(q) }

func (q pnodeQueue) Less(i, j int) bool {
	return (q[i].Expires).Before(*q[j].Expires)
}

func (q pnodeQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *pnodeQueue) Push(x interface{}) {
	n := len(*q)
	prop := x.(*pnode)
	prop.index = n
	*q = append(*q, prop)
}

func (q *pnodeQueue) Pop() interface{} {
	old := *q
	n := len(old)
	prop := old[n-1]
	prop.index = -1 // for safety
	*q = old[0 : n-1]
	return prop
}

func expirationHandler() {

	expirationTimer = time.NewTimer(time.Duration(time.Minute))

	first := true
	for true {
		<-expirationTimer.C
		expirationLock.Lock()

		expired := make([]*pnode, 0)
		now := time.Now()
		for len(expirationHeap) > 0 {
			next := expirationHeap[0]

			if next.Expires == nil {
				// Should never happen
				log.Printf("Found static property %s in "+
					"expiration heap at %d\n",
					next.path, next.index)
				heap.Pop(&expirationHeap)
				continue
			}

			if now.Before(*next.Expires) {
				break
			}

			delay := now.Sub(*next.Expires)
			if delay.Seconds() > 1.0 && !first {
				log.Printf("Missed expiration for %s by %s\n",
					next.name, delay)
			}
			if *verbose {
				log.Printf("Expiring: %s at %v\n",
					next.name, time.Now())
			}
			expired = append(expired, next)
			heap.Pop(&expirationHeap)
			metrics.expCounts.Inc()

			next.index = -1
		}

		if len(expired) > 0 {
			expirationReset()
		}

		expirationLock.Unlock()
		if len(expired) > 0 {
			propTreeMutex.Lock()
			for _, node := range expired {
				// check to be sure the property hasn't been
				// reset since we added it to the list.
				if now.Before(*node.Expires) {
					node.ops.expire(node)
				}
			}
			propTreeStore()
			propTreeMutex.Unlock()
		}
		first = false
	}
}

// Add a property to the expiration heap.
func expirationInsert(node *pnode) {
	expirationLock.Lock()

	node.index = -1
	heap.Push(&expirationHeap, node)

	// If this property is now at the top of the heap, reset the timer
	if node.index == 0 {
		expirationReset()
	}

	expirationLock.Unlock()
}

// Remove a single property from the expiration heap
func expirationRemove(node *pnode) {
	expirationLock.Lock()

	reset := (node.index == 0)
	if node.index != -1 {
		heap.Remove(&expirationHeap, node.index)
		node.index = -1
	}

	// If this property was at the top of the heap, reset the timer
	if reset {
		expirationReset()
	}

	expirationLock.Unlock()
}

func expirationReset() {
	reset := time.Minute

	if len(expirationHeap) > 0 {
		next := expirationHeap[0]
		reset = time.Until(*next.Expires)
	}
	if t := expirationTimer; t != nil {
		t.Reset(reset)
	}
}

func expirationInit() {
	expirationHeap = make(pnodeQueue, 0)
	heap.Init(&expirationHeap)
}
