/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package broker

import (
	"fmt"
	"log"
	"os"
	"sync"

	"bg/ap_common/aputil"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	zmq "github.com/pebbe/zmq4"
)

// Clients can request to be notified of any topic they wish.  This list
// represents the common topics that we know about, allowing us distinguish
// between topics a daemon doesn't recognize and those it is choosing to ignore.
// This has no functional impact, other than being logged differently in
// event_listener()
var knownTopics = [7]string{
	base_def.TOPIC_PING,
	base_def.TOPIC_CONFIG,
	base_def.TOPIC_ENTITY,
	base_def.TOPIC_RESOURCE,
	base_def.TOPIC_REQUEST,
	base_def.TOPIC_SCAN,
	base_def.TOPIC_IDENTITY,
}

var debug = false

type handlerF func(event []byte)

// Broker is an opaque handle used by daemons to communicate with ap.brokerd
type Broker struct {
	Name         string
	publisherMtx sync.Mutex
	publisher    *zmq.Socket
	subscriber   *zmq.Socket
	handlers     map[string]handlerF
}

// Ping will send a single ping message to ap.brokerd
func (b *Broker) Ping() {
	ping := &base_msg.EventPing{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(b.Name),
		Debug:       proto.String("-"),
		PingMessage: proto.String("-"),
	}

	err := b.Publish(ping, base_def.TOPIC_PING)
	if err != nil {
		log.Printf("couldn't publish %s for %s: %v\n",
			base_def.TOPIC_PING, b.Name, err)
	}
}

// Publish first marshals the protobuf into its wire format and then sends the
// resulting data on the broker's ZMQ socket
func (b *Broker) Publish(pb proto.Message, topic string) error {
	data, err := proto.Marshal(pb)
	if err != nil {
		return fmt.Errorf("error marshalling %s: %v", topic, err)
	}

	b.publisherMtx.Lock()
	_, err = b.publisher.SendMessage(topic, data)
	b.publisherMtx.Unlock()
	if err != nil {
		return fmt.Errorf("error sending %s: %v", topic, err)
	}

	return nil
}

func eventListener(b *Broker) {
	for {
		msg, err := b.subscriber.RecvMessageBytes(0)
		if err != nil {
			log.Printf("listener for %s failed to receive: %s\n", b.Name, err)
			continue
		}

		topic := string(msg[0])
		hdlr, ok := b.handlers[topic]
		if ok && hdlr != nil {
			hdlr(msg[1])
		} else if debug {
			if ok {
				log.Printf("[%s] ignoring topic: %s\n",
					b.Name, topic)
			} else {
				log.Printf("[%s] unknown topic: %s\n",
					b.Name, topic)
			}
		}
	}
}

// Handle adds a new callback function for the identified topic.  This will
// replace an existing handler for that topic.
func (b *Broker) Handle(topic string, handler handlerF) {
	b.handlers[topic] = handler
}

func (b *Broker) connect() {
	var host string

	if aputil.IsSatelliteMode() {
		host = base_def.GATEWAY_ZMQ_URL
	} else {
		host = base_def.LOCAL_ZMQ_URL
	}
	s, _ := zmq.NewSocket(zmq.SUB)
	b.subscriber = s
	err := b.subscriber.Connect(host + base_def.BROKER_ZMQ_SUB_PORT)
	if err != nil {
		log.Fatalf("Unable to connect to broker subscribe: %v\n", err)
	}
	b.subscriber.SetSubscribe("")

	b.publisher, _ = zmq.NewSocket(zmq.PUB)
	err = b.publisher.Connect(host + base_def.BROKER_ZMQ_PUB_PORT)
	if err != nil {
		log.Fatalf("Unable to connect to broker publish: %v\n", err)
	}

	go eventListener(b)
}

// Fini closes the subscriber's connection to the broker
func (b *Broker) Fini() {
	b.subscriber.Close()
}

// New allocates a brokers structure and establishes a network connection to
// the broker daemon.
func New(name string) *Broker {
	if len(name) == 0 {
		log.Printf("Broker consumer must give its name\n")
		return nil
	}

	b := Broker{
		Name:     fmt.Sprintf("%s(%d)", name, os.Getpid()),
		handlers: make(map[string]handlerF),
	}

	// Add placeholder handlers in the map for known topics
	for _, v := range knownTopics {
		b.handlers[v] = nil
	}

	b.connect()
	b.Ping()
	return &b
}
