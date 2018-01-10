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
	"bufio"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
)

const (
	blockfileName = "ip_blocklist.csv"
)

var (
	filter       [4][1024]uint64             // 4-layer bloom filter
	blockedIPs   = make(map[uint32]struct{}) // known bad IPs
	activeBlocks = make(map[uint32]struct{}) // IPs being actively blocked

	blockPeriod = time.Hour
)

// The full blocklist is maintained in an ipv4-indexed map.  We use a simple
// bloom filter as a pre-check to reduce the number of times we have to do a
// full map lookup.  The structure and order of the hashes was hand-crafted
// based on a list of 52,000 blocked IPs aggregated by CriticalStack.  Using
// half as training and half as testing, this resulted in a false positive rate
// of ~10%.  Almost 70% of negatives were detected on the first hash lookup.

func buildHashes(addr uint32) []uint16 {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, addr)

	h0 := uint16(b[3])<<8 | uint16(b[2])
	h1 := uint16(b[3])<<8 | uint16(b[1])
	h2 := uint16(b[2])<<8 | uint16(b[1])
	h3 := uint16(b[3])<<8 | uint16(b[0])
	return []uint16{h0, h1, h2, h3}
}

// Translate an index number into a (word, bit) bitmap offset
func getPosition(v uint16) (uint16, uint16) {
	const shift = 6
	const mask = (1 << shift) - 1

	return v >> shift, v & mask
}

// Add an IP address to the block list.  Each address gets stored in the
// blockedAddr map and we update the bloom filter.
func blocklistAdd(addr uint32) {
	blockedIPs[addr] = struct{}{}

	hashes := buildHashes(addr)
	for h := 0; h < len(hashes); h++ {
		word, bit := getPosition(hashes[h])
		filter[h][word] |= (1 << bit)
	}
}

// Look to see whether an IP address is in the block list
func blocklistLookup(addr uint32) bool {
	// We use a bloom filter as a quick check to determine whether we
	// need to do a full map lookup.
	hashes := buildHashes(addr)
	for h := 0; h < len(hashes); h++ {
		word, bit := getPosition(hashes[h])
		if filter[h][word]&(1<<bit) == 0 {
			return false
		}
	}
	_, blocked := blockedIPs[addr]

	// XXX: It's probably worth creating a small MRU cache of IP addresses
	// that make it through the bloom filter and aren't in the blocked list.
	// If we get unlucky and a high-traffic address (e.g., a Netflix content
	// server) escapes the filter, we're going to be doing a ton of map
	// lookups for the same IP address.

	return blocked
}

func blockExpired(path []string) {
	var ip net.IP

	if len(path) > 2 {
		ip = net.ParseIP(path[2])
	}
	if ip != nil {
		log.Printf("removing %v from actively blocked IPs\n", ip)
		addr := network.IPAddrToUint32(ip)
		delete(activeBlocks, addr)
	}
}

func notifyBlockEvent(dev net.HardwareAddr, ip net.IP) {
	protocol := base_msg.Protocol_IP
	reason := base_msg.EventNetException_BLOCKED_IP
	topic := base_def.TOPIC_EXCEPTION

	entity := &base_msg.EventNetException{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(brokerd.Name),
		Debug:       proto.String("-"),
		Protocol:    &protocol,
		Reason:      &reason,
		MacAddress:  proto.Uint64(network.HWAddrToUint64(dev)),
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(ip)),
	}

	if err := brokerd.Publish(entity, topic); err != nil {
		log.Printf("couldn't publish %s (%v): %v\n", topic, entity, err)
	}
}

// Check to see whether the given IP address is in the block list.  If it is,
// then we add a config property indicating that it should be blocked, which
// will in turn cause networkd to insert an iptables rule to implement the
// block.
func checkBlock(dev net.HardwareAddr, ip net.IP) {
	addr := network.IPAddrToUint32(ip)
	if addr != 0 && blocklistLookup(addr) {
		_, ok := activeBlocks[addr]
		if ok {
			// We've already sent the block notification, but it
			// takes a little time for that to finally be enshrined
			// in an iptable rule.
			return
		}

		log.Printf("%v is talking with blocked IP %v\n", dev, ip)
		activeBlocks[addr] = struct{}{}
		notifyBlockEvent(dev, ip)

		// Create a property for this IP, which will cause networkd to
		// add a new firewall rule blocking it.  We set an expiration
		// time for the block to avoid an ever-growing list of iptables
		// rules.
		//
		// XXX: instead of a constant timeout, we could implement some
		// sort of exponentially increasing timeout to handle persistent
		// threats.  That would require maintaining an ever-growing list
		// of expired blocks, which would need to be periodically
		// culled.  For simplicity in this initial implementation, we'll
		// live with having to re-block an address once an hour.

		prop := fmt.Sprintf("@/firewall/active/%v", ip)
		expires := time.Now().Add(blockPeriod)
		config.CreateProp(prop, "", &expires)
	}
}

// Pull a list of blocked IPs from a CSV.  The first field of each line must be
// an IP address.  The rest of the line is ignored.
func ingestBlocklist(file string) {
	csvFile, err := os.Open(file)
	if err != nil {
		log.Printf("Unable to open %s: %v\n", file, err)
		return
	}
	defer csvFile.Close()

	lineNo := 0
	cnt := 0
	reader := csv.NewReader(bufio.NewReader(csvFile))
	for {
		line, err := reader.Read()
		if err != nil {
			if err != io.EOF {
				log.Printf("failed to read line %d of %s %v\n",
					lineNo, file, err)
			}
			break
		}
		if ip := net.ParseIP(line[0]); ip != nil {
			if addr := network.IPAddrToUint32(ip); addr != 0 {
				cnt++
				blocklistAdd(addr)
			}
		}
		lineNo++
	}

	log.Printf("Ingested %d blocked IPs from %s\n", cnt, file)
}

func blocklistInit() {
	filepath := *watchDir + "/" + blockfileName
	if aputil.FileExists(filepath) {
		ingestBlocklist(filepath)
	}
}
