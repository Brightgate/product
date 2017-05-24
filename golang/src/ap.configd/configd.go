/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

/*
 * Appliance configuration server.
 *
 *
 * Property namespace.
 *
 * All configuration is accessible via a unified namespace, which is
 * filesystem-like.
 *
 * /customer/customer_id/site/site_id/appliance/appliance_id/ is the
 * full path to a per-appliance configuration space.  A shorthand for
 * each of these is defined:
 *
 * @@@/ is equivalent to /customer/customer_id for this appliance's
 * customer.
 * @@/ is equivalent to /customer/customer_id/site/site_id for this
 * appliance's site.
 * @/ is equivalent to
 * /customer/customer_id/site/site_id/appliance/appliance_id/ for this
 *  appliance.
 *
 * Each property within the namespace can be backed from a variety of
 * engines.
 *
 * config
 * decision
 * platform
 *
 * Each property within the namespace has a type.
 *
 * For example
 *
 * @/intent/uplink_mode
 *
 * is an enum with values ["GATEWAY", "BRIDGE"], backed by the config engine.
 *
 * @/

 *
 * XXX Handling list values.
 * XXX Handling groups.

 * kinds
 *   name
 *   group
 *   property
 *
 * @/network/wlan0/ssid
 *
 * is
 *
 * anchor(appliance)/group(network)/name(wlan0)/property(ssid)
 *
 * anchor(appliance) must lead to a mix of groups and properties.
 *
 * We had our fuller namespace, @@@/, representing the customer at
 * /customer/customer_id. In this case, we see a couple of new node types
 *
 * @@@/hosts
 *
 * anchor(customer)/summary(hosts)
 *
 * where summary() is a union of all the the hosts across the customer's sites.
 * XXX Do we understand how the cable provider appears in this schema?
 *
 * @@/host/6EB4F934-997D-4D39-88B2-674A49D05F14/
 *
 * anchor(site)/group(host)/name(6EB4F934-997D-4D39-88B2-674A49D05F14)/...
 *
 * If we envision the distributed configuration as a tree, with each
 * node potentially sourced from a different backing store (or layered
 * store), then we might have something like
 *
 * type ConfigNode struct {
 *	kind
 *	backing
 *	childkind
 * }
 */

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"base_def"
	"base_msg"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/golang/protobuf/proto"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"

	"database/sql"
	_ "github.com/mattn/go-sqlite3"
)

var addr = flag.String("listen-address", base_def.CONFIGD_PROMETHEUS_PORT,
	"The address to listen on for HTTP requests.")

func event_subscribe(subscriber *zmq.Socket) {
	//  First, connect our subscriber socket
	subscriber.Connect(base_def.BROKER_ZMQ_SUB_URL)
	subscriber.SetSubscribe("")

	for {
		log.Println("receive message bytes")

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

		case base_def.TOPIC_CONFIG:
			config := &base_msg.EventConfig{}
			proto.Unmarshal(msg[1], config)
			log.Println(config)

		case base_def.TOPIC_ENTITY:
			// XXX entities were blue
			entity := &base_msg.EventNetEntity{}
			proto.Unmarshal(msg[1], entity)
			log.Println(entity)

		case base_def.TOPIC_RESOURCE:
			resource := &base_msg.EventNetResource{}
			proto.Unmarshal(msg[1], resource)
			log.Println(resource)

		case base_def.TOPIC_REQUEST:
			// XXX requests were also blue
			request := &base_msg.EventNetRequest{}
			proto.Unmarshal(msg[1], request)
			log.Println(request)

		default:
			log.Println("unknown topic " + topic + "; ignoring message")
		}
	}
}

func update_property(db *sql.DB, property *string, value *string, lifetime int) {
	/* SET is an INSERT or an UPDATE. */
	/* INSERT INTO properties (NULL, %s, %s, %s, %s) */
	/* UPDATE properties SET value = %, modify_dt = %, lifetime = % WHERE name = %
	 */
	log.Printf("update property\n")

	rows, _ := db.Query("SELECT COUNT(*) FROM properties WHERE name = %", property)
	defer rows.Close()

	log.Printf("completed query\n")

	count := 0
	for rows.Next() {
		log.Printf("count(*), row = %d\n", count)

		var c int
		err := rows.Scan(&c)
		if err != nil {
			log.Fatal(err)
		}
		log.Println(c)
	}

	log.Println(count)
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("start")

	flag.Parse()

	log.Println("cli flags parsed")

	// XXX Ping!

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client launched")

	subscriber, _ := zmq.NewSocket(zmq.SUB)
	defer subscriber.Close()

	go event_subscribe(subscriber)

	log.Println("build minimal config database")

	db, err := sql.Open("sqlite3", "./config.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name='properties';")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	found_properties := false
	for rows.Next() {
		var name string
		err = rows.Scan(&name)
		if err != nil {
			log.Fatal(err)
		}

		if name == "properties" {
			found_properties = true
		}
	}

	/* Does the table we want exist? */
	if !found_properties {
		sqlStmt := `
		create table properties (
			id integer not null primary key,
			name text,
			value text,
			modify_dt datetime,
			lifetime integer
		);
		`
		log.Println("need to execute " + sqlStmt)
		_, err = db.Exec(sqlStmt)
		if err != nil {
			log.Printf("%q: %s\n", err, sqlStmt)
			return // XXX Correct failure mode?
		}
	}

	/*
		tx, err := db.Begin()
		if err != nil {
			log.Fatal(err)
		}
		stmt, err := tx.Prepare("insert into foo(id, name) values(?, ?)")
		if err != nil {
			log.Fatal(err)
		}
		defer stmt.Close()
		for i := 0; i < 100; i++ {
			_, err = stmt.Exec(i, fmt.Sprintf("こんにちわ世界%03d", i))
			if err != nil {
				log.Fatal(err)
			}
		}
		tx.Commit()

		_, err = db.Exec("insert into foo(id, name) values(1, 'foo'), (2, 'bar'), (3, 'baz')")
		if err != nil {
			log.Fatal(err)
		}
	*/

	log.Println("Set up listening socket.")
	// Open a request socket.

	incoming, _ := zmq.NewSocket(zmq.REP)
	incoming.Bind(base_def.CONFIGD_ZMQ_REP_URL)

	for {
		msg, err := incoming.RecvMessageBytes(0)
		if err != nil {
			break // XXX Nope.
		}

		query := &base_msg.ConfigQuery{}
		proto.Unmarshal(msg[0], query)

		// XXX Query by property or by value?
		log.Println(query)

		var rc base_msg.ConfigResponse_OpResponse
		if *query.Operation == base_msg.ConfigQuery_GET {
			rc = base_msg.ConfigResponse_OP_OK

			// XXX Really shouldn't do this; injection attack.
			qs := fmt.Sprintf("select * from properties where name = '%s'\n", query.GetProperty())
			log.Printf("requesting (%s)\n", qs)
			/* SELECT * from properties where value = '%s' */
			rows, err = db.Query("select * from properties where name = '%s'", query.GetProperty())
			if err != nil {
				log.Fatal(err)
			}
			defer rows.Close()

			nrows := 0
			for rows.Next() {
				/*   id integer not null primary key,
				name text,
				value text,
				modify_dt datetime,
				lifetime integer
				*/
				var id int
				var name string
				var value string
				var modify_dt time.Time
				var lifetime int
				err = rows.Scan(&id, &name, &value, &modify_dt, &lifetime)
				if err != nil {
					log.Fatal(err)
				}
				fmt.Println(id, name, value, modify_dt, lifetime)
				nrows++
			}
			err = rows.Err()
			if err != nil {
				log.Fatal(err)
			}

			log.Printf("%d rows\n", nrows)
		} else if *query.Operation == base_msg.ConfigQuery_SET {
			// XXX The lifetime will come from the schema for this kind of record.
			log.Printf("set op\n")
			lifetime := -1
			update_property(db, query.Property, query.Value, lifetime)
		} else {
			// XXX Must be a delete if operation was a DELETE.
			rc = base_msg.ConfigResponse_DELETE_PROP_NO_PERM
			log.Printf("not set or get\n")
		}

		t := time.Now()

		response := &base_msg.ConfigResponse{
			Timestamp: &base_msg.Timestamp{
				Seconds: proto.Int64(t.Unix()),
				Nanos:   proto.Int32(int32(t.Nanosecond())),
			},
			Sender:   proto.String("ap.configd(" + strconv.Itoa(os.Getpid()) + ")"),
			Debug:    proto.String("-"),
			Response: &rc,
			Property: proto.String("-"),
			Value:    proto.String("-"),
		}

		log.Println(response)
		data, err := proto.Marshal(response)

		incoming.SendBytes(data, 0)
	}
}
