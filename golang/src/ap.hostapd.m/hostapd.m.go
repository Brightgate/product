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
	"strings"
	"text/template"
	"time"

	"ap_common"
	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("listen-address", base_def.HOSTAPDM_PROMETHEUS_PORT,
		"The address to listen on for HTTP requests.")
	wifi_interface = flag.String("interface", "wlan0",
		"Wireless interface to use.")
	default_ssid  = flag.String("ssid", "set_ssid", "SSID to use")
	interfaces    = make(map[string]*HostapdConf)
	child_process *os.Process
	config        *ap_common.Config
)

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

func config_changed(event []byte) {
	reset := false
	config := &base_msg.EventConfig{}
	proto.Unmarshal(event, config)
	property := *config.Property
	path := strings.Split(property[2:], "/")

	// Watch for client identifications in @/client/<macaddr>/class.  If a
	// client changes class, we want it to go through the whole
	// authentication and connection process again.  Ideally we would just
	// disassociate that one client, but the big hammer will do for now
	//
	if len(path) == 3 && path[0] == "clients" && path[2] == "class" {
		log.Printf("%s changed class.  Force it to reauthenticate.\n",
			path[1])
		reset = true
	}

	// Watch for changes to the network conf
	if len(path) == 3 && path[0] == "network" {
		rewrite := false
		conf, ok := interfaces[path[1]]
		if !ok {
			log.Printf("Ignoring update for unknown NIC: %s\n", property)
			return
		}

		switch path[2] {
		case "ssid":
			conf.SSID = *config.NewValue
			rewrite = true

		case "passphrase":
			conf.Passphrase = *config.NewValue
			rewrite = true

		default:
			log.Printf("Ignoring update for unknown property: %s\n", property)
		}

		if rewrite {
			render_hostapd_template(conf)
			reset = true
		}
	}

	if reset && child_process != nil {
		// Ideally we would just send the child a SIGHUP and it would
		// reload the new configuration.  Unfortunately, hostapd seems
		// to need to go through a full reset before the changes are
		// correctly propagated to the wifi hw.
		//
		child_process.Signal(os.Interrupt)
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
func render_hostapd_template(conf *HostapdConf) string {
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

func base_config(name string) string {
	var err error

	ssid_prop := fmt.Sprintf("@/network/%s/ssid", name)
	ssid, err := config.GetProp(ssid_prop)
	if err != nil {
		log.Fatalf("Failed to get the ssid for %s: %v\n", name, err)
	}

	pass_prop := fmt.Sprintf("@/network/%s/passphrase", name)
	pass, err := config.GetProp(pass_prop)
	if err != nil {
		log.Fatalf("Failed to get the passphrase for %s: %v\n", name, err)
	}

	// Does the configuration file exist?
	conf_template, err = template.ParseFiles("golang/src/ap.hostapd.m/hostapd.conf.got")
	if err != nil {
		log.Fatal(err)
		os.Exit(2)
	}

	data := HostapdConf{
		InterfaceName: name,
		SSID:          ssid,
		HardwareModes: "g",
		Channel:       6,
		Passphrase:    pass,
	}
	interfaces[name] = &data

	fn := render_hostapd_template(&data)
	log.Printf("rendered to %s\n", fn)
	return (fn)
}

func main() {
	var err error
	var b ap_common.Broker

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("start")

	flag.Parse()

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client thread launched")

	b.Init("ap.hostapd.m")
	b.Handle(base_def.TOPIC_CONFIG, config_changed)
	b.Connect()
	defer b.Disconnect()

	log.Println("message bus thread launched")

	config = ap_common.NewConfig("ap.hostapd.m")
	fn := base_config(*wifi_interface)

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

		child_process = cmd.Process
		err = cmd.Wait()
		lifetime := time.Since(start_time)

		exit_count++
		total_lifetime = total_lifetime + lifetime

		if float64(total_lifetime)/float64(exit_count) < min_lifetime {
			log.Println("average lifetime has dropped below accepted minimum.")
			os.Exit(4)
		}

		log.Println("wait completed" + "out:" + outb.String() + "err:" + errb.String())
		// Give everything a chance to settle before we attempt to
		// restart the daemon and reconfigure the wifi hardware
		time.Sleep(time.Second)
	}
}
