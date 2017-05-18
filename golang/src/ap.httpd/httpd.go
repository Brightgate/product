/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// appliance HTTPD front end

package main

import (
	"flag"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"base_def"
	"base_msg"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/golang/protobuf/proto"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

var (
	addr          = flag.String("promhttp-address", ":"+strconv.Itoa(base_def.HTTPD_PROMETHEUS_PORT), "The address to listen on for Prometheus HTTP requests.")
	port          = ":8000"
	publisher_mtx sync.Mutex
	publisher     *zmq.Socket
)

var latencies = prometheus.NewSummary(prometheus.SummaryOpts{
	Name: "http_render_seconds",
	Help: "HTTP page render time",
})

var (
	pings     = 0
	configs   = 0
	entities  = 0
	resources = 0
	requests  = 0
)

func event_subscribe() {
	//  First, connect our subscriber socket
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

		switch topic {
		case base_def.TOPIC_PING:
			// XXX pings were green
			ping := &base_msg.EventPing{}
			proto.Unmarshal(msg[1], ping)
			log.Println(ping)
			pings++

		case base_def.TOPIC_CONFIG:
			config := &base_msg.EventConfig{}
			proto.Unmarshal(msg[1], config)
			log.Println(config)
			configs++

		case base_def.TOPIC_ENTITY:
			// XXX entities were blue
			entity := &base_msg.EventNetEntity{}
			proto.Unmarshal(msg[1], entity)
			log.Println(entity)
			entities++

		case base_def.TOPIC_RESOURCE:
			resource := &base_msg.EventNetResource{}
			proto.Unmarshal(msg[1], resource)
			log.Println(resource)
			resources++

		case base_def.TOPIC_REQUEST:
			// XXX requests were also blue
			request := &base_msg.EventNetRequest{}
			proto.Unmarshal(msg[1], request)
			log.Println(request)
			requests++

		default:
			log.Println("unknown topic " + topic + "; ignoring message")
		}

		// def event_subscribe() -> None:
		//     client = zeroless.Client()
		//     client.connect_local(port=bdef.BROKER_ZMQ_SUB_PORT)
		//
		//     other_events.insert(0, ("httpd.internal",
		//                             timestamp_iso8601(time.time()),
		//                             "subscription starts"))
		//     print("start")
		//
		//     while True:
		//         try:
		//             for topic, msg in listen:
		//                 print("event")
		//                 logging.info("[%s] %s", topic, msg)
		//
		//                 # XXX We are using topics to separate the message types, which
		//                 # may not be sufficiently flexible.
		//                 if topic == bdef.TOPIC_PING:
		//                     eping.ParseFromString(msg)
		//                     other_events.insert(0, ("ping",
		//                                             timestamp_iso8601(eping.timestamp),
		//                                             eping.ping_message, eping.sender))
		//
		//                 elif topic == bdef.TOPIC_CONFIG:
		//                     econfig.ParseFromString(msg)
		//                     other_events.insert(0, ("config",
		//                                             timestamp_iso8601(econfig.timestamp),
		//                                             str(econfig).replace("\n", "|")))
		//
		//                 elif topic == bdef.TOPIC_ENTITY:
		//                     enetentity.ParseFromString(msg)
		//                     entity_events.insert(0, ("entity",
		//                                              timestamp_iso8601(enetentity.timestamp),
		//                                              netaddr.IPAddress(enetentity.ipv4_address),
		//                                              enetentity.sender))
		//
		//                 elif topic == bdef.TOPIC_REQUEST:
		//                     enetrequest.ParseFromString(msg)
		//                     request_events.insert(0, ("request",
		//                                               timestamp_iso8601(enetrequest.timestamp),
		//                                               enetrequest.requestor,
		//                                               enetrequest.request))
		//
		//                 elif topic == bdef.TOPIC_RESOURCE:
		//                     enetresource.ParseFromString(msg)
		//                     other_events.insert(0, ("resource",
		//                                             timestamp_iso8601(enetresource.timestamp),
		//                                             str(enetentity).replace("\n", "|")))
		//
		//                 elif topic == bdef.TOPIC_EXCEPTION:
		//                     enetexception.ParseFromString(msg)
		//                     other_events.insert(0, ("exception",
		//                                             timestamp_iso8601(enetexception.timestamp),
		//                                             str(enetexception).replace("\n", "|")))
		//
		//                 else:
		//                     other_events.insert(0, ("httpd.internal",
		//                                             timestamp_iso8601(time.time()),
		//                                             "unknown event"))
		//         except ValueError:
		//             logging.error("listen returned more than 2 items")
		//
		//

	}

	// # Event lists are lists we believe sorted descending by timestamp.
	// entity_events = []
	// request_events = []
	// other_events = []
	//
}

func index_handler(w http.ResponseWriter, r *http.Request) {
	lt := time.Now()

	fmt.Fprintf(w, "Hello, %q", html.EscapeString(r.URL.Path))
	fmt.Fprintf(w, "Pings %d; Configs: %d; Entities: %d; Resources: %d; Requests: %d",
		pings, configs, entities, resources, requests)

	latencies.Observe(time.Since(lt).Seconds())
}

func init() {
	prometheus.MustRegister(latencies)
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Printf("start on port %s", port)

	flag.Parse()

	log.Println("cli flags parsed")

	publisher, _ = zmq.NewSocket(zmq.PUB)
	publisher.Connect(base_def.APPLIANCE_ZMQ_URL + ":" + strconv.Itoa(base_def.BROKER_ZMQ_PUB_PORT))

	time.Sleep(time.Second)

	t := time.Now()

	ping := &base_msg.EventPing{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:      proto.String(fmt.Sprintf("ap.httpd(%d)", os.Getpid())),
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

	log.Println("publish ping")

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client launched")

	/* Probably another goroutine */
	go event_subscribe()

	//
	// HTTPD_INDEX_RENDER = promc.Summary("httpd_index_render_seconds",
	//                                    "HTTP index page render time")
	//
	// def timestamp_iso8601(ts):
	//     logging.info("timestamp_iso8601 %s %s", type(ts), ts)
	//
	//     if type(ts) != type(1.):
	//         ts = ts.seconds + ts.nanos / 1.e9
	//
	//     return arrow.Arrow.fromtimestamp(ts)
	//
	//
	// @app.route("/")
	// @HTTPD_INDEX_RENDER.time()
	// def index() -> str:
	//     return flask.render_template("index.html", entity_events=entity_events,
	//                                  request_events=request_events,
	//                                  other_events=other_events, now=arrow.utcnow())
	//
	//
	// XXX Statically bound to port 8000 on the public interfaces at the
	// moment.

	http.HandleFunc("/", index_handler)

	log.Fatal(http.ListenAndServe(":8000", nil))
}
