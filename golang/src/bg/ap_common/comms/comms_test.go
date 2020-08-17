/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package comms

import (
	"fmt"
	"log"
	"os"
	"sync"
	"testing"
	"time"
)

type server struct {
	name string
	url  string
	cb   func([]byte) []byte
}

var servers = []server{
	{"server one", "tcp://127.0.0.1:3000", identityCB},
	{"server two", "tcp://127.0.0.1:3001", reverseCB},
}

type clientCase struct {
	name       string
	server     *server
	iterations int
	start      chan bool
	done       *sync.WaitGroup
}

func identityCB(in []byte) []byte {
	return in
}

func reverseCB(in []byte) []byte {
	l := len(in)
	out := make([]byte, l)

	for i, v := range in {
		out[l-i-1] = v
	}

	return out
}

// TestSimple performs a single ReqRepl operation on a single server
func TestSimple(t *testing.T) {
	comm, err := NewAPClient("test_client", servers[0].url)
	if err != nil {
		t.Errorf("NewAPClient failed: %v", err)
	}
	defer comm.Close()

	data := []byte("test at: " + time.Now().Format(time.Stamp))
	reply, err := comm.ReqRepl(data)
	if err != nil {
		t.Errorf("ReqRepl failed: %v", err)
	}

	expected := servers[0].cb(data)
	if string(reply) != string(expected) {
		t.Errorf("Got back '%s'  Expected '%s'", string(reply), data)
	}
}

func singleClient(t *testing.T, c *clientCase) {
	defer c.done.Done()

	log.Printf("%s launched\n", c.name)
	comm, err := NewAPClient(c.name, c.server.url)
	if err != nil {
		t.Errorf("NewAPClient(%s, %s) failed: %v", c.name, c.server.url, err)
	}
	defer comm.Close()

	<-c.start
	for i := 0; i < c.iterations; i++ {
		data := []byte(fmt.Sprintf("%d %s %s", i, c.name,
			time.Now().Format(time.RFC3339Nano)))

		reply, err := comm.ReqRepl(data)
		if err != nil {
			t.Errorf("ReqRepl failed: %v", err)
		}
		expected := c.server.cb(data)

		if string(reply) != string(expected) {
			t.Errorf("Got back '%s'  Expected '%s'",
				string(reply), expected)
		}
	}
	log.Printf("%s done\n", c.name)
}

// TestLoop tests a single client making multiple calls to the same server
func TestLoop(t *testing.T) {
	var done sync.WaitGroup

	c := clientCase{
		name:       "client",
		server:     &servers[0],
		iterations: 10000,
		start:      make(chan bool),
		done:       &done,
	}

	done.Add(1)
	go singleClient(t, &c)

	log.Printf("kick clients\n")
	c.start <- true
	log.Printf("wait for clients\n")
	done.Wait()
	log.Printf("all clients done\n")
}

// TestTwoClients exercises two client threads, each accessing a different
// server
func TestTwoClients(t *testing.T) {
	var done sync.WaitGroup

	clients := []clientCase{
		{
			name:       "client 1",
			server:     &servers[0],
			iterations: 10000,
			start:      make(chan bool),
			done:       &done,
		},
		{
			name:       "client 2",
			server:     &servers[1],
			iterations: 10000,
			start:      make(chan bool),
			done:       &done,
		},
	}

	for i := range clients {
		done.Add(1)
		go singleClient(t, &clients[i])
	}

	time.Sleep(time.Second)
	log.Printf("kick clients\n")
	for _, c := range clients {
		c.start <- true
	}

	log.Printf("wait for clients\n")
	done.Wait()

	log.Printf("all clients done\n")
}

// TestMultiClients exercises multiple clients each hitting multple servers at
// the same time.
func TestMultiClients(t *testing.T) {
	var done sync.WaitGroup

	clients := make([]*clientCase, 0)

	for i := 1; i < 10; i++ {
		for j, s := range servers {
			n := fmt.Sprintf("client_%d:%d", i, j)
			c := &clientCase{
				name:       n,
				server:     &s,
				iterations: 10000,
				start:      make(chan bool),
				done:       &done,
			}
			clients = append(clients, c)
		}
	}

	for _, c := range clients {
		done.Add(1)
		go singleClient(t, c)
	}

	time.Sleep(time.Second)
	log.Printf("kick clients\n")
	for _, c := range clients {
		c.start <- true
	}

	log.Printf("wait for clients\n")
	done.Wait()

	log.Printf("all clients done\n")
}

// TestDelay performs several ReqRepl transactions with increasing delays
// between them.
func TestDelay(t *testing.T) {
	var lastDelay time.Duration

	comm, err := NewAPClient("test_client", servers[0].url)
	if err != nil {
		t.Errorf("NewAPClient failed: %v", err)
	}
	defer comm.Close()

	delay := time.Second
	for delay < 10*time.Second {
		if lastDelay > 0 {
			log.Printf("transaction after delay of %v\n", lastDelay)
		}

		data := []byte("test at: " + time.Now().Format(time.Stamp))
		reply, err := comm.ReqRepl(data)
		if err != nil {
			t.Errorf("ReqRepl failed: %v", err)
		}

		expected := servers[0].cb(data)
		if string(reply) != string(expected) {
			t.Errorf("Got back '%s'  Expected '%s'", string(reply), data)
		}
		time.Sleep(delay)
		lastDelay = delay
		delay *= 2
	}
}

func TestMain(m *testing.M) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	for _, server := range servers {
		s, err := NewAPServer(server.name, server.url)
		if err != nil {
			log.Fatalf("failed to open %s: %v", server.url, err)
		}
		defer s.Close()
		go s.Serve(server.cb)
	}

	// Give the servers time to open their sockets
	time.Sleep(2 * time.Second)

	os.Exit(m.Run())
}

