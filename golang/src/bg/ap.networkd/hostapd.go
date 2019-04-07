/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/wificaps"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"
	"bg/common/network"

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

var virtualAPs map[string]*cfgapi.VirtualAP

// Used to invoke hostapd, instantiating a virtual AP on a specific device
type vapConfig struct {
	idx      int               // per-device VAP index
	Name     string            // @/network/vap/<name>/*
	vap      *cfgapi.VirtualAP // device-independent config for this AP
	physical *physDevice       // physical device hosting this virtual AP
	logical  *physDevice       // logical device hosting this virtual AP

	BSSID      string
	SSID       string
	Passphrase string
	KeyMgmt    string
	PskComment string // Used to disable wpa-psk in .conf template
	EapComment string // Used to disable wpa-eap in .conf template
	ConfPrefix string // Location of vlan and mac config files

	confFile string // Name of this NIC's hostapd.conf
	status   error  // collect hostapd failures

	RadiusAuthServer     string
	RadiusAuthServerPort string
	RadiusAuthSecret     string // RADIUS shared secret
}

type devConfig struct {
	Interface    string // Linux device name
	Mode         string
	Channel      int
	ModeNComment string // Enable 802.11n
	ModeNHTCapab string // Set the ht_capab field for 802.11n
}

type hostapdCmd struct {
	cmd    string
	res    string
	err    chan error
	queued time.Time
	sent   time.Time
}

type retransmitState struct {
	count int
	first time.Time
	last  time.Time
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
	vapName     string        // virtual AP
	wifiBand    string        // wifi mode type used by this bssid
	conn        *net.UnixConn // unix-domain control socket to hostapd
	liveCmd     *hostapdCmd   // the in-flight hostapd command
	pendingCmds []*hostapdCmd // all queued commands

	retransmits map[string]*retransmitState
	stations    map[string]*stationInfo

	sync.Mutex
}

type stationInfo struct {
	lastSeen  time.Time
	signature string
}

// We have a single hostapd process, which may be managing multiple interfaces
type hostapdHdl struct {
	process    *aputil.Child  // the running hostapd child process
	devices    []*physDevice  // the physical NICs being used
	unenrolled []*physDevice  // the logical NICs used for unenrolled clients
	vaps       []*vapConfig   // the virtual APs being hosted
	confFiles  []string       // config files passed to the child
	conns      []*hostapdConn // control sockets
	done       chan error
}

func (c *hostapdConn) String() string {
	return fmt.Sprintf("%s:%s", c.name, c.vapName)
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

func sendNetEntity(mac string, vapName, bandName, sig *string, disconnect bool) {
	band := "?"
	if bandName != nil {
		band = *bandName
	}
	action := "connect"
	if disconnect {
		action = "disconnect"
	}
	vap := "?"
	if vapName != nil {
		vap = *vapName
	}

	slog.Debugf("NetEntity(%s, vap: %s, band: %s, %s)", mac, vap, band, action)
	hwaddr, _ := net.ParseMAC(mac)
	entity := &base_msg.EventNetEntity{
		Timestamp:     aputil.NowToProtobuf(),
		Sender:        proto.String(brokerd.Name),
		Debug:         proto.String("-"),
		VirtualAP:     vapName,
		WifiSignature: sig,
		Node:          &nodeUUID,
		Disconnect:    &disconnect,
		MacAddress:    proto.Uint64(network.HWAddrToUint64(hwaddr)),
		Band:          bandName,
	}

	err := brokerd.Publish(entity, base_def.TOPIC_ENTITY)
	if err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_ENTITY, err)
	}
}

func sendNetException(mac string, vapName *string,
	reason *base_msg.EventNetException_Reason) {

	vap := "?"
	if vapName != nil {
		vap = *vapName
	}

	slog.Debugf("NetException(%s, vap: %s, reason: %d)", mac, vap, *reason)
	hwaddr, _ := net.ParseMAC(mac)
	entity := &base_msg.EventNetException{
		Timestamp:  aputil.NowToProtobuf(),
		Sender:     proto.String(brokerd.Name),
		Debug:      proto.String("-"),
		VirtualAP:  vapName,
		Reason:     reason,
		MacAddress: proto.Uint64(network.HWAddrToUint64(hwaddr)),
	}

	err := brokerd.Publish(entity, base_def.TOPIC_EXCEPTION)
	if err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_EXCEPTION, err)
	}
}

func (c *hostapdConn) getSignature(sta string) {
	sig, err := c.command("SIGNATURE " + sta)
	if err != nil {
		slog.Warnf("Failed to get signature for %s: %v", sta, err)
	} else if info, ok := c.stations[sta]; ok {
		if info.signature != sig {
			info.signature = sig
			sendNetEntity(sta, &c.vapName, nil, &sig, false)
		}
	}
}

func (c *hostapdConn) stationPresent(sta string, newConnection bool) {
	slog.Infof("%v stationPresent(%s) new: %v", c, sta, newConnection)
	info := c.stations[sta]
	if info == nil {
		sendNetEntity(sta, &c.vapName, &c.wifiBand, nil, false)
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
	slog.Infof("%v stationGone(%s)", c, sta)
	delete(c.stations, sta)
	sendNetEntity(sta, &c.vapName, &c.wifiBand, nil, true)
}

func (c *hostapdConn) stationBadPassword(sta string) {
	reason := base_msg.EventNetException_BAD_PASSWORD

	slog.Infof("%v stationBadPassword(%s)", c, sta)
	sendNetException(sta, &c.vapName, &reason)
}

// There is currently a bug on the OpenWRT boards where a client will fail to
// authenticate with EAP despite having valid credentials.  We can see this
// happening in the log as hostapd repeatedly issues RETRANSMIT messages.  The
// retries appear to happen with backoffs of 3, 6, 12, 20, 20, and 20 seconds
// before the operation finally times out.
//
// Until the problem gets fixed, we can try to work around it by restarting
// hostapd when we see it happening.  this_is_fine.gif.
func (c *hostapdConn) eapRetransmit(mac string) {
	now := time.Now()
	expired := now.Add(-1 * *retransmitTimeout)

	for mac, state := range c.retransmits {
		if state.last.Before(expired) {
			slog.Debugf("%s is clear.  %d since %s\n",
				mac, state.count, state.first.Format(time.RFC3339))
			delete(c.retransmits, mac)
		}
	}

	state := c.retransmits[mac]
	if state == nil {
		state = &retransmitState{
			count: 0,
			first: now,
		}
		c.retransmits[mac] = state
	}
	state.last = now

	if state.count++; state.count >= *retransmitLimit {
		slog.Warnf("hostapd had %d retransmits for %s since %s - restarting",
			state.count, mac, state.first.Format(time.RFC3339))
		c.retransmits = make(map[string]*retransmitState)
		c.hostapd.reset()
	}
}

// Handle an async status message from hostapd
func (c *hostapdConn) handleStatus(status string) {
	const (
		// We're looking for one of the following messages:
		//    AP-STA-CONNECTED b8:27:eb:9f:d8:e0     (client arrived)
		//    AP-STA-DISCONNECTED b8:27:eb:9f:d8:e0  (client left)
		//    AP-STA-POLL-OK b8:27:eb:9f:d8:e0       (client still here)
		//    AP-STA-POSSIBLE-PSK-MISMATCH b8:27:eb:9f:d8:e0  (bad password)
		//    CTRL-EVENT-EAP-RETRANSMIT b8:27:eb:9f:d8:e0 (possibly T268)
		msgs = "(AP-STA-CONNECTED|AP-STA-DISCONNECTED|" +
			"AP-STA-POLL-OK|AP-STA-POSSIBLE-PSK-MISMATCH|" +
			"CTRL-EVENT-EAP-RETRANSMIT|CTRL-EVENT-EAP-RETRANSMIT2)"
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
		case "AP-STA-POSSIBLE-PSK-MISMATCH":
			c.stationBadPassword(mac)
		case "CTRL-EVENT-EAP-RETRANSMIT", "CTRL-EVENT-EAP-RETRANSMIT2":
			c.eapRetransmit(mac)
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

// Set the 'locally administered' bit in the first octet of the mac address
func macSetLocal(mac string) string {
	octets := strings.Split(mac, ":")
	b, _ := strconv.ParseUint(octets[0], 16, 32)
	b |= 0x02
	octets[0] = fmt.Sprintf("%02x", b)
	mac = strings.Join(octets, ":")

	return mac
}

// Update the final bits of a mac address
func macUpdateLastOctet(mac string, val uint64) string {
	maskSize := uint64(bits.Len(uint(maxSSIDs - 1)))
	octets := strings.Split(mac, ":")
	if len(octets) == 6 {
		b, _ := strconv.ParseUint(octets[5], 16, 32)
		mask := ^((uint64(1) << maskSize) - 1)
		new := (b & mask) | val
		if new != b {
			octets[5] = fmt.Sprintf("%02x", new)
			mac = macSetLocal(strings.Join(octets, ":"))
		}
	} else {
		slog.Warnf("invalid mac address: %s", mac)
	}

	return mac
}

// hostapd is going to spawn a virtual NIC for our second BSSID.  Add a node for
// that NIC to our list of devices.
func initPseudoNic(d *physDevice, idx int) *physDevice {
	pseudo := &physDevice{
		name:   fmt.Sprintf("%s_%d", d.name, idx),
		hwaddr: macUpdateLastOctet(d.hwaddr, uint64(idx)),
		wifi:   d.wifi,
		pseudo: true,
	}

	return pseudo
}

func getDevConfig(d *physDevice) *devConfig {
	var hwMode, modeNComment, modeNHTCapab string

	w := d.wifi
	if w.activeBand == wificaps.LoBand {
		hwMode = "g"
	} else if w.activeBand == wificaps.HiBand {
		hwMode = "a"
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

	data := devConfig{
		Interface:    d.name,
		Mode:         hwMode,
		Channel:      d.wifi.activeChannel,
		ModeNComment: modeNComment,
		ModeNHTCapab: modeNHTCapab,
	}

	return &data
}

//
// Get network settings from configd and use them to initialize the AP
//
func getVAPConfig(name string, d *physDevice, idx int) *vapConfig {
	var bssid, eapComment, pskComment, passphrase, radiusServer string
	var logical *physDevice

	vap := virtualAPs[name]
	if len(vap.Rings) == 0 {
		slog.Infof("VAP %s: no assigned rings", name)
		return nil
	}

	ssid := vap.SSID
	if vap.Tag5GHz && d.wifi.activeBand == wificaps.HiBand {
		ssid += "-5ghz"
	}

	switch vap.KeyMgmt {
	case "wpa-psk":
		eapComment = "#"
		passphrase = vap.Passphrase
		if passphrase == "" {
			slog.Errorf("VAP %s: missing WPA-PSK passphrase", name)
			return nil
		}
	case "wpa-eap":
		pskComment = "#"
		if wconf.radiusSecret == "" {
			slog.Errorf("radius secret undefined")
			return nil
		}
	default:
		slog.Errorf("VAP %s: unsupported key management: %s", name,
			vap.KeyMgmt)
		return nil
	}

	if satellite {
		radiusServer = getGatewayIP()
	} else {
		radiusServer = "127.0.0.1"
	}

	if idx == 0 {
		bssid = "bssid=" + d.hwaddr
		logical = d
	} else {
		// If we create multiple SSIDs, hostapd will generate additional
		// bssids by incrementing the final octet of the nic's mac
		// address.
		p := initPseudoNic(d, idx)
		physDevices[getNicID(p)] = p
		bssid = "bss=" + p.name
		logical = p
	}
	confPrefix := fmt.Sprintf("%s/hostapd.%s.%s", confdir, d.name, name)

	data := vapConfig{
		Name:       name,
		idx:        idx,
		physical:   d,
		logical:    logical,
		vap:        vap,
		BSSID:      bssid,
		SSID:       ssid,
		Passphrase: passphrase,
		KeyMgmt:    strings.ToUpper(vap.KeyMgmt),
		PskComment: pskComment,
		EapComment: eapComment,
		ConfPrefix: confPrefix,

		RadiusAuthServer:     radiusServer,
		RadiusAuthServerPort: "1812",
		RadiusAuthSecret:     wconf.radiusSecret,
	}

	return &data
}

//
// Generate the configuration files needed for hostapd.
//
func generateVlanConf(vap *vapConfig) {
	// Create the 'vlan' file, which tells hostapd which vlans to create
	vfn := vap.ConfPrefix + ".vlan"
	vf, err := os.Create(vfn)
	if err != nil {
		slog.Fatalf("Unable to create %s: %v", vfn, err)
	}

	vapRings := make(cfgapi.RingMap)
	noVlan := rings[base_def.RING_UNENROLLED]
	for name, ring := range rings {
		if ring.VirtualAP == vap.Name && ring != noVlan {
			vapRings[name] = ring
			fmt.Fprintf(vf, "%d %s.%d\n", ring.Vlan,
				vap.physical.name, ring.Vlan)
		}
	}
	vf.Close()

	// Create the 'accept_macs' file, which tells hostapd how to map clients
	// to VLANs.
	mfn := vap.ConfPrefix + ".macs"
	mf, err := os.Create(mfn)
	if err != nil {
		slog.Fatalf("Unable to create %s: %v", mfn, err)
	}

	// One client per line, containing "<mac addr> <vlan_id>"
	for client, info := range clients {
		if ring, ok := vapRings[info.Ring]; ok {
			fmt.Fprintf(mf, "%s %d\n", client, ring.Vlan)
		}
	}
	mf.Close()
}

func (h *hostapdHdl) generateHostAPDConf() {
	devfile := *templateDir + "/hostapd.conf.got"
	apfile := *templateDir + "/virtualap.conf.got"

	files := make([]string, 0)
	unenrolled := make([]*physDevice, 0)
	devices := make([]*physDevice, 0)
	allVaps := make([]*vapConfig, 0)

	// build an alphabetical list of vap names, so the order of VAPs in the
	// config file is deterministic
	vaps := make([]string, 0)
	for name := range virtualAPs {
		vaps = append(vaps, name)
	}
	sort.Strings(vaps)

	devTemplate, err := template.ParseFiles(devfile)
	if err != nil {
		slog.Errorf("Unable to parse %s: %v", devfile, err)
		return
	}
	vapTemplate, err := template.ParseFiles(apfile)
	if err != nil {
		slog.Errorf("Unable to parse %s: %v", apfile, err)
		return
	}
	unenrolledVap := rings[base_def.RING_UNENROLLED].VirtualAP
	for _, d := range h.devices {
		confName := confdir + "/" + "hostapd.conf." + d.name
		cf, _ := os.Create(confName)
		defer cf.Close()

		dev := getDevConfig(d)
		if err = devTemplate.Execute(cf, dev); err != nil {
			slog.Warnf("%v", err)
			continue
		}

		max := d.wifi.cap.Interfaces
		for idx, name := range vaps {
			if idx == max {
				slog.Warnf("%s can only support %d of %d SSIDs",
					d.hwaddr, max, len(virtualAPs))
				break
			}
			if vap := getVAPConfig(name, d, idx); vap != nil {
				generateVlanConf(vap)
				err = vapTemplate.Execute(cf, vap)
				if err == nil {
					allVaps = append(allVaps, vap)
					idx++
				} else {
					slog.Warnf("%v", err)
				}
				if name == unenrolledVap {
					unenrolled = append(unenrolled,
						vap.logical)
				}
			}
		}

		files = append(files, confName)
		devices = append(devices, d)
	}

	updateNicProperties()

	h.vaps = allVaps
	h.devices = devices
	h.unenrolled = unenrolled
	h.confFiles = files
}

func (h *hostapdHdl) cleanup() {
	for _, c := range h.conns {
		os.Remove(c.localName)
	}
}

func (h *hostapdHdl) newConn(vap *vapConfig) *hostapdConn {
	// There are two endpoints for each control socket.  The remoteName is
	// owned by hostapd, and we need to use the name that it expects.  The
	// localName is owned by us, and the format is chosen by us.
	fullName := vap.physical.name
	if vap.idx != 0 {
		fullName += "_" + strconv.Itoa(vap.idx)
	}
	remoteName := "/var/run/hostapd/" + fullName
	localName := "/tmp/hostapd_ctrl_" + fullName + "-" +
		strconv.Itoa(os.Getpid())

	newConn := hostapdConn{
		hostapd:     h,
		name:        fullName,
		remoteName:  remoteName,
		localName:   localName,
		vapName:     vap.Name,
		wifiBand:    vap.physical.wifi.activeBand,
		active:      true,
		device:      vap.physical,
		pendingCmds: make([]*hostapdCmd, 0),
		retransmits: make(map[string]*retransmitState),
		stations:    make(map[string]*stationInfo),
	}
	slog.Debugf("%v: %s -> %s", &newConn, remoteName, localName)
	os.Remove(newConn.name)
	return &newConn
}

func (h *hostapdHdl) start() {
	h.generateHostAPDConf()
	if len(h.devices) == 0 {
		h.done <- fmt.Errorf("no suitable wireless devices available")
		return
	}
	defer h.cleanup()

	slog.Debugf("starting hostapd")
	// There is a control interface for each BSSID
	for _, v := range h.vaps {
		h.conns = append(h.conns, h.newConn(v))
	}

	stopNetworkRebuild := make(chan bool, 1)
	go rebuildUnenrolled(h.unenrolled, stopNetworkRebuild)

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
		virtualAPs = config.GetVirtualAPs()
		h.generateHostAPDConf()
		h.process.Signal(plat.ReloadSignal)
	}
}

func (h *hostapdHdl) reset() {
	if h != nil {
		slog.Infof("Killing hostapd")
		virtualAPs = config.GetVirtualAPs()
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

func getWifiDevices(active []*physDevice) []*physDevice {
	warned := false
	for {
		active = selectWifiDevices(active)
		if len(active) > 0 || !running {
			return active
		}

		if !warned {
			slog.Warnf("no wireless devices available")
			warned = true
		}
		time.Sleep(time.Second)
	}
}

func hostapdLoop() {
	var active []*physDevice

	startTimes := make([]time.Time, failuresAllowed)
	virtualAPs = config.GetVirtualAPs()
	for running {
		active = selectWifiDevices(active)
		if !running {
			break
		}

		startTimes = append(startTimes[1:failuresAllowed],
			time.Now())

		hostapd = startHostapd(active)
		if err := hostapd.wait(); err != nil {
			slog.Warnf("%v", err)
			active = nil
			wifiEvaluate = true
		}
		hostapd = nil
		if !running {
			break
		}

		if time.Since(startTimes[0]) < period {
			slog.Warnf("hostapd is dying too quickly")
			wifiEvaluate = false
		}
		resetInterfaces()
	}
}
