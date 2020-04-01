/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Extraction of data used in the Bayesian classifier results in the
// concatenation of a sequence of synthetic terms (representing various
// features of each data type) into a "sentence".  Since we may evolve
// what features are extracted over time and we may choose to cache
// sentences as our dataset becomes large, each extractor has a version,
// represented by a single character.  The concatenation of these
// extractor versions is then the version of the generated sentence.

package extract

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"bg/base_msg"
	"bg/cl-obs/defs"
	"bg/cl-obs/sentence"
	"bg/common/network"

	"github.com/klauspost/oui"
)

const (
	// Update ediSeparatorVersion when either the separator constant or one
	// of the format constants is modified.
	//   The separator character, '_', is chosen to neutralize the
	//   tokenizing and stemming implementations in the underlying
	//   third party Bayesian algorithm.  That is, our vocabulary
	//   passes through its preprocessing unchanged.
	ediSeparatorVersion = "0"
	separator           = "_"

	// The trailing separators in the following formats prevent
	// stemming.
	termMacMfgFmt    = "hw_mac_mfg_%s_"
	termMacTripleFmt = "hw_mac_triple_%s_"

	termDHCPVendorFmt      = "dh_vendor_agent_%s_"
	termDHCPOptionsFmt     = "dh_vendor_options_%s_"
	emptyDHCPOptions       = "dh_vendor_options__"
	termDHCPAAPLSpecialFmt = "dh_aapl_special_%s_"

	termDNSHitFmt = "dns_%s_"

	dnsINRequestPat = ";(.*)\tIN\t (.*)"
)

var dnsINRequestRE = regexp.MustCompile(dnsINRequestPat)

func smashMfg(mfg string) string {
	l := strings.ToLower(mfg)
	l = strings.Replace(l, "(", separator, -1)
	l = strings.Replace(l, ")", separator, -1)
	l = strings.Replace(l, ",", separator, -1)
	l = strings.Replace(l, ".", separator, -1)
	l = strings.Replace(l, "-", separator, -1)
	l = strings.Replace(l, "\u00a0", separator, -1) // Non-breaking space.
	l = strings.Replace(l, " ", separator, -1)
	return strings.Trim(l, separator)
}

func macToMfgAlias(ouiDB oui.OuiDB, smac string) string {
	var mfg string

	if strings.HasPrefix(strings.ToLower(smac), "60:90:84:a") {
		return smashMfg("Brightgate, Inc.")
	}

	entry, err := ouiDB.Query(smac)

	if err != nil {
		mfg = defs.UnknownMfg
		return mfg
	}

	return smashMfg(entry.Manufacturer)
}

func wordifyDHCPOptions(opts []byte) string {
	stropt := make([]string, 0)

	for _, b := range opts {
		stropt = append(stropt, fmt.Sprintf("%d", b))
	}

	return strings.Join(stropt, separator)
}

const ediBaseVersion = "1"

// extract information from the top level of the DeviceInfo record.
func extractDeviceInfoBase(ouiDB oui.OuiDB, di *base_msg.DeviceInfo) sentence.Sentence {
	s := sentence.New()

	mac := network.Uint64ToMac(*di.MacAddress)

	// Manufacturer's name.
	mfg := macToMfgAlias(ouiDB, mac)
	s.AddTermf(termMacMfgFmt, smashMfg(mfg))

	// First three octets of MAC.
	s.AddTermf(termMacTripleFmt, strings.ReplaceAll(mac[0:8], ":", separator))

	return s
}

const ediDHCPVersion = "0"

var aaplLongBytes = []byte{1, 121, 3, 6, 15, 119, 252, 95, 44, 46}
var aaplShortBytes = []byte{1, 121, 3, 6, 15, 119, 252}

var specialDHCPOptions map[string][]byte = map[string][]byte{
	fmt.Sprintf(termDHCPAAPLSpecialFmt, "long"):  aaplLongBytes,
	fmt.Sprintf(termDHCPAAPLSpecialFmt, "short"): aaplShortBytes,
}

func extractDeviceInfoDHCP(di *base_msg.DeviceInfo) sentence.Sentence {
	s := sentence.New()

	vendor, vendorNormalized := DHCPVendorFromDeviceInfo(di)
	if vendor != "" {
		s.AddTermf(termDHCPVendorFmt, vendorNormalized)
	}

	for o := range di.Options {
		options := di.Options[o].ParamReqList

		dhcpOptions := fmt.Sprintf(termDHCPOptionsFmt, wordifyDHCPOptions(options))
		if dhcpOptions != emptyDHCPOptions {
			s.AddTerm(dhcpOptions)
		}

		// Apply "Special" DHCP patterns
		for term, byteseq := range specialDHCPOptions {
			if bytes.Equal(options, byteseq) {
				s.AddTerm(term)
			}
		}
	}

	return s
}

const ediDNSVersion = "1"

func extractDeviceInfoDNS(di *base_msg.DeviceInfo) sentence.Sentence {
	s := sentence.New()

	for r := range di.Request {
		for q := range di.Request[r].Request {
			query := di.Request[r].Request[q]
			vals := dnsINRequestRE.FindStringSubmatch(query)
			if len(vals) < 2 {
				continue
			}
			host := vals[1]

			for _, attr := range defs.DNSAttributes {
				if strings.Contains(host, attr) {
					l := strings.Replace(attr, ".", separator, -1)
					l = strings.Replace(l, "-", separator, -1)
					s.AddTermf(termDNSHitFmt, l)
				}
			}
		}
	}

	return s
}

const ediListenVersion = "1"

func extractDeviceInfoListen(di *base_msg.DeviceInfo) sentence.Sentence {
	s := sentence.New()

	for l := range di.Listen {

		if *di.Listen[l].Type == base_msg.EventListen_SSDP {
			// XXX We only want to add this if a device is
			// publishing, not using SEARCH or DISCOVER.
			if *di.Listen[l].Ssdp.Type == base_msg.EventSSDP_ALIVE {
				s.AddTerm("listen_ssdp")
			}
		} else if *di.Listen[l].Type == base_msg.EventListen_mDNS {
			// XXX We only want to add this if a device is
			// publishing, not querying.
			s.AddTerm("listen_mdns")
		}
	}

	return s
}

const ediScanVersion = "2"

func extractDeviceInfoScan(di *base_msg.DeviceInfo) sentence.Sentence {
	s := sentence.New()

	for _, scan := range di.Scan {
		for _, host := range scan.Hosts {
			for _, port := range host.Ports {
				portid := port.GetPortId()
				if portid > 10000 {
					continue
				}
				protocol := port.GetProtocol()
				// UDP ports in open|filtered or states other
				// than "open" are super ambiguous and it's
				// hard to know what to make of them, so skip
				// them.
				if protocol == "udp" && port.GetState() != "open" {
					continue
				}

				s.AddTermf("scan_port_%s_%d", protocol, portid)

				product := port.GetProduct()

				// We're going with the assumption that having
				// TCP product information is better than not
				// having it.  We may have to white/blacklist
				// specific ports in the future.
				if protocol == "tcp" && product != "" {
					s.AddTermf("scan_port_%s_%d_prod_%s",
						protocol, portid, smashMfg(product))
				}

				// For UDP we are less sure; but we know that
				// UDP Netbios NS can give a good OS hint, so we
				// absorb those if present:
				//
				//   On a Windows 10 system: "Microsoft Windows or Samba netbios-ns"
				//   On a Macos X system: "Apple Mac OS X netbios-ns"
				if portid == 137 && protocol == "udp" &&
					strings.Contains(product, "netbios-ns") {

					s.AddTermf("scan_port_%s_%d_prod_%s",
						protocol, portid, smashMfg(product))
				}

			}
		}
	}

	return s
}

// DHCPVendorFromDeviceInfo extracts the DHCP vendor name, as well as the
// normalized form of that name, from the DeviceInfo.
func DHCPVendorFromDeviceInfo(di *base_msg.DeviceInfo) (string, string) {
	vendor := ""
	vendorNorm := ""

	for o := range di.Options {
		vc := string(di.Options[o].VendorClassId)
		if len(vc) > 0 {
			vendor = vc
			vendorNorm, _ = NormalizeDHCPVendor(vendor)
		}
	}
	return vendor, vendorNorm
}

// BayesSentenceFromDeviceInfo extracts the bayesian sentence from a
// DeviceInfo.  It returns the version identifier for the sentence as well as
// the sentence itself.
func BayesSentenceFromDeviceInfo(ouiDB oui.OuiDB, di *base_msg.DeviceInfo) sentence.Sentence {
	s := sentence.New()

	if di.MacAddress == nil {
		return s
	}

	baseSentence := extractDeviceInfoBase(ouiDB, di)
	s.AddSentence(baseSentence)

	dhcpSentence := extractDeviceInfoDHCP(di)
	s.AddSentence(dhcpSentence)

	dnsSentence := extractDeviceInfoDNS(di)
	s.AddSentence(dnsSentence)

	listenSentence := extractDeviceInfoListen(di)
	s.AddSentence(listenSentence)

	scanSentence := extractDeviceInfoScan(di)
	s.AddSentence(scanSentence)

	return s
}

// CombinedVersion is the current combined sentence version string, of the form
// "010112"
const CombinedVersion = ediSeparatorVersion + ediBaseVersion + ediDHCPVersion + ediDNSVersion + ediListenVersion + ediScanVersion
