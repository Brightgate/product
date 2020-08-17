/*
 * Copyright 2018 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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
	expirationHeap pnodeQueue
	expirationLock sync.Mutex
	expirationEval = make(chan bool, 2)
)

//
// When a client's ring assignment expires, it returns to the default ring
//
func ringExpired(node *cfgtree.PNode) {
	client := node.Parent()
	old := node.Value
	node.Value = ""
	node.Value = selectRing(client.Name(), client, "", "")
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

		slog.Debugf("Expiring: %s", next.Path())
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
	propTree.ChangesetInit()
	for _, path := range expired {
		node, _ := propTree.GetNode(path)
		if node == nil {
			continue
		}

		// check to be sure the property hasn't been
		// reset since we added it to the list.
		if node.Expires == nil || now.Before(*node.Expires) {
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

	updateNotify(updates)
	propTree.ChangesetCommit()
}

func delay() time.Duration {
	d := time.Minute

	if len(expirationHeap) > 0 {
		d = time.Until(*(expirationHeap[0].Expires))
		// Never sleep for more than a minute, to avoid any corners or
		// race conditions if we experience a drastic change in
		// wallclock time.
		if d > time.Minute {
			d = time.Minute
		}
	}
	return d
}

func expirationHandler() {
	expirationLock.Lock()
	t := time.NewTimer(delay())
	expirationLock.Unlock()

	for {
		select {
		case <-t.C:
		case <-expirationEval:
		}

		expirationLock.Lock()
		expired := findExpirations()
		t.Reset(delay())
		expirationLock.Unlock()

		processExpirations(expired)
	}
}

func expirationRemove(node *cfgtree.PNode) {
	expirationLock.Lock()
	if index := getIndex(node); index != -1 {
		heap.Remove(&expirationHeap, index)
		if index == 0 {
			expirationEval <- true
		}
		clearIndex(node)
	}
	expirationLock.Unlock()
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
		expirationEval <- true
	}
	expirationLock.Unlock()
}

func expirationPopulate(node *cfgtree.PNode) {
	if exp := node.Expires; exp != nil && exp.After(time.Now()) {
		heap.Push(&expirationHeap, node)
	}

	for _, child := range node.Children {
		expirationPopulate(child)
	}
}

func expirationInit(tree *cfgtree.PTree) {
	expirationLock.Lock()
	defer expirationLock.Unlock()

	expirationHeap = make(pnodeQueue, 0)
	heap.Init(&expirationHeap)
	expirationPopulate(tree.Root())
	expirationEval <- true
}

