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
	"regexp"
	"sync"
	"time"

	"bg/common/cfgtree"
)

var (
	expirationHeap  pnodeQueue
	expirationTimer *time.Timer
	expirationLock  sync.Mutex
)

//
// When a client's ring assignment expires, it returns to the default ring
//
func ringExpired(node *cfgtree.PNode) {

	client := node.Parent()
	old := node.Value
	node.Value = ""
	node.Value = selectRing(client.Name(), client, nil)
	node.Expires = nil
	if node.Value != old {
		updates := []*updateRecord{
			updateChange(node.Path(), &node.Value, nil),
		}
		updateNotify(updates)
	}
}

var expirationHandlers = []struct {
	path    *regexp.Regexp
	handler func(*cfgtree.PNode)
}{
	{regexp.MustCompile(`^@/clients/.*/ring$`), ringExpired},
}

func getIndex(node *cfgtree.PNode) int {
	x := node.Data()

	if idx, ok := x.(int); ok {
		return idx
	}

	return -1
}

func setIndex(node *cfgtree.PNode, idx int) {
	node.SetData(idx)
}

func clearIndex(node *cfgtree.PNode) {
	node.SetData(nil)
}

/*******************************************************************
 *
 * Implement the functions required by the container/heap interface
 */
type pnodeQueue []*cfgtree.PNode

func (q pnodeQueue) Len() int { return len(q) }

func (q pnodeQueue) Less(i, j int) bool {
	return (q[i].Expires).Before(*q[j].Expires)
}

func (q pnodeQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	setIndex(q[i], i)
	setIndex(q[j], j)
}

func (q *pnodeQueue) Push(x interface{}) {
	n := len(*q)
	prop := x.(*cfgtree.PNode)
	setIndex(prop, n)
	*q = append(*q, prop)
}

func (q *pnodeQueue) Pop() interface{} {
	old := *q
	n := len(old)
	prop := old[n-1]
	clearIndex(prop) // for safety
	*q = old[0 : n-1]
	return prop
}

// Repeatedly pop the top entry off the heap, for as long as the top entry's
// expiration is in the past.  Return a slice of all the expired properties.
func findExpirations() []string {
	now := time.Now()

	expired := make([]string, 0)
	for len(expirationHeap) > 0 {
		next := expirationHeap[0]

		if next.Expires == nil {
			// Should never happen
			slog.Warnf("Found static property %s in "+
				"expiration heap at %d",
				next.Path(), getIndex(next))
			heap.Pop(&expirationHeap)
			continue
		}

		if now.Before(*next.Expires) {
			break
		}

		if *verbose {
			slog.Debugf("Expiring: %s at %v\n",
				next.Name(), time.Now())
		}
		expired = append(expired, next.Path())
		heap.Pop(&expirationHeap)
		metrics.expCounts.Inc()

		clearIndex(next)
	}

	return expired
}

func processExpirations(expired []string) {
	now := time.Now()
	cnt := 0

	updates := make([]*updateRecord, 0)
	propTree.Lock()
	for _, path := range expired {
		node, _ := propTree.GetNode(path)
		if node == nil {
			continue
		}

		// check to be sure the property hasn't been
		// reset since we added it to the list.
		if now.Before(*node.Expires) {
			continue
		}

		// Check for any special actions to take when this property
		// expires
		for _, r := range expirationHandlers {
			if r.path.MatchString(path) {
				r.handler(node)
			}
		}

		updates = append(updates, updateExpire(path))
		cnt++
	}

	if cnt > 0 {
		updateNotify(updates)
		propTreeStore()
	}

	propTree.Unlock()
}

func expirationHandler() {

	expirationTimer = time.NewTimer(time.Duration(time.Minute))

	for true {
		<-expirationTimer.C
		expirationLock.Lock()

		expired := findExpirations()

		if len(expired) > 0 {
			expirationReset()
		}

		expirationLock.Unlock()

		processExpirations(expired)
	}
}

func expirationRemove(node *cfgtree.PNode) {
	reset := false

	expirationLock.Lock()
	if index := getIndex(node); index != -1 {
		reset = (index == 0)
		heap.Remove(&expirationHeap, index)
		clearIndex(node)
	}
	expirationLock.Unlock()

	if reset {
		expirationReset()
	}
}

func expirationsEval(paths []string) {
	reset := false

	// Iterate over all of the properties in the list and (re)evaluate their
	// position in the expiration heap.
	expirationLock.Lock()
	for _, path := range paths {
		node, _ := propTree.GetNode(path)
		if node == nil {
			// This property was inserted and then deleted as part
			// of a compound operation
			continue
		}

		// If the node was in the expiration list, pull it out
		if index := getIndex(node); index != -1 {
			reset = (index == 0)
			heap.Remove(&expirationHeap, index)
			clearIndex(node)
		}

		// If the property has an expiration time, insert it into the
		// heap
		if node.Expires != nil {
			heap.Push(&expirationHeap, node)
			if getIndex(node) == 0 {
				reset = true
			}
		}
	}
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
