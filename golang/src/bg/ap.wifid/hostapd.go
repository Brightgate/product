/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"bg/ap_common/publiclog"
	"bg/ap_common/wificaps"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"
	"bg/common/network"
	"bg/common/wifi"

	"github.com/golang/protobuf/proto"
	"go.uber.org/zap/zapcore"
)

const (
	confdir        = "/tmp"
	hostapdOptions = "-dKt"
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
	Interface   string // Linux device name
	Mode        string
	Channel     int
	CountryCode string

	ModeNComment string // Enable 802.11n
	HTCapab      string // Set the ht_capab field for 802.11n

	ModeACComment string // Enable 802.11ac
	DFSComment    string // Enable 802.11d
	VHTComment    string // Enable 802.11ac VHT capabilities
	VHTCapab      string // Set the vht_capab field for 802.11ac

	VHTWidthComment   string // Enable 802.11ac 80MHz channel
	VHTChanWidth      int
	VHTCenterFreqSeg0 int
}

type hostapdCmd struct {
	cmd    string
	res    string
	err    chan error
	queued time.Time
	sent   time.Time
}

type retransmitState struct {
	count     int       // how many RETRANSMIT events have we seen?
	broken    bool      // has this client exceeded its hard limit?
	restarted bool      // has hostapd been reset during RETRANSMIT loop?
	first     time.Time // time of the first RETRANSMIT event
	last      time.Time // time of the most recent RETRANSMIT event
}

var (
	clientRetransmits    = make(map[string]*retransmitState)
	clientRetransmitsMtx sync.Mutex
)

// hostapd has a separate control socket for each of the network interfaces it
// manages.  For each socket, we can have a single in-flight command and any
// number of queued commands.
type hostapdConn struct {
	active      bool
	eap         bool
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

	inStatus bool // currently collecting per-station status
	stations map[string]*stationInfo

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

func sendNetEntity(mac string, username, vapName, bandName, sig *string, disconnect bool) {
	band := "?"
	if bandName != nil {
		band = *bandName
	}
	action := "connect"
	if disconnect {
		action = "disconnect"
	}
	user := "?"
	if username != nil {
		user = *username
	}
	vap := "?"
	if vapName != nil {
		vap = *vapName
	}

	slog.Debugf("NetEntity(%s, user: %s vap: %s, band: %s, %s)", mac, user,
		vap, band, action)
	hwaddr, _ := net.ParseMAC(mac)
	entity := &base_msg.EventNetEntity{
		Timestamp:     aputil.NowToProtobuf(),
		Sender:        proto.String(brokerd.Name),
		Debug:         proto.String("-"),
		Username:      username,
		VirtualAP:     vapName,
		WifiSignature: sig,
		Node:          &nodeID,
		Disconnect:    &disconnect,
		MacAddress:    proto.Uint64(network.HWAddrToUint64(hwaddr)),
		Band:          bandName,
	}

	err := brokerd.Publish(entity, base_def.TOPIC_ENTITY)
	if err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_ENTITY, err)
	}
}

func sendNetException(mac, username string, vapName *string,
	reason *base_msg.EventNetException_Reason) {

	vap := "?"
	if vapName != nil {
		vap = *vapName
	}

	slog.Debugf("NetException(%s, vap: %s,  user: %s, reason: %d)",
		mac, vap, *reason)
	hwaddr, _ := net.ParseMAC(mac)
	entity := &base_msg.EventNetException{
		Timestamp:  aputil.NowToProtobuf(),
		Sender:     proto.String(brokerd.Name),
		Debug:      proto.String("-"),
		VirtualAP:  vapName,
		Reason:     reason,
		MacAddress: proto.Uint64(network.HWAddrToUint64(hwaddr)),
	}
	if username != "" {
		entity.Username = proto.String(username)
	}

	err := brokerd.Publish(entity, base_def.TOPIC_EXCEPTION)
	if err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_EXCEPTION, err)
	}
}

var signalRE = regexp.MustCompile(`signal=(\S+)\s`)

// Fetch a single station's status from hostapd.  Return the signal strength.
func (c *hostapdConn) statusOne(sta string) (string, error) {
	var rval string

	status, err := c.command("STA " + sta)
	if err == nil {
		f := signalRE.FindStringSubmatch(status)
		if len(f) != 0 {
			str := f[1]
			if _, err = strconv.Atoi(str); err == nil {
				rval = str
			}
		}
	}
	return rval, err
}

// Iterate over all of the known stations, polling for status.  Use that to
// update the per-client signal strength entries in the @/metrics tree.
func (c *hostapdConn) statusAll() {
	c.Lock()
	defer c.Unlock()

	if c.inStatus {
		slog.Warnf("found status collection already in progress")
		return
	}

	c.inStatus = true
	stations := make([]string, 0)
	for sta := range c.stations {
		stations = append(stations, sta)
	}
	c.Unlock()

	props := make(map[string]string)
	for _, sta := range stations {
		if str, err := c.statusOne(sta); err == nil {
			props["@/metrics/clients/"+sta+"/signal_str"] = str
		}
	}
	config.CreateProps(props, nil)

	c.Lock()
	c.inStatus = false
}

func (c *hostapdConn) getSignature(sta string) {
	sta = strings.ToLower(sta)
	sig, err := c.command("SIGNATURE " + sta)
	if err != nil {
		slog.Warnf("Failed to get signature for %s: %v", sta, err)
	} else if info, ok := c.stations[sta]; ok {
		if info.signature != sig {
			info.signature = sig
			sendNetEntity(sta, nil, &c.vapName, nil, &sig, false)
		}
	}
}

func (c *hostapdConn) stationPresent(sta string, newConnection bool) {
	sta = strings.ToLower(sta)
	slog.Infof("%v stationPresent(%s) new: %v", c, sta, newConnection)
	info := c.stations[sta]
	if info == nil {
		sendNetEntity(sta, nil, &c.vapName, &c.wifiBand, nil, false)
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
	sendNetEntity(sta, nil, &c.vapName, &c.wifiBand, nil, true)
}

func (c *hostapdConn) stationRetransmit(sta string) {
	reason := base_msg.EventNetException_CLIENT_RETRANSMIT

	slog.Infof("%v stationRetransmit(%s)", c, sta)
	sendNetException(sta, "", &c.vapName, &reason)
}

func (c *hostapdConn) eapSuccess(sta, username string) {
	var user *string

	clientRetransmitsMtx.Lock()
	if state, ok := clientRetransmits[sta]; ok {
		delete(clientRetransmits, sta)
		if state.count != 0 {
			slog.Infof("%s connected after %d retransmits", sta,
				state.count)
		}
	}
	clientRetransmitsMtx.Unlock()

	if len(username) > 0 {
		user = &username
	}

	slog.Infof("%v eapSuccess(%s) user=%s", c, sta, username)

	sendNetEntity(sta, user, &c.vapName, &c.wifiBand, nil, false)
	publiclog.SendLogLoginEAPSuccess(brokerd, sta, username)
}

func (c *hostapdConn) stationBadPassword(sta, username string) {
	reason := base_msg.EventNetException_BAD_PASSWORD

	slog.Infof("%v stationBadPassword(%s) user=%s", c, sta, username)

	sendNetException(sta, username, &c.vapName, &reason)
	publiclog.SendLogLoginRepeatedFailure(brokerd, sta, username)
}

func (c *hostapdConn) deauthSta(sta string) {
	sta = strings.ToLower(sta)
	slog.Infof("%v deauthenticating(%s)", c, sta)
	c.command("DEAUTHENTICATE " + sta)
}

func (c *hostapdConn) disassociate(sta string) {
	sta = strings.ToLower(sta)
	slog.Infof("%v disassociating(%s)", c, sta)
	c.command("DISASSOCIATE " + sta)
}

// Fetch the retransmit state for a specific client.  If that client has no
// state yet, allocate a state struct and insert it into the map
func getClientRetransmit(mac string) *retransmitState {
	now := time.Now()
	expired := now.Add(-1 * *retransmitTimeout)

	clientRetransmitsMtx.Lock()
	defer clientRetransmitsMtx.Unlock()

	// While we're scanning the map, clean up any counts that have aged out
	for x, state := range clientRetransmits {
		if !state.broken && state.last.Before(expired) {
			slog.Debugf("%s is clear.  %d since %s",
				x, state.count, state.first.Format(time.RFC3339))
			delete(clientRetransmits, x)
		}
	}

	state := clientRetransmits[mac]
	if state == nil {
		state = &retransmitState{first: now}
		clientRetransmits[mac] = state
	}
	state.last = now

	return state
}

// Set the 'restarted' bit for all clients in the retransmit map
func markClientRetransmit() {
	clientRetransmitsMtx.Lock()
	defer clientRetransmitsMtx.Unlock()

	for _, state := range clientRetransmits {
		state.restarted = true
	}
}

// There is currently a bug on the OpenWRT boards where a client will fail to
// authenticate with EAP despite having valid credentials.  We can see this
// happening in the log as hostapd repeatedly issues RETRANSMIT messages.  The
// retries appear to happen with backoffs of 3, 6, 12, 20, 20, and 20 seconds
// before the operation finally times out.
//
// When a client is forcibly deauthenticated, this seems to clear the problem in
// a way that simply timing out and retrying the connection doesn't.  If that
// isn't sufficient, restarting hostapd always seems to be.
func (c *hostapdConn) eapRetransmit(mac string) {
	state := getClientRetransmit(mac)
	state.count++

	if state.broken {
		return
	} else if state.count >= *retransmitHardLimit {
		state.broken = true
		if state.count == *retransmitHardLimit {
			c.stationRetransmit(mac)
		}

		if !state.restarted {
			slog.Warnf("%d retransmits for %s since %s - "+
				"restarting hostapd", state.count, mac,
				state.first.Format(time.RFC3339))

			// Remember which clients have been through a restart.
			// If this doesn't fix them, then we don't want to try
			// restarting hostapd again on their behalf.  In
			// particular, we don't want to restart hostapd every 2
			// minutes trying to fix one permanently broken client.
			markClientRetransmit()
			c.hostapd.reset()

		}

	} else if state.count >= *retransmitSoftLimit {
		slog.Warnf("%d retransmits for %s since %s - kicking",
			state.count, mac, state.first.Format(time.RFC3339))
		go c.deauthSta(mac)
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
		//    CTRL-EVENT-EAP-SUCCESS2 b8:27:eb:9f:d8:e0 [username] (success)
		//    CTRL-EVENT-EAP-FAILURE2 b8:27:eb:9f:d8:e0 [username] (bad password)
		//    CTRL-EVENT-EAP-RETRANSMIT b8:27:eb:9f:d8:e0 (possibly T268)
		msgs = "(AP-STA-CONNECTED|AP-STA-DISCONNECTED|" +
			"AP-STA-POLL-OK|AP-STA-POSSIBLE-PSK-MISMATCH|" +
			"CTRL-EVENT-EAP-SUCCESS2|CTRL-EVENT-EAP-FAILURE2|" +
			"CTRL-EVENT-EAP-RETRANSMIT|CTRL-EVENT-EAP-RETRANSMIT2)"
		macOctet = "[[:xdigit:]][[:xdigit:]]"
		macAddr  = "(" + macOctet + ":" + macOctet + ":" +
			macOctet + ":" + macOctet + ":" + macOctet + ":" +
			macOctet + ")"
		username = " ?(.*)$"
	)

	re := regexp.MustCompile(msgs + " " + macAddr + username)
	m := re.FindStringSubmatch(status)
	if len(m) >= 3 {
		var username string

		msg := m[1]
		mac := m[2]
		if len(m) == 4 {
			username = m[3]
		}

		switch msg {
		case "AP-STA-CONNECTED":
			c.stationPresent(mac, true)
		case "AP-STA-POLL-OK":
			c.stationPresent(mac, false)
		case "AP-STA-DISCONNECTED":
			c.stationGone(mac)
		case "CTRL-EVENT-EAP-SUCCESS2":
			c.eapSuccess(mac, username)
		case "AP-STA-POSSIBLE-PSK-MISMATCH", "CTRL-EVENT-EAP-FAILURE2":
			c.stationBadPassword(mac, username)
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
	pingTick := time.NewTicker(time.Second * 5)
	defer pingTick.Stop()

	statusTick := time.NewTicker(time.Second * 10)
	defer statusTick.Stop()

	for {
		select {
		case <-exit:
			return
		case <-pingTick.C:
			c.command("PING")
		case <-statusTick.C:
			c.statusAll()
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

func getHTCaps(w *wifiInfo) string {
	var rval string

	htcaps := w.cap.HTCapabilities
	if htcaps[wificaps.HTCAP_MAX_AMSDU_7935] {
		rval += "[MAX-AMSDU-7935]"
	}
	if htcaps[wificaps.HTCAP_DELAYED_BA] {
		rval += "[DELAYED-BA]"
	}
	if htcaps[wificaps.HTCAP_TX_STBC] {
		rval += "[TX-STBC]"
	}
	if htcaps[wificaps.HTCAP_RX_STBC123] {
		rval += "[RX-STBC123]"
	} else if htcaps[wificaps.HTCAP_RX_STBC12] {
		rval += "[RX-STBC12]"
	} else if htcaps[wificaps.HTCAP_RX_STBC1] {
		rval += "[RX-STBC1]"
	}
	if w.activeBand == wifi.HiBand && w.activeWidth >= 40 {
		// With a 40MHz channel, we can support a secondary
		// 20MHz channel either above or below the primary,
		// depending on what the primary channel is.
		if nModePrimaryAbove[w.activeChannel] {
			rval += "[HT40+]"
		}
		if nModePrimaryBelow[w.activeChannel] {
			rval += "[HT40-]"
		}
		if htcaps[wificaps.HTCAP_HT40_SGI] {
			rval += "[SHORT-GI-40]"
		}
		if htcaps[wificaps.HTCAP_DSSS_CCK] {
			rval += "[DSSS_CCK-40]"
		}
	} else if htcaps[wificaps.HTCAP_HT20_SGI] {
		rval += "[SHORT-GI-20]"
	}

	return rval
}

func getVHTCaps(w *wifiInfo) string {
	capToFlag := map[int]string{
		wificaps.VHTCAP_MAX_MPDU_7991:        "[MAX-MPDU-7991]",
		wificaps.VHTCAP_MAX_MPDU_11454:       "[MAX-MPDU-11454]",
		wificaps.VHTCAP_RX_LDPC:              "[RXLDPC]",
		wificaps.VHTCAP_VHT80_SGI:            "[SHORT-GI-80]",
		wificaps.VHTCAP_VHT160_SGI:           "[SHORT-GI-160]",
		wificaps.VHTCAP_TX_STBC:              "[TX-STBC-2BY1]",
		wificaps.VHTCAP_SU_BEAMFORMER:        "[SU-BEAMFORMER]",
		wificaps.VHTCAP_SU_BEAMFORMEE:        "[SU-BEAMFORMEE]",
		wificaps.VHTCAP_MU_BEAMFORMER:        "[MU-BEAMFORMER]",
		wificaps.VHTCAP_MU_BEAMFORMEE:        "[MU-BEAMFORMEE]",
		wificaps.VHTCAP_RX_STBC1:             "[RX-STBC-1]",
		wificaps.VHTCAP_RX_STBC2:             "[RX-STBC-12]",
		wificaps.VHTCAP_RX_STBC3:             "[RX-STBC-123",
		wificaps.VHTCAP_RX_STBC4:             "[RX-STBC-1234]",
		wificaps.VHTCAP_BF_ANTENNA_2:         "[BF-ANTENNA-2]",
		wificaps.VHTCAP_BF_ANTENNA_3:         "[BF-ANTENNA-3]",
		wificaps.VHTCAP_BF_ANTENNA_4:         "[BF-ANTENNA-4]",
		wificaps.VHTCAP_SOUNDING_DIMENSION_1: "[SOUNDING-DIMENSION-1]",
		wificaps.VHTCAP_SOUNDING_DIMENSION_2: "[SOUNDING-DIMENSION-2]",
		wificaps.VHTCAP_SOUNDING_DIMENSION_3: "[SOUNDING-DIMENSION-3]",
		wificaps.VHTCAP_SOUNDING_DIMENSION_4: "[SOUNDING-DIMENSION-4]",
		wificaps.VHTCAP_MAX_A_MPDU_LEN_EXP1:  "[MAX-A-MPDU-LEN-EXP1]",
		wificaps.VHTCAP_MAX_A_MPDU_LEN_EXP2:  "[MAX-A-MPDU-LEN-EXP2]",
		wificaps.VHTCAP_MAX_A_MPDU_LEN_EXP3:  "[MAX-A-MPDU-LEN-EXP3]",
		wificaps.VHTCAP_MAX_A_MPDU_LEN_EXP4:  "[MAX-A-MPDU-LEN-EXP4]",
		wificaps.VHTCAP_MAX_A_MPDU_LEN_EXP5:  "[MAX-A-MPDU-LEN-EXP5]",
		wificaps.VHTCAP_MAX_A_MPDU_LEN_EXP6:  "[MAX-A-MPDU-LEN-EXP6]",
	}

	// Sort the device's capabilities so the ordering of the options in the
	// file is consistent from run to run
	caps := aputil.SortIntKeys(w.cap.VHTCapabilities)
	rval := ""
	for _, cap := range caps {
		if option, ok := capToFlag[cap]; ok {
			rval += option
		}
	}

	return rval
}

func getDevConfig(d *physDevice) *devConfig {
	var hwMode, htCapab, vhtCapab string
	var chanWidth, centerFreq int

	w := d.wifi

	if w.activeBand == wifi.LoBand {
		hwMode = "g"
	} else if w.activeBand == wifi.HiBand {
		hwMode = "a"
	} else {
		slog.Warnf("unsupported wifi band: %s", d.wifi.activeBand)
		return nil
	}

	modeACComment := "#"
	modeNComment := "#"
	dfsComment := "#"
	vhtComment := "#"
	vhtWidthComment := "#"
	if hwMode == "a" && w.cap.WifiModes["ac"] {
		modeACComment = ""
		vhtCapab = getVHTCaps(w)
		if len(vhtCapab) > 0 {
			vhtComment = ""
		}

		if w.activeWidth == 80 {
			vhtWidthComment = ""
			chanWidth = 1
			centerFreq = w.activeChannel + 6
		}
		if d.wifi.activeChannel > 36 && d.wifi.activeChannel < 149 {
			// XXX: it looks like DFS is only needed for frequencies
			// associated with channels between 50 and 144.  Because
			// of the way the AC channels are laid out, that
			// effectively includes all channels above 36 and below
			// 149.  All of this should really be vetted by somebody
			// more qualified.
			dfsComment = ""
		}
	}
	if w.cap.WifiModes["n"] {
		modeNComment = ""
		htCapab = getHTCaps(w)
	}

	data := devConfig{
		Interface:   d.name,
		Mode:        hwMode,
		CountryCode: wconf.domain,
		Channel:     d.wifi.activeChannel,

		ModeNComment: modeNComment,
		HTCapab:      htCapab,

		ModeACComment:     modeACComment,
		DFSComment:        dfsComment,
		VHTComment:        vhtComment,
		VHTCapab:          vhtCapab,
		VHTWidthComment:   vhtWidthComment,
		VHTChanWidth:      chanWidth,
		VHTCenterFreqSeg0: centerFreq,
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
	if vap.Disabled {
		slog.Infof("VAP %s: disabled", name)
		return nil
	}

	if len(vap.Rings) == 0 {
		slog.Infof("VAP %s: no assigned rings", name)
		return nil
	}

	ssid := vap.SSID
	if vap.Tag5GHz && d.wifi.activeBand == wifi.HiBand {
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
		id := plat.NicID(p.name, p.hwaddr)
		wirelessNics[id] = p
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
func generateVlanConf(vap *vapConfig) error {
	// Determine all of the rings/vlans accessible via this VAP
	vapVlans := make(map[string]int)
	for ring, ringInfo := range rings {
		if ring == base_def.RING_UNENROLLED {
			continue
		}
		for _, ringVap := range ringInfo.VirtualAPs {
			if ringVap == vap.Name {
				vapVlans[ring] = ringInfo.Vlan
			}
		}
	}

	// Create the 'vlan' file, which tells hostapd which vlans to use
	vfn := vap.ConfPrefix + ".vlan"
	vf, err := os.Create(vfn)
	if err != nil {
		return fmt.Errorf("Unable to create %s: %v", vfn, err)
	}

	for _, vlan := range vapVlans {
		fmt.Fprintf(vf, "%d %s_%s.%d\n", vlan, vap.physical.name,
			vap.Name, vlan)
	}
	vf.Close()

	// Create the 'accept_macs' file, which tells hostapd how to map clients
	// to VLANs.
	mfn := vap.ConfPrefix + ".macs"
	mf, err := os.Create(mfn)
	if err != nil {
		return fmt.Errorf("Unable to create %s: %v", mfn, err)
	}

	// One client per line, containing "<mac addr> <vlan_id>"
	for client, info := range clients {
		if vlan, ok := vapVlans[info.Ring]; ok {
			fmt.Fprintf(mf, "%s %d\n", client, vlan)
		}
	}
	mf.Close()

	return nil
}

func (h *hostapdHdl) deauthUser(user string) {
	// We don't currently have a user->device mapping, so we deauth all
	// devices
	for _, c := range h.conns {
		list := make([]string, 0)
		if c.eap {
			c.Lock()
			for sta := range c.stations {
				list = append(list, sta)
			}
			c.Unlock()
		}

		for _, sta := range list {
			slog.Debugf("deauthing %s from %s", sta, c.name)
			c.disassociate(sta)
		}
	}
}

func (h *hostapdHdl) disassociate(sta string) {
	sta = strings.ToLower(sta)
	for _, c := range h.conns {
		c.Lock()
		_, ok := c.stations[sta]
		c.Unlock()

		if ok {
			slog.Infof("kicking %s from %s", sta, c.name)
			c.disassociate(sta)
		}
	}
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
	vaps := aputil.SortStringKeys(virtualAPs)

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

	unenrolledVap := rings[base_def.RING_UNENROLLED].VirtualAPs[0]
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
		idx := 0
		for _, name := range vaps {
			if idx == max {
				slog.Warnf("%s can only support %d of %d SSIDs",
					d.hwaddr, max, len(virtualAPs))
				break
			}
			if vap := getVAPConfig(name, d, idx); vap != nil {
				if err = generateVlanConf(vap); err == nil {
					err = vapTemplate.Execute(cf, vap)
				}
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

	h.vaps = allVaps
	h.devices = devices
	h.unenrolled = unenrolled
	h.confFiles = files
}

func (h *hostapdHdl) generateConfigFiles() {
	h.generateHostAPDConf()

	if aputil.IsGatewayMode() {
		fn, err := generateRadiusConfig()
		if err == nil {
			h.confFiles = append(h.confFiles, fn)
		} else {
			slog.Warnf("failed to generate radius config: %v", err)
		}
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
		eap:         strings.EqualFold(vap.KeyMgmt, "wpa-eap"),
		name:        fullName,
		remoteName:  remoteName,
		localName:   localName,
		vapName:     vap.Name,
		wifiBand:    vap.physical.wifi.activeBand,
		active:      true,
		device:      vap.physical,
		pendingCmds: make([]*hostapdCmd, 0),
		stations:    make(map[string]*stationInfo),
	}
	slog.Debugf("%v: %s -> %s", &newConn, remoteName, localName)
	os.Remove(newConn.name)
	return &newConn
}

func (h *hostapdHdl) start() {
	slog.Debugf("starting hostapd")

	h.generateConfigFiles()

	// There is a control interface for each BSSID
	for _, v := range h.vaps {
		conn := h.newConn(v)
		defer os.Remove(conn.localName)
		h.conns = append(h.conns, conn)
	}

	stopNetworkRebuild := make(chan bool, 1)
	go rebuildUnenrolled(h.unenrolled, stopNetworkRebuild)

	args := make([]string, 0)
	if *hostapdVerbose {
		args = append(args, "-dd")
	} else if *hostapdDebug {
		args = append(args, "-d")
	}
	args = append(args, h.confFiles...)
	h.process = aputil.NewChild(plat.HostapdCmd, args...)
	h.process.UseZapLog("hostapd: ", slog, zapcore.InfoLevel)

	slog.Infof("Starting hostapd")

	hotplugBlock()
	hotplugBlocked := true

	startTime := time.Now()
	if err := h.process.Start(); err != nil {
		hotplugUnblock()
		stopNetworkRebuild <- true
		h.done <- fmt.Errorf("failed to launch: %v", err)
		return
	}

	var wg sync.WaitGroup
	for _, c := range h.conns {
		wg.Add(1)
		go c.run(&wg)
	}

	// create a channel which will be signalled when the child exits
	waiting := true
	childChan := make(chan error, 1)
	go h.process.WaitChan(childChan)

	// create a channel which will be signalled in 10 seconds.  This should
	// give hostapd enough time to create its devices and populate the
	// bridges before we re-enable the hotplug scripts.
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()

	for waiting {
		select {
		case <-timer.C:
		case <-childChan:
			waiting = false
		}
		if hotplugBlocked {
			hotplugUnblock()
			hotplugBlocked = false
		}
	}

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
		h.generateConfigFiles()
		h.process.Signal(plat.ReloadSignal)
	}
}

func (h *hostapdHdl) reset() {
	if h != nil {
		slog.Infof("Resetting hostapd")
		virtualAPs = config.GetVirtualAPs()
		h.generateConfigFiles()
		h.process.Signal(plat.ResetSignal)
	}
}

func (h *hostapdHdl) halt() {
	slog.Infof("Halting hostapd")
	p := h.process

	p.Signal(plat.ResetSignal)
	time.AfterFunc(200*time.Millisecond, func() {
		if h.process == p {
			slog.Infof("Killing hostapd")
			p.Signal(syscall.SIGKILL)
		}
	})
}

// While hostapd was offline, all wireless clients necessarily disconnected.
// Iterate over the client list and set the 'active' state to false for all
// wireless clients associated with this node, or any node that no longer
// exists.
func clearActive() {
	// Build a map of the valid node names
	nodeSlice, _ := config.GetNodes()
	nodes := make(map[string]bool)
	for _, node := range nodeSlice {
		nodes[node.ID] = true
	}

	ops := make([]cfgapi.PropertyOp, 0)
	for mac, client := range clients {
		if (client.ConnNode == nodeID || !nodes[client.ConnNode]) &&
			client.Wireless {

			op := cfgapi.PropertyOp{
				Op:    cfgapi.PropCreate,
				Name:  "@/clients/" + mac + "/connection/active",
				Value: "false",
			}
			slog.Debugf("Setting %s to false", op.Name)
			ops = append(ops, op)
		}
	}
	if len(ops) > 0 {
		if _, err := config.Execute(nil, ops).Wait(nil); err != nil {
			slog.Warnf("Error clearing Active states: %v", err)
		}
	}
}

func startHostapd(devs []*physDevice) *hostapdHdl {
	clearActive()

	h := &hostapdHdl{
		devices: devs,
		conns:   make([]*hostapdConn, 0),
		done:    make(chan error, 1),
	}

	go h.start()
	return h
}

// If the channel/width/etc settings have changed, log it.  This returns a slice
// of per-device descriptions, which we'll use on the next invocation to detect
// changes.
func logActiveChange(dev []*physDevice, oldDescs []string) []string {
	newDescs := make([]string, 0)

	for _, d := range dev {
		desc := fmt.Sprintf("(dev: %s mode: %s chan: %d width: %d)",
			d.name, d.wifi.activeMode, d.wifi.activeChannel,
			d.wifi.activeWidth)
		newDescs = append(newDescs, desc)
	}
	sort.Strings(newDescs)

	changed := len(newDescs) != len(oldDescs)
	if !changed {
		for i := range oldDescs {
			changed = changed || (oldDescs[i] != newDescs[i])
		}
	}
	if changed {
		old := "[" + list(oldDescs) + "]"
		now := "[" + list(newDescs) + "]"
		slog.Infof("Wireless settings changed from %s to %s",
			old, now)
	}
	return newDescs
}

func hostapdLoop(wg *sync.WaitGroup, doneChan chan bool) {
	var active []*physDevice
	var activeDescs []string
	var err error

	slog.Infof("hostapd loop starting")
	p := aputil.NewPaceTracker(failuresAllowed, period)
	virtualAPs = config.GetVirtualAPs()

runLoop:
	for {
		active = selectWifiDevices(active)
		active = selectWifiChannels(active)
		if len(active) == 0 {
			// XXX: This should be event-driven rather than
			// timeout-driven.  Getting that right complicated an
			// already large code delta, so I'll do it as a
			// follow-on.
			time.Sleep(time.Second)
			continue
		}
		activeDescs = logActiveChange(active, activeDescs)

		hostapd = startHostapd(active)
		select {
		case <-doneChan:
			break runLoop
		case err = <-hostapd.done:
			hostapd = nil
		}

		if err != nil {
			slog.Warnf("%v", err)
			active = nil
			wifiEvaluate = true
		}

		if err = p.Tick(); err != nil {
			slog.Warnf("hostapd is dying too quickly: %v", err)
			wifiEvaluate = false
		}
		wifiCleanup()
	}

	if hostapd != nil {
		slog.Infof("killing active hostapd")
		hostapd.halt()
		<-hostapd.done
	}

	slog.Infof("hostapd loop exiting")

	wg.Done()
}
