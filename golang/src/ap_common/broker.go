/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package ap_common

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"
	zmq "github.com/pebbe/zmq4"
)

// Clients can request to be notified of any topic they wish.  This list
// represents the common topics that we know about, allowing us distinguish
// between topics a daemon doesn't recognize and those it is choosing to ignore.
// This has no functional impact, other than being logged differently in
// event_listener()
var known_topics = [7]string{
	base_def.TOPIC_PING,
	base_def.TOPIC_CONFIG,
	base_def.TOPIC_ENTITY,
	base_def.TOPIC_RESOURCE,
	base_def.TOPIC_REQUEST,
	base_def.TOPIC_SCAN,
	base_def.TOPIC_IDENTITY,
}

var debug = false

type hdlr_f func(event []byte)

type Broker struct {
	Name          string
	publisher_mtx sync.Mutex
	publisher     *zmq.Socket
	subscriber    *zmq.Socket
	handlers      map[string]hdlr_f
}

func (b *Broker) Ping() {
	t := time.Now()

	ping := &base_msg.EventPing{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:      proto.String(b.Name),
		Debug:       proto.String("-"),
		PingMessage: proto.String("-"),
	}

	err := b.Publish(ping, base_def.TOPIC_PING)
	if err != nil {
		log.Printf("couldn't publish %s for %s: %v\n", base_def.TOPIC_PING, b.Name, err)
	}
}

// Publish first marshals the protobuf into its wire format and then sends the
// resulting data on the broker's ZMQ socket
func (b *Broker) Publish(pb proto.Message, topic string) error {
	data, err := proto.Marshal(pb)
	if err != nil {
		return fmt.Errorf("error marshalling %s: %v", topic, err)
	}

	b.publisher_mtx.Lock()
	_, err = b.publisher.SendMessage(topic, data)
	b.publisher_mtx.Unlock()
	if err != nil {
		return fmt.Errorf("error sending %s: %v", topic, err)
	}

	return nil
}

func event_listener(b *Broker) {
	for {
		msg, err := b.subscriber.RecvMessageBytes(0)
		if err != nil {
			log.Println(err)
			break
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

func (b *Broker) Handle(topic string, handler hdlr_f) {
	if len(b.Name) == 0 {
		log.Panic("Broker hasn't been initialized yet")
	}
	b.handlers[topic] = handler
}

func (b *Broker) Disconnect() {
	b.subscriber.Close()
}

func (b *Broker) Connect() {
	if len(b.Name) == 0 {
		log.Panic("Broker hasn't been initialized yet")
	}

	s, _ := zmq.NewSocket(zmq.SUB)
	b.subscriber = s
	b.subscriber.Connect(base_def.BROKER_ZMQ_SUB_URL)
	b.subscriber.SetSubscribe("")

	b.publisher, _ = zmq.NewSocket(zmq.PUB)
	b.publisher.Connect(base_def.BROKER_ZMQ_PUB_URL)

	go event_listener(b)
}

func (b *Broker) Init(name string) {
	if len(b.Name) > 0 {
		log.Panic("Broker can't be initialized multiple times")
	}
	if len(name) == 0 {
		log.Panic("Broker consumer must give its name")
	}

	b.Name = fmt.Sprintf("%s(%d)", name, os.Getpid())
	// Add placeholder handlers in the map for known topics
	b.handlers = make(map[string]hdlr_f)
	for _, v := range known_topics {
		b.handlers[v] = nil
	}
}
