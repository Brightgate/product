/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"time"

	"bg/ap_common/aputil"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	zmq "github.com/pebbe/zmq4"
	"go.uber.org/zap"
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
	slog         *zap.SugaredLogger
	publisherMtx sync.Mutex
	publisher    *zmq.Socket
	subscriber   *zmq.Socket
	handlers     map[string][]handlerF
	sync.Mutex
}

// Ping will send a single ping message to ap.brokerd
func (b *Broker) Ping() error {
	ping := &base_msg.EventPing{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(b.Name),
		Debug:       proto.String("-"),
		PingMessage: proto.String("-"),
	}

	err := b.Publish(ping, base_def.TOPIC_PING)
	if err != nil {
		err = fmt.Errorf("couldn't ping: %v", err)
	}

	return err
}

// Publish first marshals the protobuf into its wire format and then sends the
// resulting data on the broker's ZMQ socket
func (b *Broker) Publish(pb proto.Message, topic string) error {
	if b == nil {
		return nil
	}

	data, err := proto.Marshal(pb)
	if err != nil {
		return fmt.Errorf("error marshaling %s: %v", topic, err)
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
	tlog := aputil.GetThrottledLogger(b.slog, time.Second, 15*time.Second)

	for {
		msg, err := b.subscriber.RecvMessageBytes(0)
		if err != nil {
			tlog.Errorf("listener failed to receive: %v", err)
			continue
		}

		topic := string(msg[0])
		b.Lock()
		hdlrs, ok := b.handlers[topic]
		b.Unlock()
		if ok && len(hdlrs) > 0 {
			for _, hdlr := range hdlrs {
				hdlr(msg[1])
			}
		} else if debug {
			if ok {
				b.slog.Debugf("ignoring topic: %s", topic)
			} else {
				b.slog.Debugf("unknown topic: %s", topic)
			}
		}
	}
}

// Handle adds a new callback function for the identified topic.
func (b *Broker) Handle(topic string, handler handlerF) {
	if b == nil {
		return
	}

	b.Lock()
	if b.handlers[topic] == nil {
		b.handlers[topic] = make([]handlerF, 0)
	}
	b.handlers[topic] = append(b.handlers[topic], handler)
	b.Unlock()
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
		b.slog.Fatalf("Unable to connect to broker subscribe: %v", err)
	}
	b.subscriber.SetSubscribe("")

	b.publisher, _ = zmq.NewSocket(zmq.PUB)
	err = b.publisher.Connect(host + base_def.BROKER_ZMQ_PUB_PORT)
	if err != nil {
		b.slog.Fatalf("Unable to connect to broker publish: %v", err)
	}

	go eventListener(b)
}

// Fini closes the subscriber's connection to the broker
func (b *Broker) Fini() {
	if b == nil {
		return
	}

	b.subscriber.Close()
}

// NewBroker allocates a brokers structure and establishes a network connection
// to the broker daemon.
func NewBroker(slog *zap.SugaredLogger, name string) *Broker {
	if len(name) == 0 {
		log.Fatalf("Broker consumer must give its name")
	}

	if slog == nil {
		log.Fatalf("Broker consumer must provide a logger")
	}

	b := Broker{
		Name:     fmt.Sprintf("%s(%d)", name, os.Getpid()),
		slog:     slog,
		handlers: make(map[string][]handlerF),
	}

	// Add placeholder handlers in the map for known topics
	for _, v := range knownTopics {
		b.handlers[v] = nil
	}

	b.connect()
	if err := b.Ping(); err != nil {
		b.slog.Fatalf("initial ping failed: %v", err)
	}
	return &b
}
