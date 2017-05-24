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
 * hostapd instance monitor
 *
 * Responsibilities:
 * - to run one instance of hostapd
 * - to create a configuration file for that hostapd instance that reflects the
 *   desired configuration state of the appliance
 * - to restart or signal that hostapd instance when a relevant configuration
 *   event is received
 * - to emit availability events when the hostapd instance fails or is
 *   launched
 *
 * Questions:
 * - does a monitor offer statistics to Prometheus?
 * - can we update ourselves if the template file is updated (by a
 *   software update)?
 */

package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"text/template"
	"time"

	"base_def"
	"base_msg"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/golang/protobuf/proto"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

var addr = flag.String("listen-address", base_def.HOSTAPDM_PROMETHEUS_PORT,
	"The address to listen on for HTTP requests.")
var wifi_interface = flag.String("interface", "wlan0", "Wireless interface to use.")

const min_lifetime = 1.5e9

/*
 * Property mapping:
 *
 * ./network/[InterfaceName] can only exist if ./dev/network/ contains
 * an entry for InterfaceName.
 *
 * ./network/[InterfaceName]/type will be "802.11".
 *
 * ./network/[InterfaceName]/ssid will be a string.
 * ./network/[InterfaceName]/hardware_modes may be a bitfield.
 * ./network/[InterfaceName]/channel will be an int8 or an int16.
 * ./network/[InterfaceName]/passphrase will be a secure_string.
 *
 * XXX Options such as broadcasting, etc., are for future consideration.
 */
type HostapdConf struct {
	InterfaceName string
	SSID          string
	HardwareModes string
	Channel       int
	Passphrase    string
}

func event_listener() {
	/*
	 * Capture config events.
	 */

	//  First, connect our subscriber socket
	subscriber, _ := zmq.NewSocket(zmq.SUB)
	defer subscriber.Close()
	subscriber.Connect(base_def.BROKER_ZMQ_SUB_URL)
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
		 * XXX If this configuration event is associated with my
		 * interface or with a global mode
		 * (./global/airplane_mode, for instance, then make
		 * adjustments.
		 */
	}
}

const (
	hostapd_path    = "/usr/sbin/hostapd"
	hostapd_options = "-dKt"
)

var conf_template *template.Template

/*
 * We return the full path of the file we wrote.
 */
func render_hostapd_template(conf HostapdConf) string {
	// filename is some standard non-persistent path, with the
	// interface name.
	fn := fmt.Sprintf("%s/hostapd.conf.%s", "/tmp", conf.InterfaceName)

	cf, _ := os.Create(fn)
	defer cf.Close()

	err := conf_template.Execute(cf, conf)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	return fn
}

func main() {
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("start")

	flag.Parse()

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client thread launched")

	go event_listener()
	log.Println("message bus thread launched")

	// Does the configuration file exist?
	conf_template, err = template.ParseFiles("golang/src/ap.hostapd.m/hostapd.conf.got")
	if err != nil {
		log.Fatal(err)
		os.Exit(2)
	}

	data := HostapdConf{
		InterfaceName: *wifi_interface,
		SSID:          fmt.Sprintf("test%d", os.Getpid()),
		HardwareModes: "g",
		Channel:       6,
		Passphrase:    "sosecretive",
	}

	fn := render_hostapd_template(data)
	log.Printf("rendered to %s\n", fn)

	var exit_count uint = 0
	var total_lifetime time.Duration = 0

	// Monitor loop:
	//
	// SERD
	// - fast failure: an unprivileged start of hostapd takes 1.1s
	//   on the Raspberry Pi, so 3 failures in under 5 seconds is
	//   fast failure.
	//
	// Next iteration questions:
	// - is my daemon already running?
	// - can I adopt responsibility?
	for {
		cmd := exec.Command(hostapd_path, fn)

		var outb, errb bytes.Buffer
		cmd.Stdout = &outb
		cmd.Stderr = &errb

		start_time := time.Now()
		err = cmd.Start()
		if err != nil {
			log.Fatal("start" + err.Error())
		}
		log.Printf("hostapd started, pid %d; waiting\n", cmd.Process.Pid)

		err = cmd.Wait()
		lifetime := time.Since(start_time)

		exit_count++
		total_lifetime = total_lifetime + lifetime

		if float64(total_lifetime)/float64(exit_count) < min_lifetime {
			log.Println("average lifetime has dropped below accepted minimum.")
			os.Exit(4)
		}

		log.Println("wait completed" + "out:" + outb.String() + "err:" + errb.String())
	}
}
