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

// abstract configuration data for each virtual AP
type virtualAP struct {
	id         string // @/network/vap/<ID>/*
	ssid       string // ssid to advertise
	tag5GHz    bool   // Add -5GHz tag to the ssid?
	keyMgmt    string // wpa-eap or wpa-psk
	passphrase string // passphrase to use for wpa-psk network
}

var virtualAPs []*virtualAP

// Used to invoke hostapd, instantiating a virtual AP on a specific device
type vapConfig struct {
	idx    int         // per-device VAP index
	ID     string      // @/network/vap/<ID>/*
	vap    *virtualAP  // device-independent config for this AP
	device *physDevice // physical device hosting this virtual AP

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
	vapID       string        // virtual AP
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
	process   *aputil.Child  // the running hostapd child process
	devices   []*physDevice  // the physical NICs being used
	vaps      []*vapConfig   // the virtual APs being hosted
	confFiles []string       // config files passed to the child
	conns     []*hostapdConn // control sockets
	done      chan error
}

func initVAP(root *cfgapi.PropertyNode) (*virtualAP, error) {
	vap := &virtualAP{}

	if x := root.Children["ssid"]; x != nil {
		vap.ssid = x.Value
	} else {
		return nil, fmt.Errorf("missing ssid")
	}

	if x := root.Children["keymgmt"]; x != nil {
		vap.keyMgmt = x.Value
	} else {
		return nil, fmt.Errorf("missing keymgmt")
	}
	if vap.keyMgmt == "wpa-psk" {
		if node, ok := root.Children["passphrase"]; ok {
			vap.passphrase = node.Value
		} else {
			return nil, fmt.Errorf("missing WPA-PSK passphrase")
		}
	} else if wconf.radiusSecret == "" {
		return nil, fmt.Errorf("radius secret undefined")
	}

	if x := root.Children["5ghz"]; x != nil {
		b, err := strconv.ParseBool(x.Value)
		if err != nil {
			return nil, fmt.Errorf("malformed 5ghz: %s", x.Value)
		}
		vap.tag5GHz = b
	}

	return vap, nil
}

func initVirtualAPs() {
	// Identify which VAPs have rings assigned to them, and thus need to be
	// instantiated.
	activeVAPs := make(map[string]bool)
	for _, ring := range rings {
		if vap := ring.VirtualAP; vap != "" {
			activeVAPs[vap] = true
		}
	}

	props, err := config.GetProps("@/network/vap")
	if err != nil {
		slog.Warnf("failed to get virtual AP config: %v", err)
		return
	}

	// Generate hostapd config structures for each of the active VAPs
	vaps := make([]*virtualAP, 0)
	for name, conf := range props.Children {
		if activeVAPs[name] {
			vap, err := initVAP(conf)
			if err != nil {
				slog.Warnf("unable to init vap %s: %v", name, err)
			} else {
				vap.id = name
				vaps = append(vaps, vap)
			}
		} else {
			slog.Infof("ignoring VAP %s: no rings assigned", name)
		}
	}

	virtualAPs = vaps
}

func (c *hostapdConn) String() string {
	return fmt.Sprintf("%s:%s", c.name, c.vapID)
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

func sendNetEntity(mac string, vapID, mode, sig *string, disconnect bool) {
	band := "?"
	if mode != nil {
		band = *mode
	}
	action := "connect"
	if disconnect {
		action = "disconnect"
	}
	vap := "?"
	if vapID != nil {
		vap = *vapID
	}

	slog.Debugf("NetEntity(%s, vap: %s, mode: %s, %s)", mac, vap, band, action)
	hwaddr, _ := net.ParseMAC(mac)
	entity := &base_msg.EventNetEntity{
		Timestamp:     aputil.NowToProtobuf(),
		Sender:        proto.String(brokerd.Name),
		Debug:         proto.String("-"),
		Mode:          mode,
		VirtualAP:     vapID,
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
			sendNetEntity(sta, &c.vapID, nil, &sig, false)
		}
	}
}

func (c *hostapdConn) stationPresent(sta string, newConnection bool) {
	slog.Infof("%v stationPresent(%s) new: %v", c, sta, newConnection)
	info := c.stations[sta]
	if info == nil {
		sendNetEntity(sta, &c.vapID, &c.wifiBand, nil, false)
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
	sendNetEntity(sta, nil, &c.wifiBand, nil, true)
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
func macUpdateLastOctet(mac string, val uint64) string {
	maskSize := uint64(bits.Len(uint(maxSSIDs - 1)))
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
func getVAPConfig(d *physDevice, vap *virtualAP, idx int) *vapConfig {
	var bssid, eapComment, pskComment, passphrase, radiusServer string

	ssid := vap.ssid
	if vap.tag5GHz && d.wifi.activeBand == wificaps.HiBand {
		ssid += "-5ghz"
	}

	if vap.keyMgmt == "wpa-psk" {
		eapComment = "#"
		passphrase = vap.passphrase
	} else {
		pskComment = "#"
	}

	if satellite {
		radiusServer = getGatewayIP()
	} else {
		radiusServer = "127.0.0.1"
	}

	if idx == 0 {
		bssid = "bssid=" + d.hwaddr
	} else {
		bssid = fmt.Sprintf("bss=%s_%d", d.name, idx)
		// If we create multiple SSIDs, hostapd will generate additional
		// bssids by incrementing the final octet of the nic's mac
		// address.  hostapd requires that the base and generated mac
		// addresses share the upper 47 bits, so we need to ensure that
		// the base address has the lowest bits set to 0.
		p := initPseudoNic(d)
		p.hwaddr = macUpdateLastOctet(d.hwaddr, uint64(idx))
		physDevices[getNicID(p)] = p
	}
	confPrefix := fmt.Sprintf("%s/hostapd.%s.%s", confdir, d.name, vap.id)

	data := vapConfig{
		ID:         vap.id,
		idx:        idx,
		device:     d,
		vap:        vap,
		BSSID:      bssid,
		SSID:       ssid,
		Passphrase: passphrase,
		KeyMgmt:    strings.ToUpper(vap.keyMgmt),
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
		if ring.VirtualAP == vap.ID && ring != noVlan {
			vapRings[name] = ring
			fmt.Fprintf(vf, "%d %s.%d\n", ring.Vlan,
				vap.device.name, ring.Vlan)
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
	devices := make([]*physDevice, 0)
	vaps := make([]*vapConfig, 0)

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
		for idx, v := range virtualAPs {
			if idx == max {
				slog.Warnf("%s can only support %d of %d SSIDs",
					d.hwaddr, max, len(virtualAPs))
				break
			}
			if vap := getVAPConfig(d, v, idx); vap != nil {
				generateVlanConf(vap)
				err = vapTemplate.Execute(cf, vap)
				if err == nil {
					vaps = append(vaps, vap)
					idx++
				} else {
					slog.Warnf("%v", err)
				}
			}
		}

		files = append(files, confName)
		devices = append(devices, d)
	}

	updateNicProperties()

	h.vaps = vaps
	h.devices = devices
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
	fullName := vap.device.name
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
		vapID:       vap.ID,
		wifiBand:    vap.device.wifi.activeBand,
		active:      true,
		device:      vap.device,
		pendingCmds: make([]*hostapdCmd, 0),
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
		initVirtualAPs()
		h.generateHostAPDConf()
		h.process.Signal(plat.ReloadSignal)
	}
}

func (h *hostapdHdl) reset() {
	if h != nil {
		slog.Infof("Killing hostapd")
		initVirtualAPs()
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

func hostapdLoop() {
	var active []*physDevice

	startTimes := make([]time.Time, failuresAllowed)
	warned := false
	initVirtualAPs()
	for running {
		active = selectWifiDevices(active)
		if len(active) == 0 {
			if !warned {
				slog.Warnf("no wireless devices available")
				warned = true
			}
			if running {
				time.Sleep(time.Second)
			}
			continue
		}
		warned = false

		startTimes = append(startTimes[1:failuresAllowed],
			time.Now())

		hostapd = startHostapd(active)
		if err := hostapd.wait(); err != nil {
			slog.Warnf("%v", err)
			active = nil
			wifiEvaluate = true
		}
		hostapd = nil

		if time.Since(startTimes[0]) < period {
			slog.Warnf("hostapd is dying too quickly")
			wifiEvaluate = false
		}
		resetInterfaces()
	}
}
