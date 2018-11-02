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
	"math/bits"
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
	"bg/ap_common/wificaps"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	"go.uber.org/zap/zapcore"
)

const (
	confdir        = "/tmp"
	hostapdOptions = "-dKt"
)

var (
	// In 802.11n, a 40MHz channel is constructed from 2 20MHz channels.
	// Whether the primary channel is above or below the secondary will
	// determine one of the ht_capab settings.
	nModePrimaryAbove map[int]bool
	nModePrimaryBelow map[int]bool
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

	ModeNComment string // Enable 802.11n
	ModeNHTCapab string // Set the ht_capab field for 802.11n

	authTypes []string // authentication types enabled
	confFile  string   // Name of this NIC's hostapd.conf
	status    error    // collect hostapd failures

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
// manages.  For each socket, we can have a single in-flight command and any
// number of queued commands.
type hostapdConn struct {
	active      bool
	hostapd     *hostapdHdl
	device      *physDevice
	name        string        // device name used by this bssid
	localName   string        // our end of the control socket
	remoteName  string        // hostapd's end of the control socket
	authType    string        // authentication type used by this bssid
	wifiBand    string        // wifi mode type used by this bssid
	conn        *net.UnixConn // unix-domain control socket to hostapd
	liveCmd     *hostapdCmd   // the in-flight hostapd command
	pendingCmds []*hostapdCmd // all queued commands

	stations map[string]*stationInfo

	sync.Mutex
}

type stationInfo struct {
	lastSeen  time.Time
	signature string
}

// We have a single hostapd process, which may be managing multiple interfaces
type hostapdHdl struct {
	process   *aputil.Child   // the running hostapd child process
	devices   []*physDevice   // the physical NICs being used
	confFiles []string        // config files passed to the child
	authTypes map[string]bool // authentication types offered by this AP
	conns     []*hostapdConn  // control sockets
	done      chan error
}

// Connect to the hostapd command socket for this interface and create a unix
// domain socket for it to reply to.
func (c *hostapdConn) connect() {
	laddr := net.UnixAddr{Name: c.localName, Net: "unixgram"}
	raddr := net.UnixAddr{Name: c.remoteName, Net: "unixgram"}

	c.Lock()
	for c.active {
		// Wait for the child process to create its socket
		if aputil.FileExists(c.remoteName) {
			// If our socket still exists (either from a previous
			// instance of ap.networkd or because we failed a prior
			// Dial attempt), remove it now.
			os.Remove(c.localName)
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

func (c *hostapdConn) command(cmd string) (string, error) {
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
	return hc.res, err
}

// Use a result message from hostapd to complete the current outstanding
// command.
func (c *hostapdConn) handleResult(result string) {
	if c.liveCmd == nil {
		slog.Warnf("hostapd result with no command: '%s'", result)
	} else {
		c.liveCmd.res = result
		c.liveCmd.err <- nil
		c.liveCmd = nil
	}
}

func sendNetEntity(mac string, mode, auth, sig *string, disconnect bool) {
	hwaddr, _ := net.ParseMAC(mac)
	entity := &base_msg.EventNetEntity{
		Timestamp:     aputil.NowToProtobuf(),
		Sender:        proto.String(brokerd.Name),
		Debug:         proto.String("-"),
		Mode:          mode,
		Authtype:      auth,
		WifiSignature: sig,
		Node:          &nodeUUID,
		Disconnect:    &disconnect,
		MacAddress:    proto.Uint64(network.HWAddrToUint64(hwaddr)),
	}

	err := brokerd.Publish(entity, base_def.TOPIC_ENTITY)
	if err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_ENTITY, err)
	}
}

func (c *hostapdConn) getSignature(sta string) {
	sig, err := c.command("SIGNATURE " + sta)
	if err != nil {
		slog.Warnf("Failed to get signature for %s: %v", sta, err)
	} else if info, ok := c.stations[sta]; ok {
		if info.signature != sig {
			info.signature = sig
			sendNetEntity(sta, nil, nil, &sig, false)
		}
	}
}

func (c *hostapdConn) stationPresent(sta string, newConnection bool) {
	slog.Infof("stationPresent(%s) new: %v", sta, newConnection)
	info := c.stations[sta]
	if info == nil {
		sendNetEntity(sta, &c.wifiBand, &c.authType, nil, false)
		info = &stationInfo{}
		c.stations[sta] = info
	}
	info.lastSeen = time.Now()

	if newConnection {
		// Even though the data used to generate the signature comes
		// from probe and association frames, hostapd will return an
		// empty signature if you ask too quickly.  So, we wait a
		// second.
		time.AfterFunc(time.Second, func() { c.getSignature(sta) })
	} else {
		go c.getSignature(sta)
	}

}

func (c *hostapdConn) stationGone(sta string) {
	slog.Infof("stationGone(%s)", sta)
	delete(c.stations, sta)
	sendNetEntity(sta, &c.wifiBand, nil, nil, true)
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
		msg := m[1]
		mac := m[2]
		switch msg {
		case "AP-STA-CONNECTED":
			c.stationPresent(mac, true)
		case "AP-STA-POLL-OK":
			c.stationPresent(mac, false)
		case "AP-STA-DISCONNECTED":
			c.stationGone(mac)
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

	stopCheckins := make(chan bool, 1)
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
				slog.Warnf("%s Read error: %v",
					c.device.name, err)
				break
			}
		}

		if c.liveCmd != nil {
			now := time.Now()
			delta := (now.Sub(c.liveCmd.sent)).Seconds()

			if delta > float64(*hostapdLatency) {
				slog.Warnf("hostapd blocked for %1.2f seconds",
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

// Update the final bits of a mac address
func macUpdateLastOctet(mac string, maskSize, val uint64) string {
	octets := strings.Split(mac, ":")
	if len(octets) == 6 {
		b, _ := strconv.ParseUint(octets[5], 16, 32)
		mask := ^((uint64(1) << maskSize) - 1)
		new := (b & mask) | val
		if new != b {
			octets[5] = fmt.Sprintf("%02x", new)

			// Since we changed the mac address, we need to set the
			// 'locally administered' bit in the first octet
			b, _ = strconv.ParseUint(octets[0], 16, 32)
			b |= 0x02 // Set the "locally administered" bit
			octets[0] = fmt.Sprintf("%02x", b)
			mac = strings.Join(octets, ":")
		}
	} else {
		slog.Warnf("invalid mac address: %s", mac)
	}

	return mac
}

// hostapd is going to spawn a virtual NIC for our second BSSID.  Add a node for
// that NIC to our list of devices.
func initPseudoNic(d *physDevice) *physDevice {
	pseudo := &physDevice{
		name:   d.name + "_1",
		ring:   base_def.RING_GUEST,
		wifi:   d.wifi,
		pseudo: true,
	}

	return pseudo
}

//
// Get network settings from configd and use them to initialize the AP
//
func getAPConfig(d *physDevice) *apConfig {
	var hwMode, radiusServer string
	var modeNComment, modeNHTCapab string

	w := d.wifi

	authMap := make(map[string]bool)
	pskComment := "#"
	eapComment := "#"
	for _, r := range rings {
		if r.Auth == "wpa-psk" {
			authMap["wpa-psk"] = true
			pskComment = ""
		} else if r.Auth == "wpa-eap" && radiusSecret != "" {
			authMap["wpa-eap"] = true
			eapComment = ""
		}
	}
	authList := make([]string, 0)
	for a := range authMap {
		authList = append(authList, a)
	}

	if satellite {
		radiusServer = getGatewayIP()
	} else {
		radiusServer = "127.0.0.1"
	}

	ssidCnt := len(authList)
	if ssidCnt > w.cap.Interfaces {
		slog.Warnf("%s can't support %d SSIDs", d.hwaddr, ssidCnt)
		return nil
	}

	d.ring = base_def.RING_STANDARD
	if ssidCnt > 1 {
		// If we create multiple SSIDs, hostapd will generate additional
		// bssids by incrementing the final octet of the nic's mac
		// address.  hostapd requires that the base and generated mac
		// addresses share the upper 47 bits, so we need to ensure that
		// the base address has the lowest bits set to 0.
		maskBits := uint64(bits.Len(uint(ssidCnt - 1)))
		oldMac := d.hwaddr
		d.hwaddr = macUpdateLastOctet(d.hwaddr, maskBits, 0)
		if d.hwaddr != oldMac {
			slog.Debugf("Changed mac from %s to %s", oldMac, d.hwaddr)
		}

		p := initPseudoNic(d)
		p.hwaddr = macUpdateLastOctet(d.hwaddr, maskBits, 1)
		physDevices[getNicID(p)] = p
	}

	pskssid := wifiSSID
	eapssid := wifiSSID + "-eap"
	if w.activeBand == wificaps.LoBand {
		hwMode = "g"
	} else if w.activeBand == wificaps.HiBand {
		hwMode = "a"
		pskssid += "-5GHz"
		eapssid += "-5GHz"
	} else {
		slog.Warnf("unsupported wifi band: %s", d.wifi.activeBand)
		return nil
	}

	if w.cap.WifiModes["n"] {
		// XXX: config option for short GI?
		if w.cap.FreqWidths[40] {
			// With a 40MHz channel, we can support a secondary
			// 20MHz channel either above or below the primary,
			// depending on what the primary channel is.
			if nModePrimaryAbove[w.activeChannel] {
				modeNHTCapab += "[HT40+]"
			}
			if nModePrimaryBelow[w.activeChannel] {
				modeNHTCapab += "[HT40-]"
			}
		}
	} else {
		modeNComment = "#"
	}

	data := apConfig{
		Interface:  d.name,
		HWaddr:     d.hwaddr,
		PSKSSID:    pskssid,
		EAPSSID:    eapssid,
		Mode:       hwMode,
		Channel:    d.wifi.activeChannel,
		Passphrase: wifiPassphrase,
		PskComment: pskComment,
		EapComment: eapComment,
		ConfDir:    confdir,

		ModeNComment: modeNComment,
		ModeNHTCapab: modeNHTCapab,

		authTypes:            authList,
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
		slog.Fatalf("Unable to create %s: %v", mfn, err)
	}
	defer mf.Close()

	// Create the 'vlan' file, which tells hostapd which vlans to create
	vfn := confdir + "/" + "hostapd." + auth + "." + mode + ".vlan"
	vf, err := os.Create(vfn)
	if err != nil {
		slog.Fatalf("Unable to create %s: %v", vfn, err)
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
	authTypes := make(map[string]bool)
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

		for _, t := range conf.authTypes {
			generateVlanConf(conf, t)
			authTypes[t] = true
		}

		files = append(files, confName)
		devices = append(devices, d)
	}

	updateNicProperties()

	h.devices = devices
	h.authTypes = authTypes
	h.confFiles = files
}

func (h *hostapdHdl) cleanup() {
	for _, c := range h.conns {
		os.Remove(c.localName)
	}
}

func (h *hostapdHdl) newConn(d *physDevice, auth, suffix string) *hostapdConn {
	// There are two endpoints for each control socket.  The remoteName is
	// owned by hostapd, and we need to use the name that it expects.  The
	// localName is owned by us, and the format is chosen by us.
	fullName := d.name + suffix
	remoteName := "/var/run/hostapd/" + fullName
	localName := "/tmp/hostapd_ctrl_" + fullName + "-" +
		strconv.Itoa(os.Getpid())

	newConn := hostapdConn{
		hostapd:     h,
		name:        fullName,
		remoteName:  remoteName,
		localName:   localName,
		authType:    auth,
		wifiBand:    d.wifi.activeBand,
		active:      true,
		device:      d,
		pendingCmds: make([]*hostapdCmd, 0),
		stations:    make(map[string]*stationInfo),
	}
	os.Remove(newConn.name)
	return &newConn
}

func (h *hostapdHdl) start() {
	suffix := map[string]string{
		"wpa-psk": "",   // The PSK iface is wlanX
		"wpa-eap": "_1", // The EAP iface is wlanX_1
	}

	h.generateHostAPDConf()
	if len(h.devices) == 0 {
		h.done <- fmt.Errorf("no suitable wireless devices available")
		return
	}
	defer h.cleanup()

	// There is a control interface for each BSSID, which means one for each
	// authentication type for each devices.
	for _, d := range h.devices {
		for a := range h.authTypes {
			h.conns = append(h.conns, h.newConn(d, a, suffix[a]))
		}
	}

	stopNetworkRebuild := make(chan bool, 1)
	go rebuildUnenrolled(h.devices, stopNetworkRebuild)

	h.process = aputil.NewChild(plat.HostapdCmd, h.confFiles...)
	h.process.UseZapLog("hostapd: ", slog, zapcore.InfoLevel)

	slog.Infof("Starting hostapd")

	startTime := time.Now()
	if err := h.process.Start(); err != nil {
		stopNetworkRebuild <- true
		h.done <- fmt.Errorf("failed to launch: %v", err)
		return
	}

	var wg sync.WaitGroup
	for _, c := range h.conns {
		wg.Add(1)
		go c.run(&wg)
	}

	h.process.Wait()

	slog.Infof("hostapd exited after %s", time.Since(startTime))
	stopNetworkRebuild <- true

	deadman := time.AfterFunc(*deadmanTimeout, func() {
		slog.Warnf("failed to clean up hostapd monitoring")
		syscall.Kill(syscall.Getpid(), syscall.SIGABRT)
	})

	for _, c := range h.conns {
		go c.stop()
	}

	wg.Wait()
	deadman.Stop()
	h.done <- nil
}

func (h *hostapdHdl) reload() {
	if h != nil {
		slog.Infof("Reloading hostapd")
		h.generateHostAPDConf()
		h.process.Signal(plat.ReloadSignal)
	}
}

func (h *hostapdHdl) reset() {
	if h != nil {
		slog.Infof("Killing hostapd")
		h.process.Signal(plat.ResetSignal)
	}
}

func (h *hostapdHdl) wait() error {
	err := <-h.done
	return err
}

func initChannelLists() {
	// The 2.4GHz band is crowded, so the use of 40MHz bonded channels is
	// discouraged.  Thus, the following lists only include channels in the
	// 5GHz band.
	above := []int{36, 44, 52, 60, 100, 108, 116, 124, 132, 140, 149, 157}
	below := []int{40, 48, 56, 64, 104, 112, 120, 128, 136, 144, 153, 161}

	nModePrimaryAbove = make(map[int]bool)
	for _, c := range above {
		nModePrimaryAbove[c] = true
	}

	nModePrimaryBelow = make(map[int]bool)
	for _, c := range below {
		nModePrimaryBelow[c] = true
	}
}

func startHostapd(devs []*physDevice) *hostapdHdl {
	h := &hostapdHdl{
		devices: devs,
		conns:   make([]*hostapdConn, 0),
		done:    make(chan error, 1),
	}

	initChannelLists()

	go h.start()
	return h
}
