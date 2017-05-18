/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

/*
 * DHCPv4 daemon
 */

// XXX Exception messages are not displayed.
// XXX Hardcoded to wlan0.

package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"base_def"
	"base_msg"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/golang/protobuf/proto"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"

	dhcp "github.com/krolaw/dhcp4"
)

var addr = flag.String("promhttp-address", ":"+strconv.Itoa(base_def.DHCPD_PROMETHEUS_PORT),
	"Prometheus publication HTTP port.")

var publisher_mtx sync.Mutex
var publisher *zmq.Socket

var client_map_mtx sync.Mutex
var client_map map[string]int64

func bus_listener() {
	// We need to listen for "config" channel events, since that's
	// how we would become aware of static lease assignment changes
	// (among other things).

	// First, connect our subscriber socket
	subscriber, _ := zmq.NewSocket(zmq.SUB)
	defer subscriber.Close()
	subscriber.Connect("tcp://localhost:" + strconv.Itoa(base_def.BROKER_ZMQ_SUB_PORT))
	subscriber.SetSubscribe("")

	for {
		msg, err := subscriber.RecvMessageBytes(0)
		if err != nil {
			log.Println(err)
			break
		}

		topic := string(msg[0])

		if topic != base_def.TOPIC_CONFIG {
			continue
		}

		config := &base_msg.EventConfig{}
		proto.Unmarshal(msg[1], config)
		log.Println(config)

		/*
		 * XXX Decide whether this configuration change affects
		 * us.
		 */
	}
}

type lease struct {
	nic    string    // Client's CHAddr
	expiry time.Time // When the lease expires
}

type DHCPHandler struct {
	ip            net.IP        // Server IP to use
	options       dhcp.Options  // Options to send to DHCP Clients
	start         net.IP        // Start of IP range to distribute
	leaseRange    int           // Number of IPs to distribute (starting from start)
	leaseDuration time.Duration // Lease period
	leases        map[int]lease // Map to keep track of leases
}

var StaticMap map[string]net.IP

func hwaddr_to_uint64(ha net.HardwareAddr) uint64 {
	ext_hwaddr := make([]byte, 8)
	ext_hwaddr[0] = 0
	ext_hwaddr[1] = 0
	copy(ext_hwaddr[2:], ha)

	return binary.BigEndian.Uint64(ext_hwaddr)
}

func (h *DHCPHandler) ServeDHCP(p dhcp.Packet, msgType dhcp.MessageType, options dhcp.Options) (d dhcp.Packet) {
	t := time.Now()

	hwaddr := p.CHAddr().String()
	hwaddr_u64 := hwaddr_to_uint64(p.CHAddr())

	client_map_mtx.Lock()
	client_map[hwaddr] = client_map[hwaddr] + 1
	log.Printf("client %s, map[client] %d\n", hwaddr, client_map[hwaddr])

	if client_map[hwaddr] == 1 {
		ipaddr := p.CIAddr()

		entity := &base_msg.EventNetEntity{
			Timestamp: &base_msg.Timestamp{
				Seconds: proto.Int64(t.Unix()),
				Nanos:   proto.Int32(int32(t.Nanosecond())),
			},
			Sender:      proto.String(fmt.Sprintf("ap.dhcp4d(%d)", os.Getpid())),
			Debug:       proto.String("-"),
			MacAddress:  proto.Uint64(hwaddr_u64),
			Ipv4Address: proto.Uint32(binary.BigEndian.Uint32(ipaddr)),
		}

		data, err := proto.Marshal(entity)
		if err != nil {
			log.Printf("entity couldn't marshal: %v", err)
		} else {
			publisher_mtx.Lock()
			_, err = publisher.SendMessage(base_def.TOPIC_ENTITY, data)
			if err != nil {
				log.Printf("couldn't send %v", err)
			}
			publisher_mtx.Unlock()
		}
	}

	client_map_mtx.Unlock()

	switch msgType {

	case dhcp.Discover:
		free, nic := -1, p.CHAddr().String()
		for i, v := range h.leases { // Find previous lease
			if v.nic == nic {
				free = i
				goto reply
			}
		}
		if free = h.freeLease(); free == -1 {
			return
		}
	reply:
		return dhcp.ReplyPacket(p, dhcp.Offer, h.ip, dhcp.IPAdd(h.start, free), h.leaseDuration,
			h.options.SelectOrderOrAll(options[dhcp.OptionParameterRequestList]))

	case dhcp.Request:
		if server, ok := options[dhcp.OptionServerIdentifier]; ok && !net.IP(server).Equal(h.ip) {
			return nil // Message not for this dhcp server
		}

		t := time.Now()

		protocol := base_msg.Protocol_DHCP
		entity := &base_msg.EventNetRequest{
			Timestamp: &base_msg.Timestamp{
				Seconds: proto.Int64(t.Unix()),
				Nanos:   proto.Int32(int32(t.Nanosecond())),
			},
			Sender:    proto.String(fmt.Sprintf("ap.dhcp4d(%d)", os.Getpid())),
			Debug:     proto.String("-"),
			Protocol:  &protocol,
			Requestor: proto.String(p.CHAddr().String()),
		}

		data, err := proto.Marshal(entity)
		if err != nil {
			log.Printf("entity couldn't marshal: %v", err)
		} else {
			publisher_mtx.Lock()
			_, err = publisher.SendMessage(base_def.TOPIC_REQUEST, data)
			if err != nil {
				log.Printf("couldn't send %v", err)
			}
			publisher_mtx.Unlock()
		}

		/*
		 * Based on p.CHAddr() (or a client ID), look up in the
		 * static lease map for an assignment.  Presence of an
		 * assignment would override an optional request.
		 */
		var reqIP net.IP

		static_candidate := StaticMap[p.CHAddr().String()]
		request_option := net.IP(options[dhcp.OptionRequestedIPAddress])

		log.Printf("request static? %v option? %v client? %v", static_candidate, request_option, p.CIAddr())

		if static_candidate != nil {
			reqIP = static_candidate
		} else if request_option != nil {
			reqIP = request_option
		} else {
			reqIP = net.IP(p.CIAddr())
		}

		if len(reqIP) == 4 && !reqIP.Equal(net.IPv4zero) {
			if leaseNum := dhcp.IPRange(h.start, reqIP) - 1; leaseNum >= 0 && leaseNum < h.leaseRange {
				if l, exists := h.leases[leaseNum]; !exists || l.nic == p.CHAddr().String() {
					h.leases[leaseNum] = lease{nic: p.CHAddr().String(), expiry: time.Now().Add(h.leaseDuration)}
					t := time.Now()

					action := base_msg.EventNetResource_CLAIMED
					entity := &base_msg.EventNetResource{
						Timestamp: &base_msg.Timestamp{
							Seconds: proto.Int64(t.Unix()),
							Nanos:   proto.Int32(int32(t.Nanosecond())),
						},
						Sender: proto.String(fmt.Sprintf("ap.dhcp4d(%d)", os.Getpid())),
						Debug:  proto.String("-"),
						Action: &action,
					}

					data, err := proto.Marshal(entity)
					if err != nil {
						log.Printf("entity couldn't marshal: %v", err)
					} else {
						publisher_mtx.Lock()
						_, err = publisher.SendMessage(base_def.TOPIC_RESOURCE, data)
						if err != nil {
							log.Printf("couldn't send %v", err)
						}
						publisher_mtx.Unlock()
					}

					return dhcp.ReplyPacket(p, dhcp.ACK, h.ip, reqIP, h.leaseDuration,
						h.options.SelectOrderOrAll(options[dhcp.OptionParameterRequestList]))
				}
			}
		}
		return dhcp.ReplyPacket(p, dhcp.NAK, h.ip, nil, 0, nil)

	case dhcp.Release, dhcp.Decline:
		nic := p.CHAddr().String()
		for i, v := range h.leases {
			if v.nic == nic {
				delete(h.leases, i)
				t := time.Now()

				action := base_msg.EventNetResource_RELEASED
				entity := &base_msg.EventNetResource{
					Timestamp: &base_msg.Timestamp{
						Seconds: proto.Int64(t.Unix()),
						Nanos:   proto.Int32(int32(t.Nanosecond())),
					},
					Sender: proto.String(fmt.Sprintf("ap.dhcp4d(%d)", os.Getpid())),
					Debug:  proto.String("-"),
					Action: &action,
				}

				data, err := proto.Marshal(entity)
				if err != nil {
					log.Printf("entity couldn't marshal: %v", err)
				} else {
					publisher_mtx.Lock()
					_, err = publisher.SendMessage(base_def.TOPIC_RESOURCE, data)
					if err != nil {
						log.Printf("couldn't send %v", err)
					}
					publisher_mtx.Unlock()
				}
				break
			}
		}
	}
	return nil
}

func (h *DHCPHandler) freeLease() int {
	now := time.Now()
	b := rand.Intn(h.leaseRange) // Try random first
	for _, v := range [][]int{[]int{b, h.leaseRange}, []int{0, b}} {
		for i := v[0]; i < v[1]; i++ {
			if l, ok := h.leases[i]; !ok || l.expiry.Before(now) {
				return i
			}
		}
	}
	return -1
}

func InitServeDHCP() {
	// # XXX retrieve my server address
	// #   XXX one service instance per separate network?
	// # XXX network configurations
	serverIP := net.IP{192, 168, 136, 1}
	handler := &DHCPHandler{
		ip:            serverIP,
		leaseDuration: 2 * time.Hour,
		start:         net.IP{192, 168, 136, 10},
		leaseRange:    90,
		leases:        make(map[int]lease, 10),
		options: dhcp.Options{
			dhcp.OptionSubnetMask:       []byte{255, 255, 255, 0},
			dhcp.OptionRouter:           []byte(serverIP),
			dhcp.OptionDomainNameServer: []byte(serverIP),
		},
	}
	log.Fatal(dhcp.ListenAndServeIf("wlan0", handler))
}

// # XXX quarantine range
// # XXX trusted dns, untrusted dns
// # XXX compose DHCPREPLY packet
// # XXX event log: host discovery from DHCPDISCOVER
//
// # XXX intent: siphon/monitor or active
//
// # XXX allocator statistics
// st_allocated = attr.ib(init=False, default=0)
// st_freed = attr.ib(init=False, default=0)
// st_request_fail_range = attr.ib(init=False, default=0)
// st_request_fail_busy = attr.ib(init=False, default=0)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("start")

	//     # Need to have certain network capabilities.
	//     priv_net_bind_service = prctl.cap_effective.net_bind_service
	//     priv_net_broadcast = prctl.cap_effective.net_broadcast
	//
	//     if not priv_net_bind_service:
	//         logging.warning("require CAP_NET_BIND_SERVICE to bind DHCP server port")
	//         sys.exit(1)
	//     if not priv_net_broadcast:
	//         logging.warning("require CAP_NET_BROADCAST to acquire broadcast packets")
	//         sys.exit(1)

	//     # XXX configuration retrieval

	flag.Parse()

	log.Println("cli flags parsed")

	var zerr error
	publisher, zerr = zmq.NewSocket(zmq.PUB)
	if zerr != nil {
		log.Printf("couldn't get a PUB socket: %v", zerr)
	}
	publisher.Connect(base_def.APPLIANCE_ZMQ_URL + ":" + strconv.Itoa(base_def.BROKER_ZMQ_PUB_PORT))

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client launched")

	go bus_listener()

	log.Println("message bus listener routine launched")

	t := time.Now()

	ping := &base_msg.EventPing{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:      proto.String(fmt.Sprintf("ap.dhcp4d(%d)", os.Getpid())),
		Debug:       proto.String("-"),
		PingMessage: proto.String("-"),
	}

	data, err := proto.Marshal(ping)

	publisher_mtx.Lock()
	_, err = publisher.SendMessage(base_def.TOPIC_PING, data)
	if err != nil {
		log.Println(err)
	}
	publisher_mtx.Unlock()

	client_map = make(map[string]int64)

	StaticMap = make(map[string]net.IP)

	/*
	 * To be read from a configuration store, either each query or
	 * on initialization and on sys.config events.
	 */
	StaticMap["cc:61:e5:cd:6a:f5"] = net.IP{192, 168, 136, 77}

	InitServeDHCP()
}
