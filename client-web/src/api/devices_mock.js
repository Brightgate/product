/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

// vim :set ts=2 sw=2 sts=2 et :

// To generate this file, capture an example response using chrome developer
// tools, then put it through 'jq --sort-keys .' to format it.
// Anonymize by wiping out the first three octets of the mac addrs
// s/hwAddr": "[0-9a-f][0-9a-f]:[0-9a-f][0-9a-f]:[0-9a-f][0-9a-f]/hwAddr": "00:01:02/g
//
// and change some machine names.

/* eslint-disable quotes, comma-dangle */
export default [
  {
    "active": false,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000002",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-25T17:01:02Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:08:2c:5e",
    "ipv4Addr": "192.168.229.11",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-24T18:23:19Z",
        "start": "2019-06-24T18:23:13Z"
      },
      "udp": {
        "finish": "2019-05-02T21:57:07Z",
        "start": "2019-05-02T21:57:03Z"
      },
      "vuln": {
        "finish": "2019-06-24T18:02:41Z",
        "start": "2019-06-24T18:02:34Z"
      }
    },
    "wireless": true,
    "signalStrength": -45,
    "lastActivity": "2019-06-26T19:10:25Z",
  },
  {
    "active": true,
    "dhcpExpiry": "static",
    "dhcpName": "a564029e-71bc-4dd5-91e8-a6fe44c02ac5",
    "displayName": "creed-wrt",
    "dnsName": "creed-wrt",
    "hwAddr": "00:01:02:a0:00:08",
    "ipv4Addr": "192.168.229.48",
    "ring": "standard",
    "devID": {
      "osGenus": "Linux",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:06:20Z",
        "start": "2019-06-25T19:06:04Z"
      },
      "udp": {
        "finish": "2019-06-25T17:00:33Z",
        "start": "2019-06-25T16:03:19Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:02:32Z",
        "start": "2019-06-25T19:02:24Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": true,
    "dhcpExpiry": "2019-06-26T08:30:58Z",
    "dhcpName": "a1fa1e2d-67c1-4a6b-b70b-5469635aa215",
    "displayName": "a1fa1e2d-67c1-4a6b-b70b-5469635aa215",
    "hwAddr": "00:01:02:a0:00:27",
    "ipv4Addr": "192.168.229.80",
    "ring": "standard",
    "devID": {
      "osGenus": "Linux",
      "deviceGenus": "Brightgate Appliance",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:06:55Z",
        "start": "2019-06-25T19:06:40Z"
      },
      "udp": {
        "finish": "2019-06-25T14:43:06Z",
        "start": "2019-06-25T18:33:05Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:03:06Z",
        "start": "2019-06-25T19:02:58Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": true,
    "dhcpExpiry": "2019-06-26T08:23:15Z",
    "dhcpName": "dkx2-101-03",
    "displayName": "dkx2-101-03",
    "hwAddr": "00:01:02:05:f1:ff",
    "ipv4Addr": "192.168.229.35",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:08:33Z",
        "start": "2019-06-25T19:07:50Z"
      },
      "udp": {
        "finish": "2019-06-25T18:13:25Z",
        "start": "2019-06-25T17:54:00Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:15:49Z",
        "start": "2019-06-25T19:15:43Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": false,
    "dhcpExpiry": "2019-06-04T18:23:59Z",
    "dhcpName": "",
    "displayName": "ac-wrt",
    "dnsName": "ac-wrt",
    "hwAddr": "00:01:02:80:00:02",
    "ipv4Addr": "192.168.229.98",
    "ring": "standard",
    "devID": {
      "ouiMfg": "Pegatron",
      "osGenus": "Linux",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:24:48Z",
        "start": "2019-06-23T20:24:43Z"
      },
      "udp": {
        "finish": "2019-06-03T19:03:21Z",
        "start": "2019-06-03T18:07:35Z"
      },
      "vuln": {
        "finish": "2019-06-20T16:25:10Z",
        "start": "2019-06-20T16:25:09Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": false,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000002",
    "connVAP": "psk",
    "allowedRings": [
      "quarantine",
      "unenrolled",
      "devices"
    ],
    "dhcpExpiry": "2019-04-08T18:42:43Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:fe:11:9c",
    "ipv4Addr": "192.168.227.12",
    "ring": "unenrolled",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:25:44Z",
        "start": "2019-06-23T20:25:39Z"
      },
      "udp": {
        "finish": "2019-05-28T16:20:26Z",
        "start": "2019-05-28T16:20:19Z"
      },
      "vuln": {
        "finish": "2019-05-28T16:19:09Z",
        "start": "2019-05-28T16:19:06Z"
      }
    },
    "wireless": true
  },
  {
    "active": false,
    "dhcpExpiry": "2019-06-13T20:15:12Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:9f",
    "ipv4Addr": "192.168.229.187",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-12T20:21:56Z",
        "start": "2019-06-12T20:21:51Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": false,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000002",
    "connVAP": "psk",
    "allowedRings": [
      "quarantine",
      "unenrolled",
      "devices"
    ],
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:fe:cc:f5",
    "ipv4Addr": "",
    "ring": "devices",
    "scans": {
      "tcp": {
        "finish": "2019-04-12T23:01:43Z",
        "start": "2019-04-12T23:01:39Z"
      },
      "udp": {
        "finish": "2019-04-12T19:58:38Z",
        "start": "2019-04-12T18:35:47Z"
      },
      "vuln": {
        "finish": "2019-04-12T18:00:11Z",
        "start": "2019-04-12T17:58:21Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "dhcpExpiry": "2019-06-15T21:18:05Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:a5",
    "ipv4Addr": "192.168.229.55",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-20T16:33:01Z",
        "start": "2019-06-20T16:32:56Z"
      },
      "udp": {
        "finish": "2019-06-14T17:35:34Z",
        "start": "2019-06-14T17:16:19Z"
      },
      "vuln": {
        "finish": "2019-06-14T17:44:06Z",
        "start": "2019-06-14T17:44:00Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": false,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "allowedRings": [
      "standard",
      "core",
      "quarantine"
    ],
    "dhcpExpiry": "2019-06-22T17:59:20Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:e0:5c:4b",
    "ipv4Addr": "192.168.229.91",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-21T17:59:59Z",
        "start": "2019-06-21T17:59:54Z"
      },
      "vuln": {
        "finish": "2019-06-21T17:59:59Z",
        "start": "2019-06-21T17:59:51Z"
      }
    },
    "wireless": true,
    "signalStrength": -40,
    "lastActivity": "2019-06-26T19:10:25Z",
  },
  {
    "active": false,
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:fe:17:a6",
    "ipv4Addr": "",
    "ring": "quarantine",
    "scans": {
      "tcp": {
        "finish": "2019-03-15T22:23:45Z",
        "start": "2019-03-15T22:20:53Z"
      },
      "udp": {
        "finish": "2019-03-15T19:26:33Z",
        "start": "2019-03-15T18:28:53Z"
      },
      "vuln": {
        "finish": "2019-03-15T22:12:20Z",
        "start": "2019-03-15T21:48:52Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": false,
    "dhcpExpiry": "2019-06-07T21:23:09Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:10:fb:35",
    "ipv4Addr": "192.168.229.160",
    "ring": "quarantine",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:24:09Z",
        "start": "2019-06-23T20:24:03Z"
      },
      "udp": {
        "finish": "2019-06-23T20:24:09Z",
        "start": "2019-06-23T20:24:06Z"
      },
      "vuln": {
        "finish": "2019-06-23T20:24:09Z",
        "start": "2019-06-23T20:24:03Z"
      }
    },
    "vulnerabilities": {
      "CVE-2017-0143": {
        "active": true,
        "details": "nmapScript: \"smb-vuln-ms17-010\" | Protocol: unknown | Port: unknown",
        "first_detected": "2019-06-06T22:39:41Z",
        "latest_detected": "2019-06-06T22:39:41Z",
        "repaired": null
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": true,
    "connBand": "5GHz",
    "connNode": "0dfc2484-9860-41e8-b5af-7677e18b9c2b",
    "connVAP": "eap",
    "allowedRings": [
      "standard",
      "core",
      "quarantine"
    ],
    "dhcpExpiry": "2019-06-24T01:50:32Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:51:67:41",
    "ipv4Addr": "192.168.229.115",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:23:23Z",
        "start": "2019-06-23T20:23:17Z"
      },
      "udp": {
        "finish": "2019-06-23T20:23:23Z",
        "start": "2019-06-23T20:23:17Z"
      },
      "vuln": {
        "finish": "2019-06-23T20:23:20Z",
        "start": "2019-06-23T20:23:09Z"
      }
    },
    "wireless": true,
    "signalStrength": -30,
    "lastActivity": "2019-06-26T19:10:25Z",
  },
  {
    "active": true,
    "dhcpExpiry": "2019-06-26T17:13:47Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:ce:c3:3f",
    "ipv4Addr": "192.168.229.127",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:07:50Z",
        "start": "2019-06-25T19:07:39Z"
      },
      "udp": {
        "finish": "2019-06-25T15:02:39Z",
        "start": "2019-06-25T14:43:06Z"
      },
      "vuln": {
        "finish": "2019-06-25T18:59:19Z",
        "start": "2019-06-25T18:59:13Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": true,
    "dhcpExpiry": "2019-06-26T15:35:58Z",
    "dhcpName": "skull",
    "displayName": "skull",
    "hwAddr": "00:01:02:08:27:70",
    "ipv4Addr": "192.168.229.246",
    "ring": "standard",
    "devID": {
      "ouiMfg": "Pegatron",
      "deviceGenus": "Linux Server",
      "osGenus": "Linux",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:12:01Z",
        "start": "2019-06-25T19:11:34Z"
      },
      "udp": {
        "finish": "2019-06-25T18:33:05Z",
        "start": "2019-06-25T18:13:25Z"
      },
      "vuln": {
        "finish": "2019-06-25T18:57:17Z",
        "start": "2019-06-25T18:57:11Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": true,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "allowedRings": [
      "standard",
      "core",
      "quarantine"
    ],
    "dhcpExpiry": "2019-06-26T19:07:24Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:22:64:83",
    "ipv4Addr": "192.168.229.12",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:09:10Z",
        "start": "2019-06-25T19:08:33Z"
      },
      "udp": {
        "finish": "2019-05-29T00:36:49Z",
        "start": "2019-05-29T00:17:07Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:08:03Z",
        "start": "2019-06-25T19:07:56Z"
      }
    },
    "wireless": true,
    "signalStrength": -67,
    "lastActivity": "2019-06-26T19:10:25Z",
  },
  {
    "active": false,
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:fe:df:5a",
    "ipv4Addr": "",
    "ring": "devices",
    "scans": {
      "tcp": {
        "finish": "2019-03-15T16:20:17Z",
        "start": "2019-03-15T16:26:57Z"
      },
      "udp": {
        "finish": "2019-03-15T10:57:26Z",
        "start": "2019-03-15T10:01:46Z"
      },
      "vuln": {
        "finish": "2019-03-15T14:46:09Z",
        "start": "2019-03-15T14:23:23Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": false,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "allowedRings": [
      "standard",
      "core",
      "quarantine"
    ],
    "dhcpExpiry": "2019-06-20T02:21:05Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:c0:f1:3c",
    "ipv4Addr": "192.168.229.28",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:32:14Z",
        "start": "2019-06-23T20:32:10Z"
      },
      "udp": {
        "finish": "2019-05-21T20:44:00Z",
        "start": "2019-05-21T20:43:56Z"
      },
      "vuln": {
        "finish": "2019-06-05T23:24:04Z",
        "start": "2019-06-05T23:24:02Z"
      }
    },
    "wireless": true
  },
  {
    "active": false,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "guest",
    "dhcpExpiry": "2019-06-18T20:23:32Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:5b:f1:1a",
    "ipv4Addr": "192.168.231.26",
    "ring": "guest",
    "scans": {
      "tcp": {
        "finish": "2019-06-20T16:27:32Z",
        "start": "2019-06-20T16:27:28Z"
      },
      "udp": {
        "finish": "2019-06-18T21:20:19Z",
        "start": "2019-06-18T21:20:14Z"
      },
      "vuln": {
        "finish": "2019-06-18T20:13:39Z",
        "start": "2019-06-18T20:13:33Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": false,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "allowedRings": [
      "standard",
      "core",
      "quarantine"
    ],
    "dhcpExpiry": "2019-06-25T19:38:47Z",
    "dhcpName": "Pams-MBP",
    "displayName": "Pams-MBP",
    "hwAddr": "00:01:02:b4:fc:90",
    "ipv4Addr": "192.168.229.114",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-24T23:41:03Z",
        "start": "2019-06-24T23:40:58Z"
      },
      "udp": {
        "finish": "2019-06-24T23:33:30Z",
        "start": "2019-06-24T23:29:38Z"
      },
      "vuln": {
        "finish": "2019-06-24T23:22:47Z",
        "start": "2019-06-24T23:22:41Z"
      }
    },
    "wireless": true,
    "signalStrength": -77,
    "lastActivity": "2019-06-26T19:10:25Z",
  },
  {
    "active": true,
    "dhcpExpiry": "2019-06-26T10:18:30Z",
    "dhcpName": "3b7c5700-ed16-4553-9ab8-5ab13a1e36ff",
    "displayName": "3b7c5700-ed16-4553-9ab8-5ab13a1e36ff",
    "hwAddr": "00:01:02:a0:00:79",
    "ipv4Addr": "192.168.229.10",
    "ring": "standard",
    "devID": {
      "osGenus": "Linux",
      "deviceGenus": "Brightgate Appliance",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:13:15Z",
        "start": "2019-06-25T19:12:51Z"
      },
      "udp": {
        "finish": "2019-06-25T19:15:08Z",
        "start": "2019-06-25T18:17:59Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:02:54Z",
        "start": "2019-06-25T19:02:46Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": false,
    "dhcpExpiry": "2019-06-15T20:18:54Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:a4",
    "ipv4Addr": "192.168.229.221",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:23:09Z",
        "start": "2019-06-23T20:22:59Z"
      },
      "udp": {
        "finish": "2019-06-23T20:23:09Z",
        "start": "2019-06-23T20:22:59Z"
      },
      "vuln": {
        "finish": "2019-06-23T20:23:09Z",
        "start": "2019-06-23T20:22:59Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": false,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "allowedRings": [
      "standard",
      "core",
      "quarantine"
    ],
    "dhcpExpiry": "2019-06-12T20:51:19Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:ab:75:da",
    "ipv4Addr": "192.168.229.74",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:27:38Z",
        "start": "2019-06-23T20:27:34Z"
      },
      "udp": {
        "finish": "2019-05-24T21:29:57Z",
        "start": "2019-05-24T21:29:52Z"
      },
      "vuln": {
        "finish": "2019-05-28T16:19:50Z",
        "start": "2019-05-28T16:19:47Z"
      }
    },
    "wireless": true
  },
  {
    "active": false,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "psk",
    "allowedRings": [
      "quarantine",
      "unenrolled",
      "devices"
    ],
    "dhcpExpiry": "2019-06-26T05:31:52Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:da:2e:09",
    "ipv4Addr": "192.168.230.192",
    "ring": "devices",
    "scans": {
      "tcp": {
        "finish": "2019-06-25T17:32:29Z",
        "start": "2019-06-25T17:32:24Z"
      },
      "udp": {
        "finish": "2019-05-28T16:19:47Z",
        "start": "2019-05-28T16:19:39Z"
      },
      "vuln": {
        "finish": "2019-06-25T17:32:29Z",
        "start": "2019-06-25T17:32:24Z"
      }
    },
    "wireless": true,
    "signalStrength": -97,
    "lastActivity": "2019-06-26T19:10:25Z",
  },
  {
    "active": false,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "allowedRings": [
      "standard",
      "core",
      "quarantine"
    ],
    "dhcpExpiry": "2019-05-15T18:44:13Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:93:60:7a",
    "ipv4Addr": "192.168.229.103",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:29:21Z",
        "start": "2019-06-23T20:29:16Z"
      },
      "udp": {
        "finish": "2019-05-28T16:20:48Z",
        "start": "2019-05-28T16:20:40Z"
      },
      "vuln": {
        "finish": "2019-05-28T16:19:16Z",
        "start": "2019-05-28T16:19:13Z"
      }
    },
    "wireless": false
  },
  {
    "active": true,
    "dhcpExpiry": "2019-06-26T08:23:03Z",
    "dhcpName": "creedap",
    "displayName": "creedap",
    "hwAddr": "00:01:02:d9:a7:a7",
    "ipv4Addr": "192.168.229.231",
    "ring": "standard",
    "devID": {
      "osGenus": "Linux",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:11:34Z",
        "start": "2019-06-25T19:11:15Z"
      },
      "udp": {
        "finish": "2019-06-25T14:43:23Z",
        "start": "2019-06-25T19:15:08Z"
      },
      "vuln": {
        "finish": "2019-06-25T18:59:31Z",
        "start": "2019-06-25T18:59:25Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": true,
    "dhcpExpiry": "2019-06-26T11:06:51Z",
    "dhcpName": "0dfc2484-9860-41e8-b5af-7677e18b9c2b",
    "displayName": "0dfc2484-9860-41e8-b5af-7677e18b9c2b",
    "hwAddr": "00:01:02:a0:00:53",
    "ipv4Addr": "192.168.226.5",
    "ring": "internal",
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:14:48Z",
        "start": "2019-06-25T19:14:29Z"
      },
      "udp": {
        "finish": "2019-06-25T16:03:19Z",
        "start": "2019-06-25T15:06:19Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:18:15Z",
        "start": "2019-06-25T19:18:07Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": true,
    "dhcpExpiry": "2019-06-26T09:13:45Z",
    "dhcpName": "moose",
    "displayName": "moose",
    "hwAddr": "00:01:02:88:8d:cb",
    "ipv4Addr": "192.168.229.240",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:04:10Z",
        "start": "2019-06-25T19:19:16Z"
      },
      "udp": {
        "finish": "2019-06-25T16:03:59Z",
        "start": "2019-06-25T16:00:06Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:15:07Z",
        "start": "2019-06-25T19:15:00Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": true,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "allowedRings": [
      "standard",
      "core",
      "quarantine"
    ],
    "dhcpExpiry": "2019-06-26T19:11:25Z",
    "dhcpName": "Samsung-Galaxy-S7",
    "displayName": "Samsung-Galaxy-S7",
    "hwAddr": "00:01:02:66:6b:90",
    "ipv4Addr": "192.168.229.20",
    "ring": "standard",
    "devID": {
      "ouiMfg": "Samsung",
      "deviceGenus": "Android Phone",
      "osGenus": "Android",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:16:08Z",
        "start": "2019-06-25T19:16:03Z"
      },
      "udp": {
        "finish": "2019-06-23T20:23:52Z",
        "start": "2019-06-23T20:23:50Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:16:08Z",
        "start": "2019-06-25T19:16:03Z"
      }
    },
    "wireless": true,
    "signalStrength": -65,
    "lastActivity": "2019-06-26T19:10:25Z",
  },
  {
    "active": true,
    "dhcpExpiry": "static",
    "dhcpName": "dp-raspberrypi",
    "displayName": "dp-raspberrypi",
    "hwAddr": "00:01:02:ae:b1:7f",
    "ipv4Addr": "192.168.229.52",
    "ring": "standard",
    "devID": {
      "ouiMfg": "Raspberry Pi Foundation",
      "deviceGenus": "Raspberry Pi",
      "osGenus": "Linux",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:03:53Z",
        "start": "2019-06-25T19:18:54Z"
      },
      "udp": {
        "finish": "2019-06-25T17:20:49Z",
        "start": "2019-06-25T16:23:36Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:16:45Z",
        "start": "2019-06-25T19:16:37Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": true,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000002",
    "connVAP": "eap",
    "allowedRings": [
      "standard",
      "core",
      "quarantine"
    ],
    "username": "pam@dundermifflin.com",
    "dhcpExpiry": "2019-06-26T19:10:25Z",
    "dhcpName": "pambook",
    "displayName": "pambook",
    "hwAddr": "00:01:02:b9:8d:b0",
    "ipv4Addr": "192.168.229.128",
    "devID": {
      "ouiMfg": "Apple",
      "deviceGenus": "Apple Macintosh",
      "osGenus": "macOS",
    },
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:12:31Z",
        "start": "2019-06-25T19:12:18Z"
      },
      "udp": {
        "finish": "2019-06-24T18:24:06Z",
        "start": "2019-06-24T17:24:56Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:12:43Z",
        "start": "2019-06-25T19:10:56Z"
      }
    },
    "wireless": true,
    "signalStrength": -35,
    "lastActivity": "2019-06-26T19:10:25Z",
  },
  {
    "active": true,
    "dhcpExpiry": "2019-06-26T03:03:15Z",
    "dhcpName": "XRX9C934E2CF72C",
    "displayName": "Hallway Printer",
    "friendlyName": "Hallway Printer",
    "friendlyDNS": "hallway-printer",
    "hwAddr": "00:01:02:2c:f7:2c",
    "ipv4Addr": "192.168.230.49",
    "ring": "devices",
    "devID": {
      "ouiMfg": "Xerox",
      "deviceGenus": "Xerox Printer",
      "osGenus": "Embedded/RTOS",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:11:31Z",
        "start": "2019-06-25T19:10:53Z"
      },
      "udp": {
        "finish": "2019-06-25T17:04:05Z",
        "start": "2019-06-25T17:00:33Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:15:31Z",
        "start": "2019-06-25T19:15:25Z"
      }
    },
    "vulnerabilities": {
      "defaultpassword": {
        "active": true,
        "details": "Service: ftp | Protocol: tcp | Port: 21 | User: \"admin\" | Password: \"password\"",
        "first_detected": "2020-01-03T16:39:15Z",
        "latest_detected": "2020-01-07T15:58:00Z",
        "repaired": null
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": false,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-21T17:47:40Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:da:b1:9a",
    "ipv4Addr": "192.168.229.26",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:32:09Z",
        "start": "2019-06-23T20:32:04Z"
      },
      "udp": {
        "finish": "2019-05-28T16:21:03Z",
        "start": "2019-05-28T16:20:56Z"
      },
      "vuln": {
        "finish": "2019-06-20T17:39:33Z",
        "start": "2019-06-20T17:39:22Z"
      }
    },
    "wireless": true,
    "signalStrength": -75,
    "lastActivity": "2019-06-26T19:10:25Z",
    "allowedRings": [
      "standard",
      "core",
      "quarantine"
    ],
  },
  {
    "active": true,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "allowedRings": [
      "standard",
      "core",
      "quarantine"
    ],
    "dhcpExpiry": "2019-06-26T19:07:32Z",
    "dhcpName": "DESKTOP-GEK7J5G",
    "displayName": "DESKTOP-GEK7J5G",
    "hwAddr": "00:01:02:1e:54:9f",
    "ipv4Addr": "192.168.229.32",
    "ring": "standard",
    "devID": {
      "ouiMfg": "Microsoft",
      "deviceGenus": "Microsoft Surface",
      "osGenus": "Windows",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:11:15Z",
        "start": "2019-06-25T19:09:10Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:08:14Z",
        "start": "2019-06-25T19:08:04Z"
      }
    },
    "wireless": true,
    "signalStrength": -55,
    "lastActivity": "2019-06-26T19:10:25Z",
  },
  {
    "active": false,
    "dhcpExpiry": "2019-06-12T21:19:27Z",
    "dhcpName": "schrute-farms",
    "displayName": "Dwight Schrute's PC",
    "dnsName": "",
    "friendlyName": "Dwight Schrute's PC",
    "friendlyDNS": "dwight-schrutes",
    "hwAddr": "00:01:02:9c:d0:2e",
    "ipv4Addr": "192.168.231.26",
    "ring": "guest",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:26:08Z",
        "start": "2019-06-23T20:26:02Z"
      },
      "vuln": {
        "finish": "2019-06-12T19:19:20Z",
        "start": "2019-06-12T19:19:00Z"
      }
    },
    "vulnerabilities": {
      "CVE-2014-0160": {
        "active": true,
        "details": "nmapScript: \"ssl-heartbleed\" | Protocol: tcp | Port: 443",
        "first_detected": "2019-06-12T19:19:19Z",
        "latest_detected": "2019-06-12T19:19:19Z",
        "repaired": null
      },
      "CVE-2018-6789": {
        "active": true,
        "details": "Program: \"exim\" | Version: 4.80 | Service: \"smtp\" | Protocol: tcp | Port: 25",
        "first_detected": "2019-06-12T19:19:19Z",
        "latest_detected": "2019-06-12T19:19:19Z",
        "repaired": null
      },
      "defaultpassword": {
        "active": true,
        "details": "Service: ssh | Protocol: tcp | Port: 22 | User: \"admin\" | Password: \"password\"",
        "first_detected": "2019-06-12T19:19:19Z",
        "latest_detected": "2019-06-12T19:19:19Z",
        "repaired": null
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": true,
    "dhcpExpiry": "2019-06-26T16:13:22Z",
    "dhcpName": "Danek",
    "displayName": "Danek",
    "hwAddr": "00:01:02:c9:02:d1",
    "ipv4Addr": "192.168.229.166",
    "ring": "standard",
    "devID": {
      "ouiMfg": "Apple",
      "osGenus": "macOS",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:15:23Z",
        "start": "2019-06-25T19:12:01Z"
      },
      "udp": {
        "finish": "2019-06-25T14:46:39Z",
        "start": "2019-06-25T14:43:23Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:12:31Z",
        "start": "2019-06-25T19:12:25Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": false,
    "dhcpExpiry": "2019-06-01T18:19:57Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:2d:a1:d1",
    "ipv4Addr": "192.168.229.97",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:23:07Z",
        "start": "2019-06-23T20:22:59Z"
      },
      "udp": {
        "finish": "2019-06-23T20:23:07Z",
        "start": "2019-06-23T20:22:59Z"
      },
      "vuln": {
        "finish": "2019-06-23T20:23:07Z",
        "start": "2019-06-23T20:22:59Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": true,
    "dhcpExpiry": "2019-06-26T12:12:30Z",
    "dhcpName": "beast",
    "displayName": "beast",
    "hwAddr": "00:01:02:59:46:b0",
    "ipv4Addr": "192.168.229.40",
    "ring": "standard",
    "devID": {
      "ouiMfg": "Giga-Byte",
      "deviceGenus": "Linux/Unix Server",
      "osGenus": "Linux",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:19:16Z",
        "start": "2019-06-25T19:19:05Z"
      },
      "udp": {
        "finish": "2019-06-25T16:23:36Z",
        "start": "2019-06-25T16:03:59Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:18:36Z",
        "start": "2019-06-25T19:18:30Z"
      }
    },
    "wireless": false,
    "allowedRings": [
      "quarantine",
      "standard",
      "unenrolled",
      "core",
      "devices",
      "guest"
    ],
  },
  {
    "active": true,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000002",
    "connVAP": "psk",
    "allowedRings": [
      "quarantine",
      "unenrolled",
      "devices"
    ],
    "dhcpExpiry": "2019-06-26T07:07:23Z",
    "dhcpName": "amazon-160612181",
    "displayName": "Pam's Echo Dot",
    "dnsName": "",
    "friendlyName": "Pam's Echo Dot",
    "friendlyDNS": "pams-echo-dot",
    "hwAddr": "00:01:02:03:70:78",
    "ipv4Addr": "192.168.230.166",
    "ring": "devices",
    "devID": {
      "ouiMfg": "Amazon",
      "deviceGenus": "Amazon Echo",
      "osGenus": "Linux",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:10:53Z",
        "start": "2019-06-25T19:09:49Z"
      },
      "udp": {
        "finish": "2019-06-25T17:54:00Z",
        "start": "2019-06-25T17:04:05Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:07:03Z",
        "start": "2019-06-25T19:06:55Z"
      }
    },
    "wireless": true,
    "signalStrength": -75,
    "lastActivity": "2019-06-26T19:10:25Z",
  },
  {
    "active": true,
    "dhcpExpiry": "2019-05-22T22:51:45Z",
    "dhcpName": "roku",
    "displayName": "roku",
    "hwAddr": "00:01:02:f4:1d:eb",
    "ipv4Addr": "192.168.229.166",
    "ring": "standard",
    "devID": {
      "ouiMfg": "Roku",
      "deviceGenus": "Roku Streaming Media Player",
      "osGenus": "Linux",
    },
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:28:57Z",
        "start": "2019-06-23T20:28:52Z"
      },
      "vuln": {
        "finish": "2019-05-28T16:19:35Z",
        "start": "2019-05-28T16:19:33Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "connBand": "2.4GHz",
    /* this node is deliberately invalid to ensure this works */
    "connNode": "814fb2f4-1ce4-4158-b7fa-1d4534745b94",
    "connVAP": "guest",
    "dhcpExpiry": "2019-06-07T03:53:37Z",
    "dhcpName": "device-with-invalid-nodeid",
    "displayName": "device-with-invalid-nodeid",
    "hwAddr": "00:01:02:58:be:46",
    "ipv4Addr": "192.168.229.81",
    "ring": "guest",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:23:56Z",
        "start": "2019-06-23T20:23:50Z"
      },
      "udp": {
        "finish": "2019-06-23T20:23:56Z",
        "start": "2019-06-23T20:23:52Z"
      },
      "vuln": {
        "finish": "2019-06-23T20:23:56Z",
        "start": "2019-06-23T20:23:50Z"
      }
    },
    "wireless": true
  },
];

