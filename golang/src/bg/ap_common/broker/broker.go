/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package broker

import (
	"bytes"
	"fmt"
	"os"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"nanomsg.org/go/mangos/v2"
	"nanomsg.org/go/mangos/v2/protocol/bus"
	// Importing the TCP transport
	_ "nanomsg.org/go/mangos/v2/transport/tcp"
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
	Name     string
	slog     *zap.SugaredLogger
	socket   mangos.Socket
	handlers map[string][]handlerF
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
// resulting data on the broker's COMM socket
func (b *Broker) Publish(pb proto.Message, topic string) error {
	if b == nil {
		return nil
	}

	data, err := proto.Marshal(pb)
	if err != nil {
		return fmt.Errorf("error marshaling %s: %v", topic, err)
	}

	// Pack the topic and data into a single buffer
	dataOff := len(topic) + 1
	msg := make([]byte, dataOff+len(data))
	copy(msg, []byte(topic))
	copy(msg[dataOff:], data)

	b.Lock()
	err = b.socket.Send(msg)
	b.Unlock()

	if err != nil {
		return fmt.Errorf("error sending %s: %v", topic, err)
	}

	return nil
}

func eventListener(b *Broker) {
	tlog := aputil.GetThrottledLogger(b.slog, time.Second, 15*time.Second)

	for {
		data, err := b.socket.Recv()
		if err != nil {
			tlog.Errorf("listener failed to receive: %v", err)
			continue
		}

		// The message should contain a NULL-terminated topic string
		// and a binary data blob
		dataOff := bytes.IndexByte(data, 0) + 1
		if dataOff <= 0 || dataOff >= len(data) {
			tlog.Errorf("invalid message")
			continue
		}
		topic := string(data[:dataOff-1])

		b.Lock()
		hdlrs, ok := b.handlers[topic]
		b.Unlock()

		if ok && len(hdlrs) > 0 {
			for _, hdlr := range hdlrs {
				hdlr(data[dataOff:])
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

func (b *Broker) connect() error {
	var err error

	if b.socket, err = bus.NewSocket(); err != nil {
		err = errors.Wrap(err, "creating subscriber socket")

	} else {
		url := aputil.GatewayURL(base_def.BROKER_COMM_BUS_PORT)
		if err = b.socket.Dial(url); err != nil {
			err = errors.Wrapf(err, "connecting to %s", url)
		} else {
			go eventListener(b)
		}
	}

	return err
}

// Fini closes the subscriber's connection to the broker
func (b *Broker) Fini() {
	if b == nil {
		return
	}

	b.socket.Close()
}

// NewBroker allocates a brokers structure and establishes a network connection
// to the broker daemon.
func NewBroker(slog *zap.SugaredLogger, name string) (*Broker, error) {
	if len(name) == 0 {
		return nil, fmt.Errorf("Broker consumer must give its name")
	}

	if slog == nil {
		return nil, fmt.Errorf("Broker consumer must provide a logger")
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

	if err := b.connect(); err != nil {
		return nil, err
	}

	if err := b.Ping(); err != nil {
		return nil, errors.Wrap(err, "initial broker ping failed")
	}
	return &b, nil
}
