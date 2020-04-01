/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package extract

import (
	"bg/base_msg"
	"bg/cl-obs/defs"
	"bg/cl-obs/sentence"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/klauspost/oui"
	"github.com/stretchr/testify/require"
)

func TestWordifyDHCPOptions(t *testing.T) {
	assert := require.New(t)

	assert.Equal("", wordifyDHCPOptions([]byte{}))
	assert.Equal("1", wordifyDHCPOptions([]byte{0x01}))
	assert.Equal("1_2", wordifyDHCPOptions([]byte{0x01, 0x02}))
	assert.Equal("0_18_32_50", wordifyDHCPOptions([]byte{0x00, 0x12, 0x20, 0x32}))
}

// timeToProtobuf converts a Go timestamp into the equivalent Protobuf version
// XXX copied from aputil
func timeToProtobuf(gtime *time.Time) *base_msg.Timestamp {
	if gtime == nil {
		return nil
	}

	tmp := base_msg.Timestamp{
		Seconds: proto.Int64(gtime.Unix()),
		Nanos:   proto.Int32(int32(gtime.Nanosecond())),
	}
	return &tmp
}

// macStrToProtobuf translates a mac address string into a uint64 suitable
// for inserting into a protobuf.
// XXX copied from aputil
func macStrToProtobuf(macstr string) *uint64 {
	var rval uint64

	if a, err := net.ParseMAC(macstr); err == nil {
		hwaddr := make([]byte, 8)
		hwaddr[0] = 0
		hwaddr[1] = 0
		copy(hwaddr[2:], a)
		rval = binary.BigEndian.Uint64(hwaddr)
	}
	return &rval
}

func mockDeviceInfo(mac string) *base_msg.DeviceInfo {
	t := time.Now()
	return &base_msg.DeviceInfo{
		Created:    timeToProtobuf(&t),
		Updated:    timeToProtobuf(&t),
		MacAddress: macStrToProtobuf(mac),
		DnsName:    proto.String(""),
		DhcpName:   proto.String(""),
		Entity:     &base_msg.EventNetEntity{},
		Scan:       []*base_msg.EventNetScan{},
		Request:    []*base_msg.EventNetRequest{},
		Listen:     []*base_msg.EventListen{},
		Options:    []*base_msg.DHCPOptions{},
	}
}

func TestDHCPVendorFromDeviceInfo(t *testing.T) {
	assert := require.New(t)

	vend := "Hewlett-Packard OfficeJet"
	di := mockDeviceInfo("00:11:22:33:44:55")
	di.Options = []*base_msg.DHCPOptions{
		&base_msg.DHCPOptions{},
		&base_msg.DHCPOptions{
			ParamReqList:  []byte{},
			VendorClassId: []byte(vend),
		},
	}

	v, vNorm := DHCPVendorFromDeviceInfo(di)
	assert.Equal(vend, v)
	assert.Equal("hp", vNorm)

	di.Options = []*base_msg.DHCPOptions{
		&base_msg.DHCPOptions{},
	}
	v, vNorm = DHCPVendorFromDeviceInfo(di)
	assert.Equal("", v)
	assert.Equal("", vNorm)

	di.Options = []*base_msg.DHCPOptions{
		&base_msg.DHCPOptions{
			ParamReqList:  []byte{},
			VendorClassId: []byte("Fake Corporation, Inc."),
		},
	}
	v, vNorm = DHCPVendorFromDeviceInfo(di)
	assert.Equal("Fake Corporation, Inc.", v)
	assert.Equal(defs.UnknownDHCPVendor, vNorm)
}

const mockOUI = `
OUI/MA-L			Organization
company_id			Organization
				Address

58-CB-52   (hex)		Google Inc.
58CB52     (base 16)		Google Inc.
				1600 Amphitheatre Parkway
				Mountain View CA 94043
				US

`

func TestExtractBase(t *testing.T) {
	assert := require.New(t)

	rdr := strings.NewReader(mockOUI)
	ouiDB, err := oui.OpenStatic(rdr)
	assert.NoError(err)

	di := mockDeviceInfo("58:cb:52:44:55:66")
	exp := sentence.NewFromString("hw_mac_mfg_google_inc_ hw_mac_triple_58_cb_52_")

	sent := extractDeviceInfoBase(ouiDB, di)
	assert.Equal(exp.String(), sent.String())

	di = mockDeviceInfo("00:00:00:88:99:aa")
	exp = sentence.NewFromString("hw_mac_mfg_unknown_mfg_ hw_mac_triple_00_00_00_")
	sent = extractDeviceInfoBase(ouiDB, di)
	assert.Equal(exp.String(), sent.String())
}

func TestExtractDHCP(t *testing.T) {
	assert := require.New(t)

	di := mockDeviceInfo("00:11:22:33:44:55")
	assert.Equal("", extractDeviceInfoDHCP(di).String())

	di.Options = []*base_msg.DHCPOptions{
		&base_msg.DHCPOptions{},
		&base_msg.DHCPOptions{
			ParamReqList:  aaplLongBytes,
			VendorClassId: []byte("Fake Corp, Inc."),
		},
	}

	// XXX this seems like the dh_vendor_agent_ is bad.  Needs more investigation
	exp := sentence.NewFromString("dh_aapl_special_long_ dh_vendor_agent_-unknown-dhcp-vendor-_ dh_vendor_options_1_121_3_6_15_119_252_95_44_46_")
	assert.Equal(exp.String(), extractDeviceInfoDHCP(di).String())

	di.Options = []*base_msg.DHCPOptions{
		&base_msg.DHCPOptions{},
		&base_msg.DHCPOptions{
			ParamReqList:  []byte{0x1, 0x2, 0x3},
			VendorClassId: []byte("Hewlett-Packard OfficeJet"),
		},
	}
	exp = sentence.NewFromString("dh_vendor_agent_hp_ dh_vendor_options_1_2_3_")
	assert.Equal(exp.String(), extractDeviceInfoDHCP(di).String())
}

func TestExtractDNS(t *testing.T) {
	assert := require.New(t)

	googRequest := base_msg.EventNetRequest{
		Protocol: base_msg.Protocol_DNS.Enum(),
		Request:  []string{";android.clients.google.com.\tIN\t A"},
		Response: []string{"android.clients.google.com.\t148\tIN\tCNAME\tandroid.l.google.com.",
			"android.l.google.com.\t148\tIN\tA\t216.58.194.206"},
	}
	miscRequest := base_msg.EventNetRequest{
		Protocol: base_msg.Protocol_DNS.Enum(),
		Request:  []string{";nytimes.com.\tIN\t A"},
		Response: []string{"nytimes.com.\t499\tIN\tA\t151.101.65.164"},
	}

	di := mockDeviceInfo("00:11:22:33:44:55")
	assert.Equal("", extractDeviceInfoDNS(di).String())

	di.Request = []*base_msg.EventNetRequest{
		&googRequest,
		&miscRequest,
	}

	exp := sentence.NewFromString("dns_android_clients_google_com_")
	assert.Equal(exp.String(), extractDeviceInfoDNS(di).String())

	badRequest := base_msg.EventNetRequest{
		Protocol: base_msg.Protocol_DNS.Enum(),
		Request:  []string{";garbledrequest"},
		Response: []string{"nytimes.com.\t499\tIN\tA\t151.101.65.164"},
	}

	di.Request = []*base_msg.EventNetRequest{
		&badRequest,
		&googRequest,
	}
	exp = sentence.NewFromString("dns_android_clients_google_com_")
	assert.Equal(exp.String(), extractDeviceInfoDNS(di).String())
}

func TestExtractListen(t *testing.T) {
	assert := require.New(t)

	di := mockDeviceInfo("00:11:22:33:44:55")
	assert.Equal("", extractDeviceInfoListen(di).String())

	di.Listen = []*base_msg.EventListen{
		&base_msg.EventListen{
			Type: base_msg.EventListen_SSDP.Enum(),
			Ssdp: &base_msg.EventSSDP{
				Type: base_msg.EventSSDP_ALIVE.Enum(),
			},
		},
	}

	assert.Equal("listen_ssdp", extractDeviceInfoListen(di).String())

	di.Listen = []*base_msg.EventListen{
		&base_msg.EventListen{
			Type: base_msg.EventListen_SSDP.Enum(),
			Ssdp: &base_msg.EventSSDP{
				Type: base_msg.EventSSDP_DISCOVER.Enum(),
			},
		},
	}
	assert.Equal("", extractDeviceInfoListen(di).String())

	di.Listen = []*base_msg.EventListen{
		&base_msg.EventListen{
			Type: base_msg.EventListen_mDNS.Enum(),
		},
	}

	assert.Equal("listen_mdns", extractDeviceInfoListen(di).String())
}

func TestExtractScan(t *testing.T) {
	type testcase struct {
		testName string
		port     *base_msg.Port
		expected string
	}

	tests := []testcase{
		{
			testName: "tcp port info",
			port: &base_msg.Port{
				Protocol: proto.String("tcp"),
				PortId:   proto.Int32(22),
				State:    proto.String("open"),
				Product:  proto.String("openssh"),
			},
			expected: "scan_port_tcp_22 scan_port_tcp_22_prod_openssh",
		},
		{
			testName: "udp netbios info",
			port: &base_msg.Port{
				Protocol: proto.String("udp"),
				PortId:   proto.Int32(137),
				State:    proto.String("open"),
				Product:  proto.String("Apple Mac OS X netbios-ns"),
			},
			expected: "scan_port_udp_137 scan_port_udp_137_prod_apple_mac_os_x_netbios_ns",
		},
		{
			testName: "udp non-open ignored",
			port: &base_msg.Port{
				Protocol: proto.String("udp"),
				PortId:   proto.Int32(222),
				State:    proto.String("open|filtered"),
			},
			expected: "",
		},
		{
			testName: "above 10000 ignored",
			port: &base_msg.Port{
				Protocol: proto.String("tcp"),
				PortId:   proto.Int32(22222),
				State:    proto.String("open"),
			},
			expected: "",
		},
	}

	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			assert := require.New(t)

			di := mockDeviceInfo("00:11:22:33:44:55")
			assert.Equal("", extractDeviceInfoScan(di).String())

			di.Scan = []*base_msg.EventNetScan{
				&base_msg.EventNetScan{
					Hosts: []*base_msg.Host{
						&base_msg.Host{
							Ports: []*base_msg.Port{
								test.port,
							},
						},
					},
				},
			}

			exp := sentence.NewFromString(test.expected).String()
			assert.Equal(exp, extractDeviceInfoScan(di).String())
		})
	}
}
