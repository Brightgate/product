/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package archive

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

const smacPresent = byte(1 << 7)

var (
	protoToString = map[byte]string{
		1: "tcp",
		2: "udp",
	}
	stringToProto = map[string]byte{
		"tcp": 1,
		"udp": 2,
	}
)

// translate an a.b.c.d:port string into 4 bytes of IP and 2 bytes of port#
func endpointToBinary(e string) []byte {
	rval := make([]byte, 6)
	f := strings.Split(e, ":")
	if len(f) == 2 {
		ip := net.ParseIP(f[0]).To4()
		port, _ := strconv.Atoi(f[1])
		rval[0] = ip[0]
		rval[1] = ip[1]
		rval[2] = ip[2]
		rval[3] = ip[3]
		rval[4] = byte(port & 0xff)
		rval[5] = byte((port >> 8) & 0xff)
	}
	return rval
}

// translate 4 bytes of IP and 2 bytes of port# into an a.b.c.d:port string
func endpointFromBinary(b []byte) string {
	if len(b) != 6 {
		return "bad endpoint"
	}

	port := (uint32(b[5]) << 8) | uint32(b[4])
	return fmt.Sprintf("%d.%d.%d.%d:%d", b[0], b[1], b[2], b[3], port)
}

func macToBinary(mac string) []byte {
	h, _ := net.ParseMAC(mac)
	if len(h) == 6 {
		return h
	}
	return make([]byte, 6)
}

func macFromBinary(b []byte) string {
	if len(b) != 6 {
		return "bad mac address"
	}

	rval := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		b[0], b[1], b[2], b[3], b[4], b[5])
	if rval == "00:00:00:00:00:00" {
		rval = ""
	}
	return rval
}

// MarshalBinary implements the encoding.BinaryMarshaler interface, providing a
// custom, space-saving format for DropRecords.
func (r DropRecord) MarshalBinary() ([]byte, error) {
	enc := make([]byte, 0)

	tenc, err := r.Time.MarshalBinary()
	if err != nil {
		return nil, err
	}
	tlen := byte(len(tenc))
	enc = append(enc, tlen)
	enc = append(enc, tenc...)

	l := byte(len(r.Indev))
	enc = append(enc, l)
	enc = append(enc, []byte(r.Indev)...)

	pbyte := stringToProto[strings.ToLower(r.Proto)]
	if r.Smac != "" {
		pbyte = pbyte | smacPresent
	}
	enc = append(enc, pbyte)

	enc = append(enc, endpointToBinary(r.Src)...)
	enc = append(enc, endpointToBinary(r.Dst)...)
	if r.Smac != "" {
		enc = append(enc, macToBinary(r.Smac)...)
	}

	return enc, nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface,
// providing a custom, space-saving format for DropRecords.
func (r *DropRecord) UnmarshalBinary(b []byte) error {
	tlen := b[0]
	tenc := b[1 : tlen+1]
	b = b[tlen+1:]

	if err := (&r.Time).UnmarshalBinary(tenc); err != nil {
		return fmt.Errorf("unmarshaling timestamp: %v", err)
	}

	l := b[0]
	r.Indev = string(b[1 : l+1])
	b = b[l+1:]

	pbyte := b[0]
	r.Proto = protoToString[pbyte&0x07]
	r.Src = endpointFromBinary(b[1:7])
	r.Dst = endpointFromBinary(b[7:13])
	if pbyte&smacPresent != 0 {
		r.Smac = macFromBinary(b[13:19])
	}

	return nil
}
