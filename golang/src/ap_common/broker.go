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
var known_topics = [5]string{
	base_def.TOPIC_PING,
	base_def.TOPIC_CONFIG,
	base_def.TOPIC_ENTITY,
	base_def.TOPIC_RESOURCE,
	base_def.TOPIC_REQUEST,
}

type hdlr_f func(event []byte)

type Broker struct {
	name          string
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
		Sender:      proto.String(b.name),
		Debug:       proto.String("-"),
		PingMessage: proto.String("-"),
	}

	data, err := proto.Marshal(ping)
	if err != nil {
		log.Printf("Error marshalling ping: %v\n", err)
	} else {
		err = b.Publish(base_def.TOPIC_PING, data)
		if err != nil {
			log.Printf("Error sending ping: %v\n", err)
		}
	}
}

func (b *Broker) Publish(topic string, event []byte) error {
	b.publisher_mtx.Lock()
	_, err := b.publisher.SendMessage(topic, event)
	b.publisher_mtx.Unlock()

	return err
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
		if !ok {
			log.Printf("[%s] unknown topic: %s\n", b.name, topic)
			continue
		}
		if hdlr == nil {
			log.Printf("[%s] ignoring topic: %s\n", b.name, topic)
			continue
		}

		hdlr(msg[1])
	}
}

func (b *Broker) Handle(topic string, handler hdlr_f) {
	if len(b.name) == 0 {
		log.Panic("Broker hasn't been initialized yet")
	}
	b.handlers[topic] = handler
}

func (b *Broker) Disconnect() {
	b.subscriber.Close()
}

func (b *Broker) Connect() {
	if len(b.name) == 0 {
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
	if len(b.name) > 0 {
		log.Panic("Broker can't be initialized multiple times")
	}
	if len(name) == 0 {
		log.Panic("Broker consumer must give its name")
	}

	b.name = fmt.Sprintf("%s(%d)", name, os.Getpid())
	// Add placeholder handlers in the map for known topics
	b.handlers = make(map[string]hdlr_f)
	for _, v := range known_topics {
		b.handlers[v] = nil
	}
}