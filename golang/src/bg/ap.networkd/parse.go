/*
 * Copyright 2018 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

// Rules look loke:
//     <Action> <Protocol> [FROM <endpoint>] [TO <endpoint>] [PORTS] [TIME]
//
// Actions:
// --------
// BLOCK
// ACCEPT
// CAPTURE
//
// Protocol:
// ---------
// UDP
// TCP
// ICMP
// IP?
//
// Endpoint:  <kind> <detail>
// --------
// ADDR  CIDR
// RING  ring_name
// TYPE  client_type
// IFACE wan/lan
// AP
//
// Ports: (DPORTS|SPORTS) <port list>
//
// Time:
// -----
// AFTER <time>
// BEFORE <time>
// BETWEEN <time> <time>

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	actionAccept = iota
	actionBlock
	actionCapture
)

const (
	protoAll = iota
	protoUDP
	protoTCP
	protoIP
	protoICMP
	protoMAX
)

const (
	endpointAddr = iota
	endpointType
	endpointRing
	endpointIface
	endpointAP
	endpointMAX
)

// Endpoint represents either the FROM or TO endpoint of a filter rule.
type endpoint struct {
	kind   int
	detail string
	addr   *net.IPNet
	not    bool
}

// A single parsed filter rule
type rule struct {
	text   string
	action int
	proto  int
	from   *endpoint
	to     *endpoint
	sports []uint64
	dports []uint64
	start  *time.Time
	end    *time.Time
}

type ruleList []*rule

//
// Read a rule from the file.  Drop all comments and extra whitespace.  Join
// rules that span multiple lines
//
func getRule(r *bufio.Reader) (rule string, err error) {
	cont := true
	for cont {
		var l []byte

		l, cont, err = r.ReadLine()
		if err != nil {
			break
		}

		// Drop comments (i.e., everything after a "#"
		s := strings.Split(string(l), "#")

		// Trim whitespace on front and back
		text := strings.TrimSpace(s[0])

		if strings.HasSuffix(text, `\`) {
			text = strings.TrimSuffix(text, `\`)
			cont = true
		}

		rule += text
		if len(rule) == 0 {
			// Ignore blank lines
			cont = true
		}
	}
	return
}

func getAction(t string) (action int, err error) {
	switch strings.ToUpper(t) {
	case "ACCEPT":
		action = actionAccept
	case "BLOCK":
		action = actionBlock
	case "CAPTURE":
		action = actionCapture
	default:
		err = fmt.Errorf("Unrecognized action: %s", t)
	}
	return
}

func getProtocol(p string) (proto int, err error) {
	err = nil
	proto = protoAll

	switch strings.ToUpper(p) {
	case "FROM":
	case "TO":
	case "BEFORE":
	case "AFTER":
	case "BETWEEN":
		// The PROTOCOL field is optional.  If we find another keyword,
		// we know it was elided.

	case "UDP":
		proto = protoUDP
	case "TCP":
		proto = protoTCP
	case "ICMP":
		proto = protoICMP
	case "IP":
		proto = protoIP
	default:
		err = fmt.Errorf("Unrecognized protocol: %s", p)
	}
	return
}

func getAddr(addr string) (ipnet *net.IPNet, err error) {
	if strings.ToUpper(addr) == "ALL" {
		ipnet = nil
	} else {
		_, ipnet, err = net.ParseCIDR(addr)
	}
	return
}

// Parse (FROM|TO) (ADDR <addr>|RING <ring>|TYPE <type>|IFACE <iface>|AP)
func getEndpoint(tokens []string, name string) (ep *endpoint, cnt int, err error) {
	var e endpoint

	err = nil
	cnt = 0

	if strings.ToUpper(tokens[0]) != name {
		// Both the FROM and TO fields are optional.  If this keyword
		// doesn't match the one we were looking for, assume it was
		// elided.
		return
	}
	cnt++

	if len(tokens) < 2 {
		err = fmt.Errorf("Missing %s endpoint", name)
		return
	}

	needDetail := strings.ToUpper(tokens[1]) != "AP"
	if needDetail && len(tokens) < 3 {
		err = fmt.Errorf("invalid %s endpoint: missing detail", name)
		return
	}

	kind := tokens[cnt]
	cnt++

	e.not = (cnt < len(tokens)) && (tokens[cnt] == "NOT")
	if e.not {
		cnt++
		if cnt == len(tokens) {
			err = fmt.Errorf("Invalid %s endpoint", name)
			return
		}
	}

	if needDetail {
		e.detail = tokens[cnt]
		cnt++
	}

	switch strings.ToUpper(kind) {
	case "ADDR":
		e.kind = endpointAddr
		e.addr, err = getAddr(e.detail)
	case "RING":
		e.kind = endpointRing
	case "TYPE":
		e.kind = endpointType
	case "IFACE":
		e.kind = endpointIface
	case "AP":
		e.kind = endpointAP
	default:
		err = fmt.Errorf("Invalid kind for %s endpoint: %s", name, tokens[1])
	}

	if err == nil {
		ep = &e
	}
	return
}

func parseTime(tokens []string, num int) (*time.Time, error) {
	const timeOfDayFormat = "3:04PM"
	var t time.Time
	var err error

	if len(tokens) <= num {
		return nil, fmt.Errorf("Missing time value")
	}

	loc, _ := time.LoadLocation("Local")
	t, err = time.ParseInLocation(timeOfDayFormat, tokens[num], loc)

	return &t, err
}

func parsePort(t string) (uint64, error) {
	v, err := strconv.Atoi(t)
	if err == nil && (v < 0 || v > 65535) {
		err = fmt.Errorf("value out of range")
	}

	return uint64(v), err
}

func getPorts(tokens []string) (sports, dports []uint64, cnt int, err error) {
	var ports *[]uint64

	switch strings.ToUpper(tokens[0]) {
	case "SPORTS":
		ports = &sports
	case "DPORTS":
		ports = &dports
	default:
		return
	}

	cnt = 1
	for _, t := range tokens[1:] {
		var port uint64

		if t == "SPORTS" || t == "DPORTS" || t == "TIME" {
			// Found the next token
			break
		}

		f := strings.Split(t, ":")
		if len(f) > 2 {
			err = fmt.Errorf("invalid port range: %s", t)
			break
		}

		if port, err = parsePort(f[0]); err != nil {
			err = fmt.Errorf("invalid port %s: %v", f[0], err)
			break
		}

		if len(f) == 2 {
			var high uint64
			if high, err = parsePort(f[1]); err != nil {
				err = fmt.Errorf("invalid port %s: %v", f[1],
					err)
				break
			}
			port |= (high << 32)
		}

		cnt++
		*ports = append(*ports, port)
	}

	return
}

// Parse (BEFORE <time> | AFTER <time> | BETWEEN <time> <time>)
func getTime(tokens []string) (start, end *time.Time, cnt int, err error) {
	start = nil
	end = nil
	err = nil

	switch strings.ToUpper(tokens[0]) {
	case "BEFORE":
		cnt = 2
		end, err = parseTime(tokens, 1)
	case "AFTER":
		cnt = 2
		start, err = parseTime(tokens, 1)
	case "BETWEEN":
		cnt = 3
		start, err = parseTime(tokens, 1)
		if err == nil {
			end, err = parseTime(tokens, 2)
		}
	}

	return
}

func parseRule(text string) (r *rule, err error) {
	var c int

	r = &rule{
		text:   text,
		sports: make([]uint64, 0),
		dports: make([]uint64, 0),
	}

	tokens := strings.Split(text, " ")
	t := 0
	e := len(tokens)

	if e < 1 {
		err = fmt.Errorf("no action defined")
		return
	}

	if r.action, err = getAction(tokens[t]); err != nil {
		return
	}
	if t++; t >= e {
		err = fmt.Errorf("invalid rule")
		return
	}

	if r.proto, err = getProtocol(tokens[t]); err != nil {
		return
	}
	if r.proto != protoAll {
		if t++; t >= e {
			return nil, fmt.Errorf("invalid rule")
		}
	}

	if r.from, c, err = getEndpoint(tokens[t:], "FROM"); err != nil {
		return
	}
	if t += c; t >= e {
		return
	}

	if r.to, c, err = getEndpoint(tokens[t:], "TO"); err != nil {
		return
	}
	if t += c; t >= e {
		return
	}

	for {
		var sports, dports []uint64

		if sports, dports, c, err = getPorts(tokens[t:]); err != nil {
			return
		}
		if c == 0 {
			break
		}

		if len(sports) > 0 {
			r.sports = append(r.sports, sports...)
		} else if len(dports) > 0 {
			r.dports = append(r.dports, dports...)
		}
		if t += c; t >= e {
			return
		}
	}

	if r.start, r.end, c, err = getTime(tokens[t:]); err != nil {
		return
	}

	if t += c; t < e {
		err = fmt.Errorf("Unrecognized token: '%s'", tokens[t])
	} else if r.from == nil && r.to == nil {
		err = fmt.Errorf("Rule has no endpoints")
	}

	return r, err
}

//
// ParseRules reads a list of filter rules from a file, and returns a slice
// of Rules.
func parseRulesFile(rulesFile string) (rules ruleList, err error) {
	errcnt := 0

	file, err := os.Open(rulesFile)
	if err != nil {
		slog.Errorf("Couldn't open %s: %v", rulesFile, err)
		os.Exit(1)
	}
	defer file.Close()

	r := bufio.NewReader(file)
	for true {
		rule, err := getRule(r)
		if err == io.EOF {
			break
		}

		if err != nil {
			slog.Errorf("Failed to read %s: %v", rulesFile, err)
			break
		}
		s, err := parseRule(rule)
		if err != nil {
			slog.Warnf("Failed to parse '%s': %v", rule, err)
			if errcnt++; errcnt > 10 {
				slog.Errorf("Too many errors.  Giving up.")
				break
			}
		} else {
			rules = append(rules, s)
		}
	}

	return
}

