/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
)

var (
	hostapdLog *log.Logger
)

const (
	confdir        = "/tmp"
	hostapdPath    = "/usr/sbin/hostapd"
	hostapdOptions = "-dKt"
)

type apConfig struct {
	// Fields used to populate the configuration template
	Interface  string // Linux device name
	HWaddr     string // Mac address to use
	PSKSSID    string
	EAPSSID    string
	Passphrase string
	Mode       string
	Channel    int
	PskComment string // Used to disable wpa-psk in .conf template
	EapComment string // Used to disable wpa-eap in .conf template
	ConfDir    string // Location of hostapd.conf, etc.

	confFile string // Name of this NIC's hostapd.conf
	status   error  // collect hostapd failures

	RadiusAuthServer     string
	RadiusAuthServerPort string
	RadiusAuthSecret     string // RADIUS shared secret
}

type hostapdCmd struct {
	cmd    string
	res    string
	err    chan error
	queued time.Time
	sent   time.Time
}

// hostapd has a separate control socket for each of the network interfaces it
// manages.  For each socket, we can have a single in-process command and any
// number of queued commands.
type hostapdConn struct {
	hostapd     *hostapdHdl
	active      bool
	device      *physDevice
	conn        *net.UnixConn
	liveCmd     *hostapdCmd
	pendingCmds []*hostapdCmd
	stations    map[string]time.Time

	sync.Mutex
}

// We have a single hostapd process, which may be managing multiple interfaces
type hostapdHdl struct {
	process   *aputil.Child
	devices   []*physDevice
	confFiles []string
	conns     []*hostapdConn
	done      chan error
}

// Connect to the hostapd command socket for this interface and create a unix
// domain socket for it to reply to.
func (c *hostapdConn) connect() {
	pid := os.Getpid()

	remoteName := "/var/run/hostapd/" + c.device.name
	localName := "/tmp/hostapd_ctrl_" + c.device.name + "-" + strconv.Itoa(pid)

	laddr := net.UnixAddr{Name: localName, Net: "unixgram"}
	raddr := net.UnixAddr{Name: remoteName, Net: "unixgram"}

	c.Lock()
	for c.active {
		// Wait for the child process to create its socket
		if aputil.FileExists(remoteName) {
			// If our socket still exists (either from a previous
			// instance of ap.networkd or because we failed a prior
			// Dial attempt), remove it now.
			os.Remove(localName)
			c.conn, _ = net.DialUnix("unixgram", &laddr, &raddr)
			if c.conn != nil {
				break
			}
		}
		c.Unlock()
		time.Sleep(100 * time.Millisecond)
		c.Lock()
	}
	c.Unlock()
}

// This hostapd connection is going away, so flush all of the commands out of
// the queue
func (c *hostapdConn) clearCmds() {
	err := fmt.Errorf("hostapd connection closed")
	if c.liveCmd != nil {
		c.liveCmd.err <- err
		c.liveCmd = nil
	}

	for len(c.pendingCmds) > 0 {
		c.pendingCmds[0].err <- err
		c.pendingCmds = c.pendingCmds[1:]
	}
}

// If we don't have a command in-flight, pull the next one from the pending
// queue and send it to the daemon.
func (c *hostapdConn) pushCmd() {
	if c.liveCmd != nil || len(c.pendingCmds) == 0 || c.conn == nil {
		return
	}

	l := c.pendingCmds[0]
	c.pendingCmds = c.pendingCmds[1:]

	l.sent = time.Now()
	c.conn.SetWriteDeadline(l.sent.Add(time.Second))
	if _, err := c.conn.Write([]byte(l.cmd)); err != nil {
		l.err <- fmt.Errorf("failed to send '%s' to %s: %v",
			l.cmd, c.device.name, err)
		c.pushCmd()
	} else {
		c.liveCmd = l
	}
}

func (c *hostapdConn) command(cmd string) error {
	hc := hostapdCmd{
		queued: time.Now(),
		cmd:    cmd,
		err:    make(chan error, 1),
	}

	c.Lock()
	c.pendingCmds = append(c.pendingCmds, &hc)
	c.pushCmd()
	c.Unlock()

	err := <-hc.err
	return err
}

// Use a result message from hostapd to complete the current outstanding
// command.
func (c *hostapdConn) handleResult(result string) {
	if c.liveCmd == nil {
		log.Printf("hostapd result with no command: '%s'", result)
	} else {
		c.liveCmd.res = result
		c.liveCmd.err <- nil
		c.liveCmd = nil
	}
}

func sendNetEntity(mac, mode string, disconnect bool) {
	hwaddr, _ := net.ParseMAC(mac)
	entity := &base_msg.EventNetEntity{
		Timestamp:  aputil.NowToProtobuf(),
		Sender:     proto.String(brokerd.Name),
		Debug:      proto.String("-"),
		Mode:       &mode,
		Node:       &nodeUUID,
		Disconnect: &disconnect,
		MacAddress: proto.Uint64(network.HWAddrToUint64(hwaddr)),
	}

	err := brokerd.Publish(entity, base_def.TOPIC_ENTITY)
	if err != nil {
		log.Printf("couldn't publish %s: %v\n", base_def.TOPIC_ENTITY, err)
	}
}

func (c *hostapdConn) stationPresent(sta string) {
	if _, ok := c.stations[sta]; !ok {
		sendNetEntity(sta, c.device.activeMode, false)
	}
	c.stations[sta] = time.Now()
}

func (c *hostapdConn) stationGone(sta string) {
	delete(c.stations, sta)
	sendNetEntity(sta, c.device.activeMode, true)
}

// Handle an async status message from hostapd
func (c *hostapdConn) handleStatus(status string) {
	const (
		// We're looking for one of the following messages:
		//    AP-STA-CONNECTED b8:27:eb:9f:d8:e0     (client arrived)
		//    AP-STA-DISCONNECTED b8:27:eb:9f:d8:e0  (client left)
		//    AP-STA-POLL-OK b8:27:eb:9f:d8:e0       (client still here)
		msgs     = "(AP-STA-CONNECTED|AP-STA-DISCONNECTED|AP-STA-POLL-OK)"
		macOctet = "[[:xdigit:]][[:xdigit:]]"
		macAddr  = "(" + macOctet + ":" + macOctet + ":" +
			macOctet + ":" + macOctet + ":" + macOctet + ":" +
			macOctet + ")"
	)
	re := regexp.MustCompile(msgs + " " + macAddr)

	m := re.FindStringSubmatch(status)
	if len(m) == 3 {
		switch m[1] {
		case "AP-STA-CONNECTED":
			c.stationPresent(m[2])
		case "AP-STA-POLL-OK":
			c.stationPresent(m[2])
		case "AP-STA-DISCONNECTED":
			c.stationGone(m[2])
		}
	}
}

// close the socket, which will interrupt any pending read/write.
func (c *hostapdConn) stop() {
	c.Lock()
	c.active = false
	if c.conn != nil {
		c.conn.Close()
	}
	c.Unlock()
}

// Send periodic PINGs to hostapd to make sure it is still alive and responding
func (c *hostapdConn) checkIn(exit chan bool) {
	t := time.NewTicker(time.Second * 5)
	defer t.Stop()

	for {
		select {
		case <-exit:
			return
		case <-t.C:
			c.command("PING")
		}
	}
}

func (c *hostapdConn) run(wg *sync.WaitGroup) {

	go c.command("ATTACH")
	c.connect()

	stopCheckins := make(chan bool)
	go c.checkIn(stopCheckins)

	buf := make([]byte, 4096)
	c.Lock()
	for c.active {
		c.pushCmd()

		c.Unlock()
		c.conn.SetReadDeadline(time.Now().Add(time.Second))
		n, err := c.conn.Read(buf[:])
		c.Lock()

		if err != nil {
			// We expect this read to timeout regularly, so we
			// ignore those errors.
			netErr, ok := err.(net.Error)
			if !ok || !netErr.Timeout() {
				log.Printf("%s Read error: %v\n",
					c.device.name, err)
				break
			}
		}

		if c.liveCmd != nil {
			now := time.Now()
			delta := (now.Sub(c.liveCmd.sent)).Seconds()

			if delta > float64(*hostapdLatency) {
				log.Printf("hostapd blocked for %1.2f seconds",
					delta)
				c.hostapd.reset()
				break
			}
		}

		if n > 0 {
			// hostapd prefaces unsolicited status messages with <#>
			if buf[0] == '<' {
				c.handleStatus(string(buf[3:n]))
			} else {
				c.handleResult(string(buf[:n]))
			}
		}
	}
	stopCheckins <- true
	c.clearCmds()
	c.Unlock()

	wg.Done()
}

//
// Replace the final nybble of a mac address to match the transformations
// hostapd performs to support multple SSIDs
//
func macUpdateLastOctet(mac string, nybble uint64) string {
	octets := strings.Split(mac, ":")
	if len(octets) == 6 {
		b, _ := strconv.ParseUint(octets[5], 16, 32)
		newNybble := (b & 0xf0) | nybble
		if newNybble != b {
			octets[5] = fmt.Sprintf("%02x", newNybble)

			// Since we changed the mac address, we need to set the
			// 'locally administered' bit in the first octet
			b, _ = strconv.ParseUint(octets[0], 16, 32)
			b |= 0x02 // Set the "locally administered" bit
			octets[0] = fmt.Sprintf("%02x", b)
			mac = strings.Join(octets, ":")
		}
	} else {
		log.Printf("invalid mac address: %s", mac)
	}
	return mac
}

//
// Get network settings from configd and use them to initialize the AP
//
func getAPConfig(d *physDevice) *apConfig {
	var radiusServer string

	ssidCnt := 0
	pskComment := "#"
	eapComment := "#"
	for _, r := range rings {
		if r.Auth == "wpa-psk" {
			pskComment = ""
			ssidCnt++
		} else if r.Auth == "wpa-eap" {
			eapComment = ""
			ssidCnt++
		}
	}

	if satellite {
		internal := rings[base_def.RING_INTERNAL]
		gateway := network.SubnetRouter(internal.Subnet)
		radiusServer = gateway
	} else {
		radiusServer = "127.0.0.1"
	}

	if ssidCnt > d.interfaces {
		log.Printf("%s can't support %d SSIDs\n", d.hwaddr, ssidCnt)
		return nil
	}
	if ssidCnt > 1 {
		// If we create multiple SSIDs, hostapd will generate
		// additional bssids by incrementing the final octet of the
		// nic's mac address.  To accommodate that, hostapd wants the
		// final nybble of the final octet to be 0.
		newMac := macUpdateLastOctet(d.hwaddr, 0)
		if newMac != d.hwaddr {
			log.Printf("Changed mac from %s to %s\n", d.hwaddr, newMac)
			d.hwaddr = newMac
		}
	}

	pskssid := wifiSSID
	eapssid := wifiSSID + "-eap"
	mode := d.activeMode
	if mode == "ac" {
		// 802.11ac is configured using "hw_mode=a"
		mode = "a"
		pskssid += "-5GHz"
		eapssid += "-5GHz"
	}
	data := apConfig{
		Interface:  d.name,
		HWaddr:     d.hwaddr,
		PSKSSID:    pskssid,
		EAPSSID:    eapssid,
		Mode:       mode,
		Channel:    d.activeChannel,
		Passphrase: wifiPassphrase,
		PskComment: pskComment,
		EapComment: eapComment,
		ConfDir:    confdir,

		RadiusAuthServer:     radiusServer,
		RadiusAuthServerPort: "1812",
		RadiusAuthSecret:     radiusSecret,
	}

	return &data
}

//
// Generate the configuration files needed for hostapd.
//
func generateVlanConf(conf *apConfig, auth string) {

	mode := conf.Mode

	// Create the 'accept_macs' file, which tells hostapd how to map clients
	// to VLANs.
	mfn := confdir + "/" + "hostapd." + auth + "." + mode + ".macs"
	mf, err := os.Create(mfn)
	if err != nil {
		log.Fatalf("Unable to create %s: %v\n", mfn, err)
	}
	defer mf.Close()

	// Create the 'vlan' file, which tells hostapd which vlans to create
	vfn := confdir + "/" + "hostapd." + auth + "." + mode + ".vlan"
	vf, err := os.Create(vfn)
	if err != nil {
		log.Fatalf("Unable to create %s: %v\n", vfn, err)
	}
	defer vf.Close()

	for ring, config := range rings {
		if config.Auth != auth || config.Vlan <= 0 {
			continue
		}

		fmt.Fprintf(vf, "%d\tvlan.%s.%d\n", config.Vlan, mode, config.Vlan)

		// One client per line, containing "<mac addr> <vlan_id>"
		for client, info := range clients {
			if info.Ring == ring {
				fmt.Fprintf(mf, "%s %d\n", client, config.Vlan)
			}
		}
	}
}

func (h *hostapdHdl) generateHostAPDConf() {
	tfile := *templateDir + "/hostapd.conf.got"

	files := make([]string, 0)
	devices := make([]*physDevice, 0)

	for _, d := range h.devices {
		// Create hostapd.conf, using the apConfig contents to fill
		// out the .got template
		t, err := template.ParseFiles(tfile)
		if err != nil {
			continue
		}

		confName := confdir + "/" + "hostapd.conf." + d.name
		cf, _ := os.Create(confName)
		defer cf.Close()

		conf := getAPConfig(d)
		err = t.Execute(cf, conf)
		if err != nil {
			continue
		}
		generateVlanConf(conf, "wpa-psk")
		generateVlanConf(conf, "wpa-eap")

		files = append(files, confName)
		devices = append(devices, d)
	}

	h.devices = devices
	h.confFiles = files
}

func (h *hostapdHdl) start() {
	var wg sync.WaitGroup

	deleteBridges()

	h.generateHostAPDConf()
	if len(h.devices) == 0 {
		h.done <- fmt.Errorf("no suitable wireless devices available")
		return
	}

	for _, d := range h.devices {
		os.Remove("/var/run/hostapd/" + d.name)
		newConn := hostapdConn{
			hostapd:     h,
			active:      true,
			device:      d,
			pendingCmds: make([]*hostapdCmd, 0),
			stations:    make(map[string]time.Time),
		}
		h.conns = append(h.conns, &newConn)
	}
	createBridges()
	resetInterfaces()

	h.process = aputil.NewChild(hostapdPath, h.confFiles...)
	h.process.LogOutputTo("hostapd: ", log.Ldate|log.Ltime, os.Stderr)

	log.Printf("Starting hostapd\n")

	startTime := time.Now()
	if err := h.process.Start(); err != nil {
		h.done <- fmt.Errorf("failed to launch: %v", err)
		return
	}

	for _, c := range h.conns {
		wg.Add(1)
		go c.run(&wg)
	}

	h.process.Wait()

	log.Printf("hostapd exited after %s\n", time.Since(startTime))

	for _, c := range h.conns {
		go c.stop()
	}

	wg.Wait()
	h.done <- nil
}

func (h *hostapdHdl) reload() {
	h.generateHostAPDConf()
	log.Printf("Reloading hostapd\n")
	h.process.Signal(syscall.SIGINT)

}

func (h *hostapdHdl) reset() {
	if h != nil {
		log.Printf("Killing hostapd\n")
		h.process.Signal(syscall.SIGINT)
	}
}

func (h *hostapdHdl) wait() error {
	err := <-h.done
	return err
}

func startHostapd(devs []*physDevice) *hostapdHdl {
	h := &hostapdHdl{
		devices: devs,
		conns:   make([]*hostapdConn, 0),
		done:    make(chan error, 1),
	}

	go h.start()
	return h
}
