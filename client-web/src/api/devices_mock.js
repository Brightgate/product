/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */
// vim :set ts=2 sw=2 sts=2 et :
export default {
  'DbgRequest': 'mock',
  'Devices': [
    {
      'HwAddr': 'c6:25:c3:ab:54:4b',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 0.45,
      'Ring': 'standard',
      'DHCPExpiry': '2018-09-13T13:33',
      'IPv4Addr': '192.168.52.48',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-09-12T15:13:16-07:00',
          'finish': '2018-09-12T15:13:21-07:00',
        },
        'vulnerability': {
          'start': '2018-09-12T15:13:02-07:00',
          'finish': '2018-09-12T15:13:16-07:00',
        },
      },
    },
    {
      'HwAddr': '40:4e:36:22:64:83',
      'Manufacturer': 'Samsung',
      'Model': 'Galaxy S5',
      'Kind': 'android',
      'Confidence': 0.69,
      'Ring': 'core',
      'DHCPExpiry': '2018-11-10T18:09',
      'IPv4Addr': '192.168.51.8',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-17T17:04:45-07:00',
          'finish': '2018-10-17T17:04:49-07:00',
        },
        'udp_ports': {
          'start': '2018-09-10T15:19:43-07:00',
          'finish': '2018-09-10T15:39:23-07:00',
        },
        'vulnerability': {
          'start': '2018-10-17T17:04:30-07:00',
          'finish': '2018-10-17T17:04:45-07:00',
        },
      },
    },
    {
      'HwAddr': '44:2a:60:dd:c5:8f',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 0.84,
      'Ring': 'standard',
      'HumanName': 'BrightgateSetup',
      'DHCPExpiry': '2018-11-14T04:47',
      'IPv4Addr': '192.168.52.48',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-30T08:05:19-07:00',
          'finish': '2018-10-30T08:05:23-07:00',
        },
        'udp_ports': {
          'start': '2018-10-30T04:39:48-07:00',
          'finish': '2018-10-30T04:43:35-07:00',
        },
        'vulnerability': {
          'start': '2018-11-05T11:55:21-08:00',
          'finish': '2018-11-05T11:55:30-08:00',
        },
      },
    },
    {
      'HwAddr': '6c:40:08:b4:fc:90',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 0.95,
      'Ring': 'core',
      'DHCPExpiry': '2018-10-30T11:58',
      'IPv4Addr': '192.168.51.12',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-29T13:58:57-07:00',
          'finish': '2018-10-29T13:59:01-07:00',
        },
        'udp_ports': {
          'start': '2018-09-19T14:40:34-07:00',
          'finish': '2018-09-19T14:40:39-07:00',
        },
        'vulnerability': {
          'start': '2018-10-29T13:58:42-07:00',
          'finish': '2018-10-29T13:58:57-07:00',
        },
      },
    },
    {
      'HwAddr': '98:10:e8:f1:b5:22',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 1,
      'Ring': 'standard',
      'DHCPExpiry': '2018-11-13T11:37',
      'IPv4Addr': '192.168.52.19',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-11-05T11:57:12-08:00',
          'finish': '2018-10-30T08:54:41-07:00',
        },
        'udp_ports': {
          'start': '2018-10-30T07:57:58-07:00',
          'finish': '2018-10-30T08:01:27-07:00',
        },
        'vulnerability': {
          'start': '2018-11-13T10:19:50-08:00',
          'finish': '2018-11-05T11:55:21-08:00',
        },
      },
      'Vulnerabilities': {
        'CVE-2018-6789': {
          'first_detected': '2018-07-25T11:32:13-07:00',
          'latest_detected': '2018-07-26T16:21:19-07:00',
          'active': false,
          'details': '',
        },
      },
    },
    {
      'HwAddr': 'e0:e6:2e:45:64:96',
      'Manufacturer': 'Samsung',
      'Model': 'Galaxy S5',
      'Kind': 'android',
      'Confidence': 0.39,
      'Ring': 'core',
      'DHCPExpiry': '2018-10-24T02:21',
      'IPv4Addr': '192.168.51.21',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-23T13:31:19-07:00',
          'finish': '2018-10-23T13:31:23-07:00',
        },
        'vulnerability': {
          'start': '2018-10-23T13:21:18-07:00',
          'finish': '2018-10-23T13:21:26-07:00',
        },
      },
    },
    {
      'HwAddr': '60:90:84:a0:00:02',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 0.8,
      'Ring': 'standard',
      'DHCPExpiry': '2018-11-08T17:59',
      'IPv4Addr': '192.168.52.27',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-09-12T11:25:31-07:00',
          'finish': '2018-09-12T11:25:47-07:00',
        },
        'udp_ports': {
          'start': '2018-09-12T12:27:15-07:00',
          'finish': '2018-09-12T12:27:19-07:00',
        },
        'vulnerability': {
          'start': '2018-09-12T11:30:22-07:00',
          'finish': '2018-09-12T11:35:41-07:00',
        },
      },
      'Vulnerabilities': {
        'defaultpassword': {
          'first_detected': '2018-09-11T17:33:07-07:00',
          'latest_detected': '2018-09-12T11:35:41-07:00',
          'active': false,
          'details': '',
        },
      },
    },
    {
      'HwAddr': '60:90:84:a0:00:14',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 0.8,
      'Ring': 'standard',
      'HumanName': 'bgdemo-b',
      'DNSName': 'bgdemo-b',
      'DHCPExpiry': '2018-09-11T14:19',
      'IPv4Addr': '192.168.52.12',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-09-10T16:53:25-07:00',
          'finish': '2018-09-10T16:53:29-07:00',
        },
        'vulnerability': {
          'start': '2018-09-10T16:52:50-07:00',
          'finish': '2018-09-10T16:53:04-07:00',
        },
      },
    },
    {
      'HwAddr': 'd4:dc:cd:f3:59:f0',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 1,
      'Ring': 'standard',
      'DHCPExpiry': '2018-11-13T21:04',
      'IPv4Addr': '192.168.52.13',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-25T10:32:12-07:00',
          'finish': '2018-10-25T10:32:17-07:00',
        },
        'udp_ports': {
          'start': '2018-10-25T09:26:37-07:00',
          'finish': '2018-10-25T09:26:41-07:00',
        },
        'vulnerability': {
          'start': '2018-10-25T10:28:21-07:00',
          'finish': '2018-10-25T10:28:27-07:00',
        },
      },
    },
    {
      'HwAddr': 'b8:27:eb:55:67:80',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 0.65,
      'Ring': 'standard',
      'HumanName': 'ap1',
      'DNSName': 'ap1',
      'DHCPExpiry': '2018-11-01T00:23',
      'IPv4Addr': '192.168.52.32',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-08-14T06:18:37-07:00',
          'finish': '2018-08-14T06:18:54-07:00',
        },
        'udp_ports': {
          'start': '2018-08-14T13:25:32-07:00',
          'finish': '2018-08-14T13:25:37-07:00',
        },
        'vulnerability': {
          'start': '2018-08-14T13:06:55-07:00',
          'finish': '2018-08-14T13:07:10-07:00',
        },
      },
      'Vulnerabilities': {
        'defaultpassword': {
          'first_detected': '2018-08-02T11:16:31-07:00',
          'latest_detected': '2018-08-03T09:15:49-07:00',
          'active': false,
          'details': '',
        },
      },
    },
    {
      'HwAddr': '10:dd:b1:c9:02:d1',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 0.86,
      'Ring': 'standard',
      'HumanName': 'Danek',
      'DHCPExpiry': '2018-11-14T17:53',
      'IPv4Addr': '192.168.52.39',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-30T09:23:52-07:00',
          'finish': '2018-10-30T09:24:32-07:00',
        },
        'udp_ports': {
          'start': '2018-10-30T08:50:26-07:00',
          'finish': '2018-10-30T08:53:32-07:00',
        },
        'vulnerability': {
          'start': '2018-11-05T11:55:20-08:00',
          'finish': '2018-11-05T11:55:30-08:00',
        },
      },
    },
    {
      'HwAddr': '60:90:84:a0:00:0e',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 0.8,
      'Ring': 'standard',
      'HumanName': 'bgdemo-a',
      'DNSName': 'bgdemo-a',
      'DHCPExpiry': '2018-10-31T18:53',
      'IPv4Addr': '192.168.52.17',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-09-13T16:12:39-07:00',
          'finish': '2018-09-13T16:12:56-07:00',
        },
        'udp_ports': {
          'start': '2018-09-13T16:40:37-07:00',
          'finish': '2018-09-13T16:40:41-07:00',
        },
        'vulnerability': {
          'start': '2018-09-13T13:55:54-07:00',
          'finish': '2018-09-13T14:07:16-07:00',
        },
      },
      'Vulnerabilities': {
        'defaultpassword': {
          'first_detected': '2018-09-28T11:12:54-07:00',
          'latest_detected': '2018-09-28T12:59:55-07:00',
          'active': true,
          'details': '',
        },
      },
    },
    {
      'HwAddr': '68:5b:35:88:8d:cb',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 1,
      'Ring': 'standard',
      'HumanName': 'moose',
      'DNSName': 'moose',
      'DHCPExpiry': 'static',
      'IPv4Addr': '192.168.52.21',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-11-05T11:57:12-08:00',
          'finish': '2018-10-29T17:12:22-07:00',
        },
        'udp_ports': {
          'start': '2018-10-29T16:32:08-07:00',
          'finish': '2018-10-29T16:35:39-07:00',
        },
        'vulnerability': {
          'start': '2018-11-13T10:19:40-08:00',
          'finish': '2018-11-13T10:19:48-08:00',
        },
      },
    },
    {
      'HwAddr': 'b8:27:eb:ae:b1:7f',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 0.95,
      'Ring': 'standard',
      'HumanName': 'dp-pi',
      'DNSName': 'dp-pi',
      'DHCPExpiry': 'static',
      'IPv4Addr': '192.168.52.52',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-30T08:55:18-07:00',
          'finish': '2018-10-30T08:55:41-07:00',
        },
        'udp_ports': {
          'start': '2018-10-30T08:47:54-07:00',
          'finish': '2018-10-30T06:53:26-07:00',
        },
        'vulnerability': {
          'start': '2018-11-05T11:55:30-08:00',
          'finish': '2018-11-05T11:55:38-08:00',
        },
      },
    },
    {
      'HwAddr': 'b8:27:eb:d9:a7:a7',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 1,
      'Ring': 'standard',
      'HumanName': 'nilsap',
      'DNSName': 'nilsap',
      'DHCPExpiry': 'static',
      'IPv4Addr': '192.168.52.20',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-11-05T11:57:12-08:00',
          'finish': '2018-10-30T08:55:18-07:00',
        },
        'udp_ports': {
          'start': '2018-10-30T09:27:05-07:00',
          'finish': '2018-10-30T07:41:23-07:00',
        },
        'vulnerability': {
          'start': '2018-11-05T11:55:12-08:00',
          'finish': '2018-11-05T11:55:22-08:00',
        },
      },
    },
    {
      'HwAddr': '08:00:27:d5:92:83',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 0.81,
      'Ring': 'standard',
      'HumanName': 'island',
      'DNSName': 'island',
      'DHCPExpiry': '2018-11-10T22:53',
      'IPv4Addr': '192.168.52.53',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-09T12:05:33-07:00',
          'finish': '2018-10-09T12:05:37-07:00',
        },
        'udp_ports': {
          'start': '2018-10-05T14:52:22-07:00',
          'finish': '2018-10-05T15:11:56-07:00',
        },
        'vulnerability': {
          'start': '2018-11-13T09:41:49-08:00',
          'finish': '2018-11-13T09:41:59-08:00',
        },
      },
    },
    {
      'HwAddr': '32:75:ae:1b:4e:c2',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 0.75,
      'Ring': 'standard',
      'HumanName': 'nilswrt',
      'DNSName': 'nilswrt',
      'DHCPExpiry': '2018-09-18T11:40',
      'IPv4Addr': '192.168.52.17',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-09-17T12:38:39-07:00',
          'finish': '2018-09-17T12:38:43-07:00',
        },
        'udp_ports': {
          'start': '2018-09-11T16:29:25-07:00',
          'finish': '2018-09-11T16:29:29-07:00',
        },
        'vulnerability': {
          'start': '2018-09-17T12:38:23-07:00',
          'finish': '2018-09-17T12:38:39-07:00',
        },
      },
      'Vulnerabilities': {
        'defaultpassword': {
          'first_detected': '2018-08-30T18:29:45-07:00',
          'latest_detected': '2018-09-11T07:07:22-07:00',
          'active': false,
          'details': '',
        },
      },
    },
    {
      'HwAddr': 'b8:27:eb:ca:8d:b5',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 1,
      'Ring': 'internal',
      'HumanName': 'sat-conference',
      'DNSName': 'sat-conference',
      'DHCPExpiry': '2018-11-14T07:12',
      'IPv4Addr': '192.168.56.3',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-08-03T13:04:33-07:00',
          'finish': '2018-08-03T13:04:50-07:00',
        },
        'udp_ports': {
          'start': '2018-06-29T10:52:36-07:00',
          'finish': '2018-06-29T11:11:54-07:00',
        },
        'vulnerability': {
          'start': '2018-11-13T10:19:42-08:00',
          'finish': '2018-11-13T10:19:51-08:00',
        },
      },
      'Vulnerabilities': {
        'CVE-2018-6789': {
          'first_detected': '2018-04-25T16:07:54-07:00',
          'latest_detected': '2018-06-29T11:26:34-07:00',
          'active': false,
          'details': '',
        },
      },
    },
    {
      'HwAddr': '9e:ef:d5:fe:19:20',
      'Manufacturer': 'Xerox',
      'Model': 'Phaser',
      'Kind': 'printer',
      'Confidence': 0.24,
      'Ring': '',
      'DHCPExpiry': 'static',
      'IPv4Addr': '',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-09-12T08:48:20-07:00',
          'finish': '2018-09-12T08:48:44-07:00',
        },
        'udp_ports': {
          'start': '2018-09-12T08:37:35-07:00',
          'finish': '2018-09-12T08:46:10-07:00',
        },
        'vulnerability': {
          'start': '2018-09-12T06:19:56-07:00',
          'finish': '2018-09-12T06:44:52-07:00',
        },
      },
    },
    {
      'HwAddr': '00:0c:e7:11:22:34',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 0.8,
      'Ring': 'standard',
      'DHCPExpiry': '2018-11-14T22:01',
      'IPv4Addr': '192.168.52.59',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-26T15:30:01-07:00',
          'finish': '2018-10-26T15:30:05-07:00',
        },
        'udp_ports': {
          'start': '2018-10-26T14:24:31-07:00',
          'finish': '2018-10-26T14:44:34-07:00',
        },
        'vulnerability': {
          'start': '2018-10-26T13:42:53-07:00',
          'finish': '2018-10-26T13:48:11-07:00',
        },
      },
      'Vulnerabilities': {
        'defaultpassword': {
          'first_detected': '2018-09-27T14:58:35-07:00',
          'latest_detected': '2018-10-26T13:48:11-07:00',
          'active': true,
          'details': '',
        },
      },
    },
    {
      'HwAddr': '54:26:96:d3:51:69',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 0.57,
      'Ring': 'standard',
      'HumanName': 'Sushants-MBP',
      'DHCPExpiry': '2018-11-14T18:12',
      'IPv4Addr': '192.168.52.10',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-29T17:12:59-07:00',
          'finish': '2018-10-29T17:13:04-07:00',
        },
        'udp_ports': {
          'start': '2018-10-29T15:17:45-07:00',
          'finish': '2018-10-29T15:17:49-07:00',
        },
        'vulnerability': {
          'start': '2018-10-29T17:12:22-07:00',
          'finish': '2018-10-29T17:12:37-07:00',
        },
      },
    },
    {
      'HwAddr': '9c:4f:da:26:c4:17',
      'Manufacturer': 'Apple',
      'Model': 'iPhone',
      'Kind': 'ios',
      'Confidence': 1,
      'Ring': 'core',
      'DHCPExpiry': '2018-10-20T15:06',
      'IPv4Addr': '192.168.51.10',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-17T16:35:06-07:00',
          'finish': '2018-10-17T16:35:10-07:00',
        },
        'udp_ports': {
          'start': '2018-10-05T11:31:48-07:00',
          'finish': '2018-10-05T11:31:52-07:00',
        },
        'vulnerability': {
          'start': '2018-11-13T10:12:33-08:00',
          'finish': '2018-10-17T16:35:06-07:00',
        },
      },
    },
    {
      'HwAddr': '3c:18:a0:41:5f:68',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 1,
      'Ring': 'standard',
      'DHCPExpiry': '2018-10-12T13:23',
      'IPv4Addr': '192.168.52.41',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-11T18:40:49-07:00',
          'finish': '2018-10-11T18:41:40-07:00',
        },
        'udp_ports': {
          'start': '2018-10-11T22:26:30-07:00',
          'finish': '2018-10-11T22:26:34-07:00',
        },
        'vulnerability': {
          'start': '2018-11-13T09:42:01-08:00',
          'finish': '2018-10-11T22:26:25-07:00',
        },
      },
    },
    {
      'HwAddr': 'a0:cc:2b:c0:f1:3c',
      'Manufacturer': 'Samsung',
      'Model': 'Galaxy S5',
      'Kind': 'android',
      'Confidence': 0.97,
      'Ring': 'core',
      'DHCPExpiry': '2018-10-31T17:49',
      'IPv4Addr': '192.168.51.17',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-29T15:17:40-07:00',
          'finish': '2018-10-29T15:17:45-07:00',
        },
        'udp_ports': {
          'start': '2018-10-29T12:31:44-07:00',
          'finish': '2018-10-29T12:31:48-07:00',
        },
        'vulnerability': {
          'start': '2018-10-29T15:10:19-07:00',
          'finish': '2018-10-29T15:10:33-07:00',
        },
      },
    },
    {
      'HwAddr': '00:cd:fe:04:2a:88',
      'Manufacturer': 'Apple',
      'Model': 'iPhone',
      'Kind': 'ios',
      'Confidence': 1,
      'Ring': 'core',
      'DHCPExpiry': '2018-10-22T14:07',
      'IPv4Addr': '192.168.51.42',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-18T02:55:40-07:00',
          'finish': '2018-10-18T02:55:44-07:00',
        },
        'vulnerability': {
          'start': '2018-10-18T02:55:23-07:00',
          'finish': '2018-10-18T02:55:40-07:00',
        },
      },
    },
    {
      'HwAddr': 'a6:1c:90:ee:63:e8',
      'Manufacturer': 'Xerox',
      'Model': 'Phaser',
      'Kind': 'printer',
      'Confidence': 0.24,
      'Ring': 'standard',
      'DHCPExpiry': '2018-11-01T21:16',
      'IPv4Addr': '192.168.52.55',
      'Active': false,
      'Scans': {
        'vulnerability': {
          'start': '2018-11-13T10:19:21-08:00',
          'finish': '2018-11-13T10:19:33-08:00',
        },
      },
    },
    {
      'HwAddr': 'dc:bf:e9:83:31:20',
      'Manufacturer': 'Motorola',
      'Model': 'Moto Z',
      'Kind': 'android',
      'Confidence': 0.51,
      'Ring': 'core',
      'HumanName': 'android-6d72570e777e3050',
      'DHCPExpiry': '2018-11-14T03:56',
      'IPv4Addr': '192.168.51.18',
      'Active': true,
    },
    {
      'HwAddr': '60:f8:1d:d2:46:82',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 1,
      'Ring': 'standard',
      'HumanName': 'Daniels-MBP',
      'DHCPExpiry': '2018-11-13T18:02',
      'IPv4Addr': '192.168.52.22',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-29T13:45:40-07:00',
          'finish': '2018-10-29T13:45:44-07:00',
        },
        'udp_ports': {
          'start': '2018-10-24T15:52:54-07:00',
          'finish': '2018-10-24T15:52:58-07:00',
        },
        'vulnerability': {
          'start': '2018-10-29T13:45:11-07:00',
          'finish': '2018-10-29T13:45:26-07:00',
        },
      },
    },
    {
      'HwAddr': 'e4:9a:dc:d2:b6:81',
      'Manufacturer': 'Apple',
      'Model': 'iPhone',
      'Kind': 'ios',
      'Confidence': 1,
      'Ring': 'core',
      'DHCPExpiry': '2018-11-13T19:02',
      'IPv4Addr': '192.168.51.11',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-30T04:46:04-07:00',
          'finish': '2018-10-30T04:46:09-07:00',
        },
        'udp_ports': {
          'start': '2018-10-26T11:28:20-07:00',
          'finish': '2018-10-26T11:33:44-07:00',
        },
        'vulnerability': {
          'start': '2018-11-13T10:19:31-08:00',
          'finish': '2018-11-13T10:19:39-08:00',
        },
      },
    },
    {
      'HwAddr': '6c:40:08:b9:8d:b0',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 1,
      'Ring': 'standard',
      'HumanName': 'catbook',
      'DHCPExpiry': '2018-10-24T13:16',
      'IPv4Addr': '192.168.52.18',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-23T13:31:23-07:00',
          'finish': '2018-10-23T13:31:27-07:00',
        },
        'udp_ports': {
          'start': '2018-10-16T21:59:30-07:00',
          'finish': '2018-10-16T21:59:34-07:00',
        },
        'vulnerability': {
          'start': '2018-10-23T13:21:26-07:00',
          'finish': '2018-10-23T13:21:34-07:00',
        },
      },
    },
    {
      'HwAddr': '64:9a:be:da:b1:9a',
      'Manufacturer': 'Apple',
      'Model': 'iPhone',
      'Kind': 'ios',
      'Confidence': 1,
      'Ring': 'core',
      'HumanName': 'Nilss-iPhone',
      'DHCPExpiry': '2018-11-14T17:20',
      'IPv4Addr': '192.168.51.9',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-29T19:00:27-07:00',
          'finish': '2018-10-29T19:00:31-07:00',
        },
        'udp_ports': {
          'start': '2018-10-05T11:31:30-07:00',
          'finish': '2018-10-05T11:31:34-07:00',
        },
        'vulnerability': {
          'start': '2018-11-13T09:42:00-08:00',
          'finish': '2018-10-29T18:42:59-07:00',
        },
      },
    },
    {
      'HwAddr': 'b8:27:eb:db:ce:ec',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 0.97,
      'Ring': 'standard',
      'HumanName': 'bg-ea-pi',
      'DHCPExpiry': 'static',
      'IPv4Addr': '',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-30T08:54:41-07:00',
          'finish': '2018-10-30T08:55:01-07:00',
        },
        'udp_ports': {
          'start': '2018-10-30T09:05:25-07:00',
          'finish': '2018-10-30T07:24:52-07:00',
        },
        'vulnerability': {
          'start': '2018-11-05T11:55:22-08:00',
          'finish': '2018-11-05T11:55:33-08:00',
        },
      },
    },
    {
      'HwAddr': '10:ae:60:4b:5f:9e',
      'Manufacturer': 'Amazon',
      'Model': 'Fire TV',
      'Kind': 'media',
      'Confidence': 0.45,
      'Ring': 'standard',
      'DHCPExpiry': 'static',
      'IPv4Addr': '',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-01T02:30:26-07:00',
          'finish': '2018-10-01T02:30:30-07:00',
        },
        'vulnerability': {
          'start': '2018-10-01T02:30:11-07:00',
          'finish': '2018-10-01T02:30:26-07:00',
        },
      },
      'Vulnerabilities': {
        'defaultpassword': {
          'first_detected': '2018-09-22T15:41:43-07:00',
          'latest_detected': '2018-09-28T11:05:13-07:00',
          'active': false,
          'details': '',
        },
      },
    },
    {
      'HwAddr': '6c:40:08:ab:75:da',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 0.98,
      'Ring': 'standard',
      'HumanName': 'moosew',
      'DNSName': 'moosew',
      'DHCPExpiry': '2018-11-14T18:12',
      'IPv4Addr': '192.168.52.9',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-26T16:27:29-07:00',
          'finish': '2018-10-26T16:27:33-07:00',
        },
        'udp_ports': {
          'start': '2018-10-24T16:20:45-07:00',
          'finish': '2018-10-24T16:20:49-07:00',
        },
        'vulnerability': {
          'start': '2018-10-30T09:36:24-07:00',
          'finish': '2018-10-30T09:36:30-07:00',
        },
      },
    },
    {
      'HwAddr': 'a0:40:a0:53:71:73',
      'Manufacturer': 'Generic',
      'Model': 'ESP8266',
      'Kind': 'iot',
      'Confidence': 0.81,
      'Ring': 'standard',
      'DHCPExpiry': '2018-11-13T22:17',
      'IPv4Addr': '192.168.52.40',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-30T09:38:09-07:00',
          'finish': '2018-10-30T08:53:53-07:00',
        },
        'udp_ports': {
          'start': '2018-10-30T07:26:23-07:00',
          'finish': '2018-10-30T08:23:38-07:00',
        },
        'vulnerability': {
          'start': '2018-11-05T11:55:20-08:00',
          'finish': '2018-11-05T11:55:30-08:00',
        },
      },
    },
    {
      'HwAddr': 'b8:27:eb:64:ee:4f',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 0.8,
      'Ring': 'internal',
      'HumanName': 'sat-conservatory',
      'DNSName': 'sat-conservatory',
      'DHCPExpiry': '2018-11-13T21:20',
      'IPv4Addr': '192.168.56.4',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-06-12T10:26:15-07:00',
          'finish': '2018-06-12T10:28:34-07:00',
        },
        'udp_ports': {
          'start': '2018-06-12T09:28:56-07:00',
          'finish': '2018-06-12T10:26:10-07:00',
        },
        'vulnerability': {
          'start': '2018-11-13T10:19:21-08:00',
          'finish': '2018-11-13T10:19:33-08:00',
        },
      },
    },
    {
      'HwAddr': '0c:4d:e9:ce:bb:de',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 1,
      'Ring': 'core',
      'HumanName': 'catbook',
      'DHCPExpiry': '2018-10-12T01:21',
      'IPv4Addr': '192.168.51.58',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-11T05:39:48-07:00',
          'finish': '2018-10-11T05:39:52-07:00',
        },
        'vulnerability': {
          'start': '2018-10-11T05:16:52-07:00',
          'finish': '2018-10-11T05:17:07-07:00',
        },
      },
    },
    {
      'HwAddr': 'b8:27:eb:6d:4c:74',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 1,
      'Ring': 'standard',
      'HumanName': 'duvall-pi0',
      'DNSName': 'duvall-pi0',
      'DHCPExpiry': 'static',
      'IPv4Addr': '192.168.52.56',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-16T17:20:27-07:00',
          'finish': '2018-10-16T17:20:45-07:00',
        },
        'udp_ports': {
          'start': '2018-10-16T17:36:49-07:00',
          'finish': '2018-10-16T17:36:53-07:00',
        },
        'vulnerability': {
          'start': '2018-10-16T14:48:26-07:00',
          'finish': '2018-10-16T15:13:25-07:00',
        },
      },
    },
    {
      'HwAddr': '08:00:27:65:cb:9c',
      'Manufacturer': 'VirtualBox',
      'Model': 'VM',
      'Kind': 'computer',
      'Confidence': 0.99,
      'Ring': 'standard',
      'HumanName': 'kali',
      'DNSName': 'kali',
      'DHCPExpiry': '2018-10-16T09:29',
      'IPv4Addr': '192.168.52.17',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-15T13:33:47-07:00',
          'finish': '2018-10-15T13:33:51-07:00',
        },
        'udp_ports': {
          'start': '2018-10-12T13:52:12-07:00',
          'finish': '2018-10-12T13:52:17-07:00',
        },
        'vulnerability': {
          'start': '2018-10-15T13:33:27-07:00',
          'finish': '2018-10-15T13:33:42-07:00',
        },
      },
    },
    {
      'HwAddr': 'ac:bc:32:d1:79:0b',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 0.8,
      'Ring': 'standard',
      'HumanName': 'Danek',
      'DHCPExpiry': '2018-11-14T17:41',
      'IPv4Addr': '192.168.52.8',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-30T07:26:19-07:00',
          'finish': '2018-10-30T07:26:23-07:00',
        },
        'udp_ports': {
          'start': '2018-10-29T13:59:01-07:00',
          'finish': '2018-10-29T13:59:05-07:00',
        },
        'vulnerability': {
          'start': '2018-11-05T11:17:45-08:00',
          'finish': '2018-11-05T11:17:45-08:00',
        },
      },
    },
    {
      'HwAddr': '0c:4d:e9:b3:e4:19',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 1,
      'Ring': 'standard',
      'DHCPExpiry': '2018-11-14T17:48',
      'IPv4Addr': '192.168.52.17',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-11-05T11:57:12-08:00',
          'finish': '2018-10-29T18:13:39-07:00',
        },
        'udp_ports': {
          'start': '2018-10-29T16:28:23-07:00',
          'finish': '2018-10-29T16:32:08-07:00',
        },
        'vulnerability': {
          'start': '2018-11-05T11:55:12-08:00',
          'finish': '2018-11-05T11:55:19-08:00',
        },
      },
    },
    {
      'HwAddr': '60:90:84:80:00:02',
      'Manufacturer': 'Xerox',
      'Model': 'Phaser',
      'Kind': 'printer',
      'Confidence': 0.24,
      'Ring': 'standard',
      'DHCPExpiry': '2018-11-06T21:44',
      'IPv4Addr': '192.168.52.54',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-11-05T11:57:12-08:00',
          'finish': '0001-01-01T00:00:00Z',
        },
        'vulnerability': {
          'start': '2018-11-05T11:55:12-08:00',
          'finish': '2018-11-05T11:55:20-08:00',
        },
      },
    },
    {
      'HwAddr': 'ac:5f:3e:66:6b:90',
      'Manufacturer': 'Apple',
      'Model': 'Macbook',
      'Kind': 'computer',
      'Confidence': 0.92,
      'Ring': 'standard',
      'HumanName': 'Samsung-Galaxy-S7',
      'DHCPExpiry': '2018-11-14T18:23',
      'IPv4Addr': '192.168.52.18',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-29T19:07:18-07:00',
          'finish': '2018-10-29T19:07:22-07:00',
        },
        'udp_ports': {
          'start': '2018-10-29T14:34:19-07:00',
          'finish': '2018-10-29T15:31:09-07:00',
        },
        'vulnerability': {
          'start': '2018-10-29T19:07:03-07:00',
          'finish': '2018-10-29T19:07:18-07:00',
        },
      },
    },
    {
      'HwAddr': 'b8:27:eb:a7:a6:35',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 1,
      'Ring': 'standard',
      'HumanName': 'nils2',
      'DNSName': 'nils2',
      'DHCPExpiry': '2018-10-31T18:49',
      'IPv4Addr': '192.168.52.50',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-30T09:38:09-07:00',
          'finish': '2018-10-30T08:39:38-07:00',
        },
        'udp_ports': {
          'start': '2018-10-30T08:30:28-07:00',
          'finish': '2018-10-30T08:50:01-07:00',
        },
        'vulnerability': {
          'start': '2018-10-30T09:36:08-07:00',
          'finish': '2018-10-30T09:36:17-07:00',
        },
      },
    },
    {
      'HwAddr': '00:80:92:cf:9e:02',
      'Manufacturer': 'Generic',
      'Model': 'ESP8266',
      'Kind': 'iot',
      'Confidence': 0.81,
      'Ring': 'standard',
      'DHCPExpiry': '2018-09-11T13:43',
      'IPv4Addr': '192.168.52.49',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-09-10T18:04:24-07:00',
          'finish': '2018-09-10T18:31:23-07:00',
        },
        'udp_ports': {
          'start': '2018-09-10T18:34:54-07:00',
          'finish': '2018-09-10T18:34:58-07:00',
        },
        'vulnerability': {
          'start': '2018-09-10T17:56:51-07:00',
          'finish': '2018-09-07T17:34:51-07:00',
        },
      },
    },
    {
      'HwAddr': '88:79:7e:5c:b0:e8',
      'Manufacturer': 'Motorola',
      'Model': 'Moto Z',
      'Kind': 'android',
      'Confidence': 0.47,
      'Ring': 'core',
      'DHCPExpiry': '2018-10-19T02:23',
      'IPv4Addr': '192.168.51.12',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-17T23:49:14-07:00',
          'finish': '2018-10-17T23:49:18-07:00',
        },
        'udp_ports': {
          'start': '2018-10-17T00:45:32-07:00',
          'finish': '2018-10-17T00:45:36-07:00',
        },
        'vulnerability': {
          'start': '2018-10-17T23:49:06-07:00',
          'finish': '2018-10-17T23:49:13-07:00',
        },
      },
    },
    {
      'HwAddr': '9c:93:4e:2c:f7:2c',
      'Manufacturer': 'Xerox',
      'Model': 'Phaser',
      'Kind': 'printer',
      'Confidence': 1,
      'Ring': 'standard',
      'HumanName': 'XRX9C934E2CF72C',
      'DHCPExpiry': '2018-11-14T03:16',
      'IPv4Addr': '192.168.52.24',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-30T08:39:38-07:00',
          'finish': '2018-10-30T08:40:24-07:00',
        },
        'udp_ports': {
          'start': '2018-10-30T09:20:30-07:00',
          'finish': '2018-10-30T09:23:52-07:00',
        },
        'vulnerability': {
          'start': '2018-11-05T11:55:20-08:00',
          'finish': '2018-11-05T11:55:30-08:00',
        },
      },
      'Vulnerabilities': {
        'defaultpassword': {
          'first_detected': '2018-08-09T23:44:43-07:00',
          'latest_detected': '2018-10-25T07:53:01-07:00',
          'active': false,
          'details': '',
        },
      },
    },
    {
      'HwAddr': '00:0d:5d:05:f2:2d',
      'Manufacturer': 'Xerox',
      'Model': 'Phaser',
      'Kind': 'printer',
      'Confidence': 0.24,
      'Ring': 'standard',
      'DHCPExpiry': 'static',
      'IPv4Addr': '192.168.52.57',
      'Active': true,
    },
    {
      'HwAddr': '00:0d:5d:05:f1:d5',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 0.53,
      'Ring': 'standard',
      'DHCPExpiry': 'static',
      'IPv4Addr': '192.168.52.30',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-04-19T15:11:47-07:00',
          'finish': '2018-04-19T15:12:09-07:00',
        },
        'udp_ports': {
          'start': '2018-04-19T15:12:31-07:00',
          'finish': '2018-04-19T15:12:35-07:00',
        },
        'vulnerability': {
          'start': '2018-04-19T14:48:05-07:00',
          'finish': '2018-04-19T14:48:10-07:00',
        },
      },
    },
    {
      'HwAddr': '32:99:d2:24:7e:33',
      'Manufacturer': 'Xerox',
      'Model': 'Phaser',
      'Kind': 'printer',
      'Confidence': 0.24,
      'Ring': 'standard',
      'DHCPExpiry': '2018-09-13T14:39',
      'IPv4Addr': '192.168.52.33',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-09-12T15:14:14-07:00',
          'finish': '2018-09-12T15:14:18-07:00',
        },
        'vulnerability': {
          'start': '2018-11-13T10:19:42-08:00',
          'finish': '2018-11-13T10:19:50-08:00',
        },
      },
    },
    {
      'HwAddr': '90:2b:34:59:46:ae',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 0.83,
      'Ring': 'standard',
      'HumanName': 'seconion',
      'DHCPExpiry': '2018-11-14T17:28',
      'IPv4Addr': '192.168.52.51',
      'Active': true,
      'Scans': {
        'tcp_ports': {
          'start': '2018-10-30T09:24:33-07:00',
          'finish': '2018-10-30T09:27:05-07:00',
        },
        'udp_ports': {
          'start': '2018-10-30T07:41:23-07:00',
          'finish': '2018-10-30T08:38:38-07:00',
        },
        'vulnerability': {
          'start': '2018-11-13T10:19:33-08:00',
          'finish': '2018-11-13T10:19:42-08:00',
        },
      },
    },
    {
      'HwAddr': '00:0d:5d:05:f1:ff',
      'Manufacturer': 'Xerox',
      'Model': 'Phaser',
      'Kind': 'printer',
      'Confidence': 0.24,
      'Ring': 'standard',
      'DHCPExpiry': 'static',
      'IPv4Addr': '192.168.52.38',
      'Active': true,
      'Scans': {
        'vulnerability': {
          'start': '2018-11-13T10:12:33-08:00',
          'finish': '0001-01-01T00:00:00Z',
        },
      },
    },
    {
      'HwAddr': 'b8:27:eb:26:4b:3f',
      'Manufacturer': 'Raspberry Pi Foundation',
      'Model': 'Raspberry Pi 3',
      'Kind': 'computer',
      'Confidence': 1,
      'Ring': 'standard',
      'DHCPExpiry': '2018-09-18T10:21',
      'IPv4Addr': '192.168.52.9',
      'Active': false,
      'Scans': {
        'tcp_ports': {
          'start': '2018-09-17T11:36:15-07:00',
          'finish': '2018-09-17T11:36:19-07:00',
        },
        'vulnerability': {
          'start': '2018-11-13T10:19:39-08:00',
          'finish': '2018-11-13T10:19:47-08:00',
        },
      },
    },
  ],
};
