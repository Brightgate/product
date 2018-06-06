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
	reset := time.Duration(time.Minute)
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
			if delay.Seconds() > 1.0 {
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

		if len(expirationHeap) > 0 {
			next := expirationHeap[0]
			reset = time.Until(*next.Expires)
		}

		expirationTimer.Reset(reset)
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
	}
}

func nextExpiration() *pnode {
	if len(expirationHeap) == 0 {
		return nil
	}

	return expirationHeap[0]
}

/*
 * Update the expiration time of a single property (possibly setting an
 * expiration for the first time).  If this property either starts or ends at
 * the top of the expiration heap, reset the expiration timer accordingly.
 */
func expirationUpdate(node *pnode) {
	reset := false

	expirationLock.Lock()

	if node == nextExpiration() {
		reset = true
	}

	if node.Expires == nil {
		// This node doesn't have an expiration.  If it's in the heap,
		// it's probably because we just made the setting permanent.
		// Pull it out of the heap.
		if node.index != -1 {
			heap.Remove(&expirationHeap, node.index)
			node.index = -1
		}
	} else {
		if node.index == -1 {
			heap.Push(&expirationHeap, node)
		}
		heap.Fix(&expirationHeap, node.index)
	}

	if node == nextExpiration() {
		reset = true
	}

	if reset {
		if next := nextExpiration(); next != nil {
			expirationTimer.Reset(time.Until(*next.Expires))
		}
	}
	expirationLock.Unlock()
}

/*
 * Remove a single property from the expiration heap
 */
func expirationRemove(node *pnode) {
	expirationLock.Lock()
	if node.index != -1 {
		heap.Remove(&expirationHeap, node.index)
		node.index = -1
	}
	expirationLock.Unlock()
}

func expirationInit() {
	expirationHeap = make(pnodeQueue, 0)
	heap.Init(&expirationHeap)

	expirationTimer = time.NewTimer(time.Duration(time.Minute))
	go expirationHandler()
}
