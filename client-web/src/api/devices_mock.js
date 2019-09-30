/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
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
    "confidence": 0,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000002",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-25T17:01:02Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:08:2c:5e",
    "ipv4Addr": "192.168.229.11",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "confidence": 0,
    "dhcpExpiry": "static",
    "dhcpName": "a564029e-71bc-4dd5-91e8-a6fe44c02ac5",
    "displayName": "creed-wrt",
    "dnsName": "creed-wrt",
    "hwAddr": "00:01:02:a0:00:08",
    "ipv4Addr": "192.168.229.48",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
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
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T08:30:58Z",
    "dhcpName": "a1fa1e2d-67c1-4a6b-b70b-5469635aa215",
    "displayName": "a1fa1e2d-67c1-4a6b-b70b-5469635aa215",
    "hwAddr": "00:01:02:a0:00:27",
    "ipv4Addr": "192.168.229.80",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
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
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T08:23:15Z",
    "dhcpName": "dkx2-101-03",
    "displayName": "dkx2-101-03",
    "hwAddr": "00:01:02:05:f1:ff",
    "ipv4Addr": "192.168.229.35",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-06-04T18:23:59Z",
    "dhcpName": "",
    "displayName": "ac-wrt",
    "dnsName": "ac-wrt",
    "hwAddr": "00:01:02:80:00:02",
    "ipv4Addr": "192.168.229.98",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000002",
    "connVAP": "psk",
    "dhcpExpiry": "2019-04-08T18:42:43Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:fe:11:9c",
    "ipv4Addr": "192.168.227.12",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-06-13T20:15:12Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:9f",
    "ipv4Addr": "192.168.229.187",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-12T20:21:56Z",
        "start": "2019-06-12T20:21:51Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000002",
    "connVAP": "psk",
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:fe:cc:f5",
    "ipv4Addr": "",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "confidence": 0,
    "dhcpExpiry": "2019-06-15T21:18:05Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:a5",
    "ipv4Addr": "192.168.229.55",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-22T17:59:20Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:e0:5c:4b",
    "ipv4Addr": "192.168.229.91",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "confidence": 0,
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:fe:17:a6",
    "ipv4Addr": "",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-06-07T21:23:09Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:10:fb:35",
    "ipv4Addr": "192.168.229.160",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "connBand": "5GHz",
    "connNode": "0dfc2484-9860-41e8-b5af-7677e18b9c2b",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-24T01:50:32Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:51:67:41",
    "ipv4Addr": "192.168.229.115",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T17:13:47Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:ce:c3:3f",
    "ipv4Addr": "192.168.229.127",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T15:35:58Z",
    "dhcpName": "skull",
    "displayName": "skull",
    "hwAddr": "00:01:02:08:27:70",
    "ipv4Addr": "192.168.229.246",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
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
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-26T19:07:24Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:22:64:83",
    "ipv4Addr": "192.168.229.12",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "confidence": 0,
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:fe:df:5a",
    "ipv4Addr": "",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-20T02:21:05Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:c0:f1:3c",
    "ipv4Addr": "192.168.229.28",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "guest",
    "dhcpExpiry": "2019-06-18T20:23:32Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:5b:f1:1a",
    "ipv4Addr": "192.168.231.26",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-25T19:38:47Z",
    "dhcpName": "Pams-MBP",
    "displayName": "Pams-MBP",
    "hwAddr": "00:01:02:b4:fc:90",
    "ipv4Addr": "192.168.229.114",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-04-26T18:28:50Z",
    "dhcpName": "",
    "displayName": "creed2",
    "dnsName": "creed2",
    "hwAddr": "00:01:02:a7:a6:35",
    "ipv4Addr": "192.168.229.53",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:27:23Z",
        "start": "2019-06-23T20:27:18Z"
      },
      "udp": {
        "finish": "2019-04-26T21:23:56Z",
        "start": "2019-04-26T21:23:52Z"
      },
      "vuln": {
        "finish": "2019-05-28T16:20:02Z",
        "start": "2019-05-28T16:19:59Z"
      }
    },
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T10:18:30Z",
    "dhcpName": "3b7c5700-ed16-4553-9ab8-5ab13a1e36ff",
    "displayName": "3b7c5700-ed16-4553-9ab8-5ab13a1e36ff",
    "hwAddr": "00:01:02:a0:00:79",
    "ipv4Addr": "192.168.229.10",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-06-15T20:18:54Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:a4",
    "ipv4Addr": "192.168.229.221",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-12T20:51:19Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:ab:75:da",
    "ipv4Addr": "192.168.229.74",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:4b:77:32",
    "ipv4Addr": "",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "",
    "scans": {
      "tcp": {
        "finish": "2019-04-08T18:51:54Z",
        "start": "2019-04-08T18:59:07Z"
      },
      "udp": {
        "finish": "2019-04-08T16:44:30Z",
        "start": "2019-04-08T18:24:11Z"
      },
      "vuln": {
        "finish": "2019-04-08T18:50:38Z",
        "start": "2019-04-08T18:50:39Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "psk",
    "dhcpExpiry": "2019-06-26T05:31:52Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:da:2e:09",
    "ipv4Addr": "192.168.230.192",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "confidence": 0,
    "dhcpExpiry": "2019-06-04T18:40:07Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:14",
    "ipv4Addr": "192.168.229.239",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:26:49Z",
        "start": "2019-06-23T20:26:44Z"
      },
      "udp": {
        "finish": "2019-06-06T21:49:13Z",
        "start": "2019-06-06T21:49:09Z"
      },
      "vuln": {
        "finish": "2019-06-20T16:25:09Z",
        "start": "2019-06-20T16:25:00Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "dhcpExpiry": "2019-05-15T18:44:13Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:93:60:7a",
    "ipv4Addr": "192.168.229.103",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "confidence": 0,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000002",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-24T12:36:54Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:27:d2:d1",
    "ipv4Addr": "192.168.229.131",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:29:41Z",
        "start": "2019-06-23T20:29:36Z"
      },
      "udp": {
        "finish": "2019-05-28T16:20:11Z",
        "start": "2019-05-28T16:20:05Z"
      },
      "vuln": {
        "finish": "2019-06-23T13:30:27Z",
        "start": "2019-06-23T13:30:22Z"
      }
    },
    "wireless": true,
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T08:23:03Z",
    "dhcpName": "creedap",
    "displayName": "creedap",
    "hwAddr": "00:01:02:d9:a7:a7",
    "ipv4Addr": "192.168.229.231",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
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
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T11:06:51Z",
    "dhcpName": "0dfc2484-9860-41e8-b5af-7677e18b9c2b",
    "displayName": "0dfc2484-9860-41e8-b5af-7677e18b9c2b",
    "hwAddr": "00:01:02:a0:00:53",
    "ipv4Addr": "192.168.226.5",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T09:13:45Z",
    "dhcpName": "moose",
    "displayName": "moose",
    "hwAddr": "00:01:02:88:8d:cb",
    "ipv4Addr": "192.168.229.240",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-05-26T03:09:43Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:ce:c3:3d",
    "ipv4Addr": "192.168.229.199",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:26:00Z",
        "start": "2019-06-23T20:25:55Z"
      },
      "udp": {
        "finish": "2019-05-28T16:22:01Z",
        "start": "2019-05-28T16:21:58Z"
      },
      "vuln": {
        "finish": "2019-05-28T16:21:55Z",
        "start": "2019-05-28T16:21:52Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-06-04T18:48:28Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:85",
    "ipv4Addr": "192.168.229.147",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:23:45Z",
        "start": "2019-06-23T20:23:39Z"
      },
      "udp": {
        "finish": "2019-06-23T20:23:45Z",
        "start": "2019-06-23T20:23:43Z"
      },
      "vuln": {
        "finish": "2019-06-23T20:23:45Z",
        "start": "2019-06-23T20:23:38Z"
      }
    },
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T08:23:53Z",
    "dhcpName": "sat-conservatory",
    "displayName": "sat-conservatory",
    "hwAddr": "00:01:02:64:ee:4f",
    "ipv4Addr": "192.168.226.3",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "internal",
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:12:17Z",
        "start": "2019-06-25T19:11:58Z"
      },
      "udp": {
        "finish": "2019-06-25T18:17:59Z",
        "start": "2019-06-25T17:20:49Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:02:45Z",
        "start": "2019-06-25T19:02:37Z"
      }
    },
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-26T19:11:25Z",
    "dhcpName": "Samsung-Galaxy-S7",
    "displayName": "Samsung-Galaxy-S7",
    "hwAddr": "00:01:02:66:6b:90",
    "ipv4Addr": "192.168.229.20",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
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
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-06-04T21:04:09Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:0e",
    "ipv4Addr": "192.168.229.15",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:26:42Z",
        "start": "2019-06-23T20:26:37Z"
      },
      "udp": {
        "finish": "2019-04-02T18:35:26Z",
        "start": "2019-04-02T17:39:48Z"
      },
      "vuln": {
        "finish": "2019-05-28T16:19:31Z",
        "start": "2019-05-28T16:19:28Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:fe:df:59",
    "ipv4Addr": "",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "guest",
    "scans": {
      "tcp": {
        "finish": "2019-03-15T16:18:46Z",
        "start": "2019-03-15T16:15:12Z"
      },
      "udp": {
        "finish": "2019-03-15T11:26:15Z",
        "start": "2019-03-15T10:30:33Z"
      },
      "vuln": {
        "finish": "2019-03-15T13:36:28Z",
        "start": "2019-03-15T13:13:39Z"
      }
    },
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T08:23:12Z",
    "dhcpName": "dev-test2",
    "displayName": "dev-test2",
    "hwAddr": "00:01:02:ca:8d:b5",
    "ipv4Addr": "192.168.229.159",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:06:40Z",
        "start": "2019-06-25T19:06:23Z"
      },
      "udp": {
        "finish": "2019-06-25T15:06:19Z",
        "start": "2019-06-25T14:46:40Z"
      },
      "vuln": {
        "finish": "2019-06-25T18:59:43Z",
        "start": "2019-06-25T18:59:37Z"
      }
    },
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "static",
    "dhcpName": "dp-raspberrypi",
    "displayName": "dp-raspberrypi",
    "hwAddr": "00:01:02:ae:b1:7f",
    "ipv4Addr": "192.168.229.52",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-06-13T19:49:53Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:6b",
    "ipv4Addr": "192.168.229.56",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:24:15Z",
        "start": "2019-06-23T20:24:10Z"
      },
      "udp": {
        "finish": "2019-05-24T21:30:19Z",
        "start": "2019-05-24T21:30:15Z"
      },
      "vuln": {
        "finish": "2019-06-23T20:24:15Z",
        "start": "2019-06-23T20:24:09Z"
      }
    },
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "guest",
    "dhcpExpiry": "2019-06-24T13:17:03Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:bf:63:26",
    "ipv4Addr": "192.168.229.25",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:23:17Z",
        "start": "2019-06-23T20:23:10Z"
      },
      "udp": {
        "finish": "2019-06-23T20:23:17Z",
        "start": "2019-06-23T20:23:09Z"
      },
      "vuln": {
        "finish": "2019-06-23T20:23:17Z",
        "start": "2019-06-23T20:23:07Z"
      }
    },
    "wireless": true,
    "signalStrength": -75,
    "lastActivity": "2019-06-26T19:10:25Z",
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-06-15T22:43:33Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:56:f9:37",
    "ipv4Addr": "192.168.229.184",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:23:30Z",
        "start": "2019-06-23T20:23:23Z"
      },
      "udp": {
        "finish": "2019-06-23T20:23:30Z",
        "start": "2019-06-23T20:23:24Z"
      },
      "vuln": {
        "finish": "2019-06-23T20:23:26Z",
        "start": "2019-06-23T20:23:17Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:53:71:73",
    "ipv4Addr": "",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:31:a5:9a",
    "ipv4Addr": "",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "",
    "scans": {
      "tcp": {
        "finish": "2019-04-23T23:01:30Z",
        "start": "2019-04-23T23:01:27Z"
      }
    },
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000002",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-26T19:10:25Z",
    "dhcpName": "catbook",
    "displayName": "catbook",
    "hwAddr": "00:01:02:b9:8d:b0",
    "ipv4Addr": "192.168.229.128",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "confidence": 0,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000002",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-24T07:30:27Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:5c:b0:e8",
    "ipv4Addr": "192.168.229.175",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:24:43Z",
        "start": "2019-06-23T20:24:38Z"
      },
      "vuln": {
        "finish": "2019-06-23T10:46:55Z",
        "start": "2019-06-23T10:46:50Z"
      }
    },
    "wireless": true
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-05-30T18:54:55Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:8b",
    "ipv4Addr": "192.168.229.95",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:30:07Z",
        "start": "2019-06-23T20:30:02Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-05-30T19:04:25Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:92",
    "ipv4Addr": "192.168.229.251",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:25:55Z",
        "start": "2019-06-23T20:25:50Z"
      }
    },
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-26T18:28:42Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:d1:79:0b",
    "ipv4Addr": "192.168.229.19",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:07:39Z",
        "start": "2019-06-25T19:06:23Z"
      },
      "udp": {
        "finish": "2019-06-25T00:27:13Z",
        "start": "2019-06-25T00:23:11Z"
      },
      "vuln": {
        "finish": "2019-06-25T18:54:51Z",
        "start": "2019-06-25T18:29:14Z"
      }
    },
    "wireless": true,
    "signalStrength": -85,
    "lastActivity": "2019-06-26T19:10:25Z",
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:4b:77:31",
    "ipv4Addr": "",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "",
    "scans": {
      "tcp": {
        "finish": "2019-04-08T18:49:35Z",
        "start": "2019-04-08T18:47:58Z"
      },
      "udp": {
        "finish": "2019-04-08T15:39:27Z",
        "start": "2019-04-08T14:45:19Z"
      },
      "vuln": {
        "finish": "2019-04-08T17:45:35Z",
        "start": "2019-04-08T17:36:05Z"
      }
    },
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T03:03:15Z",
    "dhcpName": "XRX9C934E2CF72C",
    "displayName": "XRX9C934E2CF72C",
    "hwAddr": "00:01:02:2c:f7:2c",
    "ipv4Addr": "192.168.230.49",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "devices",
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
        "active": false,
        "details": "Service: ftp | Protocol: tcp | Port: 21 | User: \"admin\" | Password: \"password\"",
        "first_detected": "2019-03-03T16:39:15Z",
        "latest_detected": "2019-06-20T15:58:00Z",
        "repaired": null
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:31:a5:99",
    "ipv4Addr": "",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "",
    "scans": {
      "tcp": {
        "finish": "2019-04-23T23:01:03Z",
        "start": "2019-04-23T23:00:59Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-06-21T20:24:25Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:26",
    "ipv4Addr": "192.168.229.22",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:26:26Z",
        "start": "2019-06-23T20:26:22Z"
      },
      "udp": {
        "finish": "2019-05-28T16:19:56Z",
        "start": "2019-05-28T16:19:48Z"
      },
      "vuln": {
        "finish": "2019-06-20T20:22:22Z",
        "start": "2019-06-20T20:22:12Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-06-13T19:16:48Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:9e",
    "ipv4Addr": "192.168.229.150",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:23:50Z",
        "start": "2019-06-23T20:23:43Z"
      },
      "udp": {
        "finish": "2019-06-23T20:23:50Z",
        "start": "2019-06-23T20:23:45Z"
      },
      "vuln": {
        "finish": "2019-06-23T20:23:50Z",
        "start": "2019-06-23T20:23:43Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-21T17:47:40Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:da:b1:9a",
    "ipv4Addr": "192.168.229.26",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
  },
  {
    "active": true,
    "confidence": 0,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-26T19:07:32Z",
    "dhcpName": "DESKTOP-GEK7J5G",
    "displayName": "DESKTOP-GEK7J5G",
    "hwAddr": "00:01:02:1e:54:9f",
    "ipv4Addr": "192.168.229.32",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
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
    "confidence": 0,
    "dhcpExpiry": "2019-06-12T21:19:27Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:9c:d0:2e",
    "ipv4Addr": "192.168.231.26",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T16:13:22Z",
    "dhcpName": "Danek",
    "displayName": "Danek",
    "hwAddr": "00:01:02:c9:02:d1",
    "ipv4Addr": "192.168.229.166",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-06-01T18:19:57Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:2d:a1:d1",
    "ipv4Addr": "192.168.229.97",
    "kind": "",
    "manufacturer": "",
    "model": "",
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
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T08:23:20Z",
    "dhcpName": "duvall-pi0",
    "displayName": "duvall-pi0",
    "hwAddr": "00:01:02:6d:4c:74",
    "ipv4Addr": "192.168.229.126",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:09:49Z",
        "start": "2019-06-25T19:06:55Z"
      },
      "udp": {
        "finish": "2019-06-25T16:00:06Z",
        "start": "2019-06-25T15:02:39Z"
      },
      "vuln": {
        "finish": "2019-06-25T18:55:53Z",
        "start": "2019-06-25T18:55:44Z"
      }
    },
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "dhcpExpiry": "2019-06-26T12:12:30Z",
    "dhcpName": "beast",
    "displayName": "beast",
    "hwAddr": "00:01:02:59:46:b0",
    "ipv4Addr": "192.168.229.40",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "connBand": "5GHz",
    "connNode": "0dfc2484-9860-41e8-b5af-7677e18b9c2b",
    "connVAP": "guest",
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:b9:8d:b1",
    "ipv4Addr": "",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000002",
    "connVAP": "psk",
    "dhcpExpiry": "2019-06-26T07:07:23Z",
    "dhcpName": "amazon-160612181",
    "displayName": "echo-dot",
    "dnsName": "echo-dot",
    "hwAddr": "00:01:02:03:70:78",
    "ipv4Addr": "192.168.230.166",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "devices",
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
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-05-30T21:01:47Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:93",
    "ipv4Addr": "192.168.229.207",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:26:58Z",
        "start": "2019-06-23T20:26:52Z"
      },
      "vuln": {
        "finish": "2019-05-29T21:49:31Z",
        "start": "2019-05-29T21:49:29Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-05-22T22:51:45Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:f4:1d:eb",
    "ipv4Addr": "192.168.229.166",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
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
    "confidence": 0,
    "dhcpExpiry": "2019-06-13T18:18:55Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:99",
    "ipv4Addr": "192.168.229.243",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:27:03Z",
        "start": "2019-06-23T20:26:58Z"
      }
    },
    "wireless": false
  },
  {
    "active": true,
    "confidence": 0,
    "connBand": "5GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "eap",
    "dhcpExpiry": "2019-06-26T18:06:42Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:d2:46:82",
    "ipv4Addr": "192.168.229.85",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-25T19:11:57Z",
        "start": "2019-06-25T19:11:31Z"
      },
      "udp": {
        "finish": "2019-06-13T23:24:57Z",
        "start": "2019-06-13T23:19:55Z"
      },
      "vuln": {
        "finish": "2019-06-25T19:07:31Z",
        "start": "2019-06-25T19:07:23Z"
      }
    },
    "wireless": true,
    "signalStrength": -55,
    "lastActivity": "2019-06-26T19:10:25Z",
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "static",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:b3:e4:19",
    "ipv4Addr": "",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-05-03T22:47:53Z",
        "start": "2019-05-03T22:47:50Z"
      },
      "udp": {
        "finish": "2019-03-21T22:22:27Z",
        "start": "2019-03-21T22:16:49Z"
      },
      "vuln": {
        "finish": "2019-03-21T22:51:44Z",
        "start": "2019-03-21T22:51:43Z"
      }
    },
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "connBand": "2.4GHz",
    "connNode": "001-201901BB-000001",
    "connVAP": "guest",
    "dhcpExpiry": "2019-06-07T03:53:37Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:58:be:46",
    "ipv4Addr": "192.168.229.81",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
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
    "wireless": false
  },
  {
    "active": false,
    "confidence": 0,
    "dhcpExpiry": "2019-05-14T22:41:44Z",
    "dhcpName": "",
    "displayName": "",
    "hwAddr": "00:01:02:a0:00:84",
    "ipv4Addr": "192.168.229.153",
    "kind": "",
    "manufacturer": "",
    "model": "",
    "ring": "standard",
    "scans": {
      "tcp": {
        "finish": "2019-06-23T20:23:59Z",
        "start": "2019-06-23T20:23:52Z"
      },
      "udp": {
        "finish": "2019-06-23T20:23:59Z",
        "start": "2019-06-23T20:23:57Z"
      },
      "vuln": {
        "finish": "2019-06-23T20:23:59Z",
        "start": "2019-06-23T20:23:52Z"
      }
    },
    "wireless": false
  }
];
