/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package defs

// Classification maps.

// Operating systems.

// UnknownOS represents the classification result that the OS is not known
const UnknownOS = "-unknown-os-"

// OSGenusMap holds a sparse identifier space to organize operating systems.
var OSGenusMap = map[int]string{
	0x00: UnknownOS,
	0x10: "Windows",
	0x20: "macOS",
	0x30: "iOS",
	0x34: "watchOS",
	0x36: "tvOS",
	0x38: "iPadOS",
	0x40: "Android",
	0x50: "Linux",
	0x60: "BSD",
	0x70: "Unix",
	0x80: "Embedded/RTOS",
}

// OSRevGenusMap is an inverted form of OSGenusMap, indexed by OS genus name
var OSRevGenusMap map[string]uint64

// OSSpeciesMap holds a sparse identifier space to organize operating
// systems on a more detailed level.
var OSSpeciesMap = map[int]string{
	0x0000: UnknownOS,
	0x1000: "Windows",
	0x1700: "Windows 7",
	0x1800: "Windows 8",
	0x1a00: "Windows 10",
	0x2000: "macOS",
	0x3000: "iOS",
	0x3400: "watchOS",
	0x3600: "tvOS",
	0x3800: "iPadOS",
	0x4000: "Android",
	0x4100: "Fire OS",
	0x5000: "Linux",
	0x5100: "Debian",
	0x5150: "Raspbian",
	0x5200: "Red Hat",
	0x5300: "Ubuntu",
	0x5400: "OpenWrt",
	0x5500: "Buildroot",
	0x6000: "BSD",
	0x6110: "FreeBSD",
	0x6120: "NetBSD",
	0x6130: "OpenBSD",
	0x6140: "OrbisOS",
	0x7000: "Unix",
	0x7100: "Solaris/Illumos",
	0x8000: "Embedded/RTOS",
	0x8100: "VxWorks",
	0x8200: "HP-Futuresmart",
	0x8300: "ZentriOS",
}

// OSRevSpeciesMap is an inverted form of OSRevSpeciesMap, indexed by OS species name
var OSRevSpeciesMap map[string]uint64

// Devices.

// UnknownDevice represents the classification result that the device is not known
const UnknownDevice = "-unknown-device-"

// DeviceGenusMap is a dense identifier space to organize device genera.
var DeviceGenusMap = map[int]string{
	0x00: UnknownDevice,
	0x01: "Amazon Kindle",
	0x02: "Android Phone",
	0x03: "Apple iPhone/iPad",
	0x04: "Apple Macintosh",
	0x05: "Belkin Wemo",
	0x06: "Google Home",
	0x07: "Google Pixel",
	0x08: "Nest Sensor",
	0x09: "Raspberry Pi",
	0x0a: "Roku Streaming Media Player",
	0x0b: "Sonos Wireless Sound Device",
	0x0c: "Ubiquiti AP",
	0x0d: "Ubiquiti mFi",
	0x0e: "Windows PC",
	0x0f: "Xerox Printer",
	0x10: "Apple iPad",
	0x11: "Apple Watch",
	0x12: "Microsoft Surface",
	0x13: "Amazon Echo",
	0x14: "TiVo DVR",
	0x15: "Linux/Unix Server",
	0x16: "Sony PlayStation",
	0x17: "Brightgate Appliance",
	0x18: "Apple TV",
	0x19: "Google Chromecast",
	0x1a: "Linux/Unix VM",
	0x1b: "Windows VM",
	0x1c: "macOS VM",
	0x1d: "HP Printer",
	0x1e: "Hackintosh",
	0x1f: "Apple AirPort",
	0x20: "OBi100/200",
	0x21: "Dreambox",
	0x22: "Wyze Cam",
}

// DeviceRevMap is an inverted form of DeviceGenusMap, indexed by device genus name
var DeviceRevMap map[string]uint64

// Manufacturers.

// UnknownMfg represents the extraction/classification result that the manufacturer is not known
const UnknownMfg = "-unknown-mfg-"

// MfgAliasMap maps short-form 3rd party manufacturer names to the IEEE OUI
// forms.  (Primarily for IEEE OUI mappings.)
var MfgAliasMap = map[string][]string{
	"Amazon":                      {"Amazon Technologies Inc.", "Amazon.com, LLC"},
	"Apple":                       {"Apple, Inc."},
	"Arcadyan":                    {"Arcadyan Corporation"},
	"Arris":                       {"ARRIS Group, Inc."},
	"Asix Electronics":            {"ASIX ELECTRONICS CORP."},
	"Asus":                        {"ASUSTek COMPUTER INC."},
	"AzureWave":                   {"AzureWave Technology Inc."},
	"Barnes&Noble":                {},
	"Belkin":                      {"Belkin International Inc."},
	"BizLink":                     {"BizLink (Kunshan) Co.,Ltd"},
	"Bose":                        {"Bose Corporation"},
	"Brightgate":                  {"DSSD Inc"},
	"Brother":                     {"Brother industries, LTD."},
	"Cameo":                       {"Cameo Communications, INC."},
	"Canon":                       {"CANON INC."},
	"CASwell":                     {"CASwell INC."},
	"CE Link":                     {"CE LINK LIMITED"},
	"Chamberlain":                 {"The Chamberlain Group, Inc"},
	"Chongqing Fugui Electronics": {"CHONGQING FUGUI ELECTRONICS CO.,LTD."},
	"Cloud Network":               {"Cloud Network Technology (Samoa) Limited"},
	"Compal":                      {"Compal Communications, Inc.", "COMPAL INFORMATION (KUNSHAN) CO., LTD. "},
	"Dell":                        {"Dell Inc."},
	"D&M Holdings":                {"D&M Holdings Inc."},
	"Edimax":                      {"Edimax Technology Co. Ltd."},
	"Eero":                        {"eero inc."},
	"Espressif":                   {"Espressif Inc."},
	"FN-Link":                     {"FN-LINK TECHNOLOGY LIMITED"},
	"Foxconn":                     {"Hon Hai Precision Ind. Co.,Ltd."},
	"GainSpan":                    {"GainSpan Corp."},
	"Google":                      {"Google, Inc."},
	"Giga-Byte":                   {"GIGA-BYTE TECHNOLOGY CO.,LTD."},
	"Grandstream":                 {"Grandstream Networks, Inc."},
	"Hikvision":                   {"Hangzhou Hikvision Digital Technology Co.,Ltd."},
	"HP":                          {"Hewlett Packard"},
	"HTC":                         {"HTC Corporation"},
	"Huawei":                      {"HUAWEI TECHNOLOGIES CO.,LTD"},
	"Hui Zhou Gaoshengda":         {"Hui Zhou Gaoshengda Technology Co.,LTD"},
	"Humax":                       {"HUMAX Co., Ltd."},
	"IEEE":                        {"IEEE Registration Authority"},
	"Intel":                       {"Intel Corporate"},
	"Iskra Transmission":          {"Iskra Transmission d.d."},
	"JK Microsystems":             {"JK microsystems, Inc."},
	"Kyocera":                     {"KYOCERA Display Corporation"},
	"Lenovo":                      {},
	"LG":                          {"LG Innotek", "LG Electronics (Mobile Communications)"},
	"LiteOn":                      {"Liteon Technology Corporation"},
	"Logitech":                    {},
	"Luxshare":                    {"Luxshare Precision Industry Company Limited"},
	"MediaTek":                    {"MediaTek Inc."},
	"Microsoft":                   {"Microsoft Corporation"},
	"Mitel":                       {"MITEL CORPORATION"},
	"Motorola":                    {},
	"Motorola Mobility":           {"Motorola Mobility LLC, a Lenovo Company"},
	"MMB Research":                {"MMB Research Inc."},
	"Murata":                      {"Murata Manufacturing Co., Ltd."},
	"NEC":                         {"NEC Platforms, Ltd"},
	"Nest":                        {"Nest Labs Inc."},
	"Netgear":                     {"NETGEAR"},
	"Nintendo":                    {"Nintendo Co.,Ltd", "Nintendo Co., Ltd."},
	"Nvidia":                      {"NVIDIA"},
	"Obihai Technology":           {"Obihai Technology, Inc."},
	"OnePlus Technology":          {"OnePlus Technology (Shenzhen) Co., Ltd", "OnePlus Tech (Shenzhen) Ltd"},
	"Onkyo":                       {"Onkyo Corporation"},
	"Panda Wireless":              {"Panda Wireless, Inc."},
	"PATECH":                      {},
	"PCS Systemtechnik":           {"PCS Systemtechnik GmbH"},
	"Pegatron":                    {"PEGATRON CORPORATION"},
	"Philips Lighting":            {"Philips Lighting BV"},
	"Polycom":                     {},
	"Private":                     {},
	"Ralink Technology":           {"Ralink Technology, Corp."},
	"Raritan Computer":            {"Raritan Computer, Inc"},
	"Realtek":                     {"REALTEK SEMICONDUCTOR CORP."},
	"Raspberry Pi Foundation":     {"Raspberry Pi Foundation"},
	"Ring":                        {},
	"Rivet Networks":              {},
	"Roku":                        {"Roku, Inc.", "Roku, Inc"},
	"Salcomp":                     {"Salcomp (Shenzhen) CO., LTD."},
	"Samsung":                     {"SAMSUNG ELECTRO-MECHANICS(THAILAND)", "Samsung Electronics Co.,Ltd"},
	"Seiko Epson":                 {"Seiko Epson Corporation"},
	"Shenzhen Ogemray":            {"Shenzhen Ogemray Technology Co., Ltd."},
	"Sichuan AI-Link":             {"Sichuan AI-Link Technology Co., Ltd."},
	"Silex Technology":            {"Silex Technology, Inc.", "silex technology, Inc."},
	"Silicondust":                 {"Silicondust Engineering Ltd"},
	"Snap AV":                     {},
	"Sonos":                       {"Sonos, Inc."},
	"Sophos":                      {"Sophos Ltd"},
	"Sony":                        {"Sony Corporation", "Sony Interactive Entertainment Inc."},
	"Synology":                    {"Synology Incorporated"},
	"Taiyo Yuden":                 {"Taiyo Yuden Co., Ltd."},
	"TCL":                         {"Shenzhen TCL New Technology Co., Ltd"},
	"TCT Mobile":                  {"TCT mobile ltd"},
	"Technicolor CH":              {"Technicolor CH USA Inc."},
	"Texas Instruments":           {},
	"TiVo":                        {"TiVo"},
	"TP-Link":                     {"TP-LINK TECHNOLOGIES CO.,LTD."},
	"Ubiquiti":                    {"Ubiquiti Networks Inc."},
	"Valve":                       {"Valve Corporation"},
	"Vizio":                       {"Vizio, Inc"},
	"VMware":                      {"VMware, Inc."},
	"Wistron":                     {"Wistron Corporation", "Wistron Neweb Corporation", "Wistron InfoComm(Kunshan)Co.,Ltd."},
	"Xerox":                       {"Xerox Corporation", "XEROX CORPORATION"},
	"XN Systems":                  {"xn systems"},
	"Zentri":                      {"Zentri Pty Ltd"},
}

// MfgReverseAliasMap is an inverted form of MfgAliasMap, mapping IEEE OUI manufacturer names to our short-form.
var MfgReverseAliasMap map[string]string

// DHCP Vendor strings.

// UnknownDHCPVendor represents the extraction result that the DHCP vendor is not recognized
const UnknownDHCPVendor = "-unknown-dhcp-vendor-"

// DHCPVendors is a list of known short-form DHCP vendor names; see also DHCPVendorPatterns
var DHCPVendors = []string{
	UnknownDHCPVendor,
	"aastra_ip_phone",
	"android",
	"araknis",
	"canon",
	"cytracom",
	"dhcpv4",
	"google",
	"linux",
	"microsoft",
	"hp",
	"polycom",
	"rabbit_2000",
	"solaris",
	"sony_ps4",
	"ubiquiti",
	"udhcp",
	"udhcpc",
	"xerox",
}

// DHCPVendorPatterns maps regexp patterns to short-form DHCP vendor names
var DHCPVendorPatterns = map[string]string{
	UnknownDHCPVendor:            UnknownDHCPVendor, // Shouldn't match any agents.
	"^AastraIPPhone":             "aastra_ip_phone",
	"^android-dhcp-":             "android",
	"^Araknis":                   "araknis",
	"^Canon":                     "canon",
	"^Cytracom ":                 "cytracom",
	"^DHCPV4C":                   "dhcpv4",
	"^dhcpcd[- ]":                "dhcpcd",
	"^EDGEMARC":                  "edgemark",
	"^Grandstream":               "grandstream",
	"^GoogleWifi":                "google",
	"^Linux":                     "linux",
	"^HP LaserJet":               "hp",
	"^HP Printer":                "hp",
	"^Hewlett-Packard OfficeJet": "hp",
	"^Hewlett-Packard JetDirect": "hp",
	"^Mfg=Hewlett Packard":       "hp",
	"^Mfg=Hewlett-Packard":       "hp",
	"^HUAWEI:android":            "huawei_android",
	"^MSFT ":                     "microsoft",
	"^Polycom-":                  "polycom",
	"^PS4":                       "sony_ps4",
	"^Rabbit2000-TCPIP":          "rabbit_2000",
	"^SUNW.i86pc":                "solaris",
	"^ubnt":                      "ubiquiti",
	"^udhcpc":                    "udhcpc",
	"^udhcp":                     "udhcp",
	"^Mfg=Xerox":                 "xerox",
	"^Mfg=FujiXerox":             "xerox",
	"^XEROX":                     "xerox",
}

// DNSAttributes contains a list of notable DNS queries.  Queries not present
// in this list are ignored when a classifier is being trained from incoming
// DeviceInfos.  If you add or delete more than one or two, you must bump the
// DNS extraction version.
//
// Keep ordered with: sort -t . -k4,4 -k3,3 -k2,2 -k1,1
var DNSAttributes = []string{
	"api.amazon.com",
	"device-messaging-na.amazon.com",
	"device-metrics-us.amazon.com",
	"ntp-g7g.amazon.com",
	"softwareupdates.amazon.com",
	"todo-ta-g7g.amazon.com",
	"captive.apple.com",
	"configuration.apple.com",
	"gs.apple.com",
	"gs-loc.apple.com",
	"guzzoni.apple.com",
	"iadsdk.apple.com",
	"iphone-ld.apple.com",
	"lcdn-locator.apple.com",
	"ls.apple.com",
	"mesu.apple.com",
	"push.apple.com",
	"time.apple.com",
	"time-ios.apple.com",
	"xp.apple.com",
	"time1.google.com",
	"play.googleapis.com",
	"connectivitycheck.gstatic.com",
	"ccc.hpeprint.com",
	"availability.icloud.com",
	"-btmmdns.icloud.com",
	"-caldav.icloud.com",
	"-calendars.icloud.com",
	"-ckdatabase.icloud.com",
	"-contacts.icloud.com",
	"gateway.icloud.com",
	"-keyvalueservice.icloud.com",
	"-quota.icloud.com",
	"setup.icloud.com",
	"ipv6.msftconnecttest.com",
	"www.msftconnecttest.com",
	"devices.nest.com",
	"frontdoor.nest.com",
	"time.nest.com",
	"sonos.pandora.com",
	"update-firmware.sonos.com",
	"sr.symcd.com",
	"daisy.ubuntu.com",
	"ntp.ubuntu.com",
	"time.windows.com",
	"download.windowsupdate.com",
	"heartbeat.xwemo.com",
	// "weather.nest.com",		// Also used by phone app.
	// "clients3.google.com",	// Also used by Sonos.
	"heartbeat.lswf.net",
	"dl.playstation.net",
	"api.xbcs.net",
	"nat.xbcs.net",
	"archive.raspberrypi.org",
	"mirrordirector.raspbian.org",
	"time-osx.g.aaplimg.com.",
	"api-glb-sjc.smoot.apple.com",
	"android.clients.google.com",
	"settings-win.data.microsoft.com",
	"displaycatalog.mp.microsoft.com",
	"transport.home.nest.com",
	"feature-config.sslauth.sonos.com",
	"account.ws.sonos.com",
	"service-catalog.ws.sonos.com",
	"remserv11.support.xerox.com",
	"epdg.epc.att.net",
	"sentitlement2.mobile.att.net",
	"vvm.mobile.att.net",
	"np.communication.playstation.net",
	"np.community.playstation.net",
	"ps4.update.playstation.net",
	"android.pool.ntp.org",
	"debian.pool.ntp.org",
	"openwrt.pool.ntp.org",
	"smartos.pool.ntp.org",
	"sonostime.pool.ntp.org",
	"ubnt.pool.ntp.org",
}

func init() {
	OSRevGenusMap = make(map[string]uint64)

	for k, v := range OSGenusMap {
		OSRevGenusMap[v] = uint64(k)
	}

	OSRevSpeciesMap = make(map[string]uint64)

	for k, v := range OSSpeciesMap {
		OSRevSpeciesMap[v] = uint64(k)
	}

	DeviceRevMap = make(map[string]uint64)

	for k, v := range DeviceGenusMap {
		DeviceRevMap[v] = uint64(k)
	}

	MfgReverseAliasMap = make(map[string]string)

	for s, al := range MfgAliasMap {
		MfgReverseAliasMap[s] = s

		for a := range al {
			MfgReverseAliasMap[al[a]] = s
		}
	}

	// XXX this seems like a weird way to assemble things.
	// Revise the structure of the above.
	for _, v := range DHCPVendorPatterns {
		DHCPVendors = append(DHCPVendors, v)
	}
}

