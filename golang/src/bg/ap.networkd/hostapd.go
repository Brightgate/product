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
	"os"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/network"
	"bg/base_def"
)

var (
	hostapdLog     *log.Logger
	hostapdProcess *aputil.Child // track the hostapd proc
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

func apReset(restart bool) {
	log.Printf("Resetting hostapd\n")

	// XXX: ideally, for simple changes we should just be able to update the
	// config files and SIGHUP the running hostapd.  In practice, it seems
	// like we need to fully restart hostapd for some of them to take
	// effect.  (Ring assignments in particular have required a full restart
	// for some iOS clients.  It might be worth exploring a soft reset
	// followed by an explicit deauth/disassoc for the affected client.)
	//
	hostapdProcess.Signal(syscall.SIGINT)
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

//////////////////////////////////////////////////////////////////////////
//
// hostapd configuration and monitoring
//

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

func generateHostAPDConf(devs []*physDevice) []string {
	tfile := *templateDir + "/hostapd.conf.got"
	files := make([]string, 0)

	for _, d := range devs {
		// Create hostapd.conf, using the apConfig contents to fill
		// out the .got template
		t, err := template.ParseFiles(tfile)
		if err != nil {
			log.Fatal(err)
			os.Exit(2)
		}

		confName := confdir + "/" + "hostapd.conf." + d.name
		cf, _ := os.Create(confName)
		defer cf.Close()

		conf := getAPConfig(d)
		err = t.Execute(cf, conf)
		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}
		generateVlanConf(conf, "wpa-psk")
		generateVlanConf(conf, "wpa-eap")

		files = append(files, confName)
	}

	return files
}

//
// Prepare and launch hostapd.  Return when the child hostapd process exits
//
func runOne(devs []*physDevice) error {
	var ra aputil.RunAbort

	deleteBridges()

	confFiles := generateHostAPDConf(devs)
	if len(confFiles) == 0 {
		return fmt.Errorf("no suitable wireless devices available")
	}

	createBridges()

	hostapdProcess = aputil.NewChild(hostapdPath, confFiles...)
	hostapdProcess.LogOutputTo("hostapd: ", log.Ldate|log.Ltime, os.Stderr)

	log.Printf("Starting hostapd\n")

	startTime := time.Now()
	if err := hostapdProcess.Start(); err != nil {
		return fmt.Errorf("failed to launch: %v", err)
	}

	ra.SetRunning()
	ra.ClearAbort()
	go resetInterfaces(&ra)

	hostapdProcess.Wait()

	log.Printf("hostapd exited after %s\n", time.Since(startTime))

	// If the child died before all the interfaces were reset, tell
	// them to give up.
	ra.SetAbort()
	for ra.IsRunning() {
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}
