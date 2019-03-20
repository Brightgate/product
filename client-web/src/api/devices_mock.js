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

// To generate this file, capture an example response using chrome developer
// tools, then put it through 'jq --sort-keys .' to format it.
// Anonymize by wiping out the first three octets of the mac addrs
// s/HwAddr": "[0-9a-f][0-9a-f]:[0-9a-f][0-9a-f]:[0-9a-f][0-9a-f]/HwAddr": "00:01:02/g
//
// and change some machine names.

/* eslint-disable quotes, comma-dangle */
export default {
  "Devices": [
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T15:08",
      "HumanName": "dkx2-101-03",
      "HwAddr": "00:01:02:05:f1:ff",
      "IPv4Addr": "192.168.229.138",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:11:56Z",
          "start": "2019-03-20T18:11:20Z"
        },
        "udp": {
          "finish": "2019-03-20T14:40:27Z",
          "start": "2019-03-20T14:21:03Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:33:04Z",
          "start": "2019-03-20T17:33:03Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "ConnBand": "2.4GHz",
      "ConnNode": "7f74092b-c0e3-40ff-9b89-02aad83a8c73",
      "ConnVAP": "eap",
      "DHCPExpiry": "2019-03-21T17:48",
      "HumanName": "HWLAB-INTELSTICK",
      "HwAddr": "00:01:02:93:60:7a",
      "IPv4Addr": "192.168.229.103",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:09:44Z",
          "start": "2019-03-20T18:09:16Z"
        },
        "udp": {
          "finish": "2019-03-20T14:21:03Z",
          "start": "2019-03-20T13:25:16Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:33:06Z",
          "start": "2019-03-20T17:33:04Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "ConnBand": "2.4GHz",
      "ConnNode": "7f74092b-c0e3-40ff-9b89-02aad83a8c73",
      "ConnVAP": "eap",
      "DHCPExpiry": "2019-03-21T17:16",
      "HumanName": "PamAroneXSMax",
      "HwAddr": "00:01:02:08:2c:5e",
      "IPv4Addr": "192.168.229.67",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:09:47Z",
          "start": "2019-03-20T18:09:44Z"
        },
        "vuln": {
          "finish": "2019-03-18T21:59:57Z",
          "start": "2019-03-18T21:59:56Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "ConnBand": "5GHz",
      "ConnNode": "abdc7ca6-2a7e-40e0-9988-d3a5d2489f6a",
      "ConnVAP": "eap",
      "DHCPExpiry": "2019-03-21T16:43",
      "HumanName": "Unnamed (60:f8:1d:d2:46:82)",
      "HwAddr": "00:01:02:d2:46:82",
      "IPv4Addr": "192.168.229.193",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:14:01Z",
          "start": "2019-03-20T18:13:31Z"
        },
        "udp": {
          "finish": "2019-03-15T23:34:36Z",
          "start": "2019-03-15T23:31:33Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:59:30Z",
          "start": "2019-03-20T17:59:29Z"
        }
      }
    },
    {
      "Active": false,
      "Confidence": 0,
      "ConnNode": "abdc7ca6-2a7e-40e0-9988-d3a5d2489f6a",
      "ConnVAP": "eap",
      "DHCPExpiry": "static",
      "HumanName": "Unnamed (b8:27:eb:f2:f3:60)",
      "HwAddr": "00:01:02:f2:f3:60",
      "IPv4Addr": "",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "unenrolled"
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T16:45",
      "HumanName": "sat-conference",
      "HwAddr": "00:01:02:ca:8d:b5",
      "IPv4Addr": "192.168.226.4",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "internal",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:11:15Z",
          "start": "2019-03-20T18:10:58Z"
        },
        "udp": {
          "finish": "2019-03-20T13:25:16Z",
          "start": "2019-03-20T17:29:18Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:37:38Z",
          "start": "2019-03-20T17:14:38Z"
        }
      }
    },
    {
      "Active": false,
      "Confidence": 0,
      "ConnNode": "abdc7ca6-2a7e-40e0-9988-d3a5d2489f6a",
      "ConnVAP": "eap",
      "DHCPExpiry": "static",
      "HumanName": "Unnamed (48:45:20:50:2a:3a)",
      "HwAddr": "00:01:02:50:2a:3a",
      "IPv4Addr": "",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-15T16:31:58Z",
          "start": "2019-03-15T16:31:57Z"
        },
        "udp": {
          "finish": "2019-03-15T16:31:58Z",
          "start": "2019-03-15T16:31:57Z"
        },
        "vuln": {
          "finish": "2019-03-15T16:31:55Z",
          "start": "2019-03-15T16:31:55Z"
        }
      }
    },
    {
      "Active": false,
      "Confidence": 0,
      "DHCPExpiry": "static",
      "HumanName": "Unnamed (a0:40:a0:53:71:73)",
      "HwAddr": "00:01:02:53:71:73",
      "IPv4Addr": "",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard"
    },
    {
      "Active": false,
      "Confidence": 0,
      "DHCPExpiry": "static",
      "HumanName": "Unnamed (9c:ef:d5:fe:df:58)",
      "HwAddr": "00:01:02:fe:df:58",
      "IPv4Addr": "",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "unenrolled",
      "Scans": {
        "tcp": {
          "finish": "2019-03-15T16:26:57Z",
          "start": "2019-03-15T16:24:07Z"
        },
        "udp": {
          "finish": "2019-03-15T10:01:46Z",
          "start": "2019-03-15T16:28:52Z"
        },
        "vuln": {
          "finish": "2019-03-15T15:32:46Z",
          "start": "2019-03-15T15:09:59Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T09:59",
      "HumanName": "Jim",
      "HwAddr": "00:01:02:c9:02:d1",
      "IPv4Addr": "192.168.229.144",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:11:20Z",
          "start": "2019-03-20T18:08:12Z"
        },
        "udp": {
          "finish": "2019-03-20T14:44:04Z",
          "start": "2019-03-20T14:41:27Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:11:17Z",
          "start": "2019-03-20T17:54:04Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T18:03",
      "HumanName": "Unnamed (98:10:e8:f1:b5:22)",
      "HwAddr": "00:01:02:f1:b5:22",
      "IPv4Addr": "192.168.229.168",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:13:09Z",
          "start": "2019-03-20T18:12:24Z"
        },
        "udp": {
          "finish": "2019-03-20T16:28:46Z",
          "start": "2019-03-20T15:39:50Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:59:30Z",
          "start": "2019-03-20T17:59:30Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "ConnBand": "5GHz",
      "ConnNode": "abdc7ca6-2a7e-40e0-9988-d3a5d2489f6a",
      "ConnVAP": "eap",
      "DHCPExpiry": "2019-03-21T14:41",
      "HumanName": "Unnamed (ac:bc:32:d1:79:0b)",
      "HwAddr": "00:01:02:d1:79:0b",
      "IPv4Addr": "192.168.229.187",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:15:15Z",
          "start": "2019-03-20T18:11:56Z"
        },
        "udp": {
          "finish": "2019-03-19T13:56:17Z",
          "start": "2019-03-19T13:51:19Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:32:52Z",
          "start": "2019-03-20T17:29:32Z"
        }
      }
    },
    {
      "Active": false,
      "Confidence": 0,
      "DHCPExpiry": "static",
      "HumanName": "Unnamed (9c:ef:d5:fe:df:59)",
      "HwAddr": "00:01:02:fe:df:59",
      "IPv4Addr": "",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "guest",
      "Scans": {
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
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "ConnBand": "5GHz",
      "ConnNode": "abdc7ca6-2a7e-40e0-9988-d3a5d2489f6a",
      "ConnVAP": "eap",
      "DHCPExpiry": "2019-03-21T16:47",
      "HumanName": "Ryans-Mini",
      "HwAddr": "00:01:02:f3:59:f0",
      "IPv4Addr": "192.168.229.196",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:13:12Z",
          "start": "2019-03-20T18:13:09Z"
        },
        "udp": {
          "finish": "2019-03-19T22:29:50Z",
          "start": "2019-03-19T22:25:44Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:37:39Z",
          "start": "2019-03-20T17:37:38Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "ConnBand": "5GHz",
      "ConnNode": "abdc7ca6-2a7e-40e0-9988-d3a5d2489f6a",
      "ConnVAP": "guest",
      "DHCPExpiry": "2019-03-21T16:32",
      "HumanName": "Stanleys-MBP-10",
      "HwAddr": "00:01:02:37:14:c9",
      "IPv4Addr": "192.168.229.70",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T16:35:49Z",
          "start": "2019-03-20T16:35:46Z"
        },
        "udp": {
          "finish": "2019-03-15T16:34:19Z",
          "start": "2019-03-15T16:34:15Z"
        },
        "vuln": {
          "finish": "2019-03-15T16:34:17Z",
          "start": "2019-03-15T16:34:15Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T16:42",
      "HumanName": "Stanleys-MBP",
      "HwAddr": "00:01:02:b3:e4:19",
      "IPv4Addr": "192.168.229.179",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:12:24Z",
          "start": "2019-03-20T18:12:02Z"
        },
        "udp": {
          "finish": "2019-03-19T21:30:17Z",
          "start": "2019-03-19T21:26:25Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:54:04Z",
          "start": "2019-03-20T17:54:04Z"
        }
      }
    },
    {
      "Active": false,
      "Confidence": 0,
      "ConnBand": "5GHz",
      "ConnNode": "abdc7ca6-2a7e-40e0-9988-d3a5d2489f6a",
      "ConnVAP": "eap",
      "DHCPExpiry": "2019-03-19T23:19",
      "HumanName": "Unnamed (6c:40:08:b9:8d:b0)",
      "HwAddr": "00:01:02:b9:8d:b0",
      "IPv4Addr": "192.168.227.219",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "unenrolled",
      "Scans": {
        "tcp": {
          "finish": "2019-03-19T23:12:54Z",
          "start": "2019-03-19T23:12:51Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "ConnBand": "5GHz",
      "ConnNode": "abdc7ca6-2a7e-40e0-9988-d3a5d2489f6a",
      "ConnVAP": "eap",
      "DHCPExpiry": "2019-03-21T16:48",
      "HumanName": "Kellys-MBP",
      "HwAddr": "00:01:02:d3:51:69",
      "IPv4Addr": "192.168.229.169",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:13:31Z",
          "start": "2019-03-20T18:13:12Z"
        },
        "udp": {
          "finish": "2019-03-15T16:31:50Z",
          "start": "2019-03-15T16:31:50Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:59:30Z",
          "start": "2019-03-20T17:59:30Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T15:24",
      "HumanName": "theonion",
      "HwAddr": "00:01:02:59:46:ae",
      "IPv4Addr": "192.168.229.99",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:10:58Z",
          "start": "2019-03-20T18:10:31Z"
        },
        "udp": {
          "finish": "2019-03-20T17:24:32Z",
          "start": "2019-03-20T16:28:46Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:33:03Z",
          "start": "2019-03-20T17:32:53Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "ConnBand": "5GHz",
      "ConnNode": "abdc7ca6-2a7e-40e0-9988-d3a5d2489f6a",
      "ConnVAP": "eap",
      "DHCPExpiry": "2019-03-21T17:48",
      "HumanName": "Unnamed (40:4e:36:22:64:83)",
      "HwAddr": "00:01:02:22:64:83",
      "IPv4Addr": "192.168.229.21",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:10:31Z",
          "start": "2019-03-20T18:10:00Z"
        },
        "vuln": {
          "finish": "2019-03-20T18:06:34Z",
          "start": "2019-03-20T18:06:33Z"
        }
      }
    },
    {
      "Active": false,
      "Confidence": 0,
      "DHCPExpiry": "static",
      "HumanName": "Unnamed (9e:ef:d5:fe:17:a6)",
      "HwAddr": "00:01:02:fe:17:a6",
      "IPv4Addr": "",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "quarantine",
      "Scans": {
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
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "static",
      "HumanName": "stanley-raspberrypi",
      "HwAddr": "00:01:02:ae:b1:7f",
      "IPv4Addr": "192.168.229.52",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:07:53Z",
          "start": "2019-03-20T18:14:01Z"
        },
        "udp": {
          "finish": "2019-03-20T17:27:38Z",
          "start": "2019-03-20T16:31:52Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:54:04Z",
          "start": "2019-03-20T17:35:36Z"
        }
      }
    },
    {
      "Active": false,
      "Confidence": 0,
      "DHCPExpiry": "static",
      "HumanName": "Unnamed (9c:ef:d5:fe:df:5a)",
      "HwAddr": "00:01:02:fe:df:5a",
      "IPv4Addr": "",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "devices",
      "Scans": {
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
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-20T22:27",
      "HumanName": "811dbd58-4371-4375-8698-af4265d66531",
      "HwAddr": "00:01:02:a0:00:27",
      "IPv4Addr": "192.168.229.104",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-19T23:05:38Z",
          "start": "2019-03-19T23:05:35Z"
        },
        "udp": {
          "finish": "2019-03-19T23:29:38Z",
          "start": "2019-03-19T22:34:05Z"
        },
        "vuln": {
          "finish": "2019-03-19T22:35:49Z",
          "start": "2019-03-19T22:35:47Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T16:46",
      "HumanName": "sat-conservatory",
      "HwAddr": "00:01:02:64:ee:4f",
      "IPv4Addr": "192.168.226.3",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "internal",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:08:27Z",
          "start": "2019-03-20T18:08:10Z"
        },
        "udp": {
          "finish": "2019-03-20T15:36:05Z",
          "start": "2019-03-20T14:40:27Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:29:32Z",
          "start": "2019-03-20T18:06:34Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T16:36",
      "HumanName": "raspberrypi",
      "HwAddr": "00:01:02:7e:c5:03",
      "IPv4Addr": "192.168.229.19",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:11:43Z",
          "start": "2019-03-20T18:11:15Z"
        },
        "udp": {
          "finish": "2019-03-20T16:31:52Z",
          "start": "2019-03-20T15:36:05Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:33:08Z",
          "start": "2019-03-20T17:33:06Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T16:34",
      "HumanName": "darryl",
      "HwAddr": "00:01:02:88:8d:cb",
      "IPv4Addr": "192.168.229.166",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:07:57Z",
          "start": "2019-03-20T18:15:16Z"
        },
        "udp": {
          "finish": "2019-03-20T14:41:27Z",
          "start": "2019-03-20T14:37:40Z"
        },
        "vuln": {
          "finish": "2019-03-20T18:06:33Z",
          "start": "2019-03-20T17:33:08Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T11:18",
      "HumanName": "XRX00000000002C",
      "HwAddr": "00:01:02:2c:f7:2c",
      "IPv4Addr": "192.168.229.81",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:09:16Z",
          "start": "2019-03-20T18:08:27Z"
        },
        "udp": {
          "finish": "2019-03-20T17:29:18Z",
          "start": "2019-03-20T17:24:32Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:33:04Z",
          "start": "2019-03-20T17:33:04Z"
        }
      },
      "Vulnerabilities": {
        "defaultpassword": {
          "active": true,
          "details": "Service: ftp | Protocol: tcp | Port: 21 | User: \"admin\" | Password: \"password\"",
          "first_detected": "2019-03-03T16:39:15Z",
          "latest_detected": "2019-03-20T17:33:04Z",
          "repaired": "0001-01-01T00:00:00Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T06:49",
      "HumanName": "duvall-pi0",
      "HwAddr": "00:01:02:6d:4c:74",
      "IPv4Addr": "192.168.229.126",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:08:10Z",
          "start": "2019-03-20T18:07:53Z"
        },
        "udp": {
          "finish": "2019-03-20T13:22:42Z",
          "start": "2019-03-20T17:27:38Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:14:38Z",
          "start": "2019-03-20T17:59:30Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T16:06",
      "HumanName": "creed2",
      "HwAddr": "00:01:02:a7:a6:35",
      "IPv4Addr": "192.168.229.53",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T16:34:43Z",
          "start": "2019-03-20T16:34:40Z"
        },
        "udp": {
          "finish": "2019-03-20T12:29:42Z",
          "start": "2019-03-20T11:33:56Z"
        },
        "vuln": {
          "finish": "2019-03-20T15:49:18Z",
          "start": "2019-03-20T15:35:50Z"
        }
      }
    },
    {
      "Active": false,
      "Confidence": 0,
      "DHCPExpiry": "static",
      "HumanName": "Unnamed (9e:ef:d5:fe:19:2c)",
      "HwAddr": "00:01:02:fe:19:2c",
      "IPv4Addr": "",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "unenrolled",
      "Scans": {
        "tcp": {
          "finish": "2019-03-15T16:29:24Z",
          "start": "2019-03-15T16:29:21Z"
        },
        "udp": {
          "finish": "2019-03-15T15:23:33Z",
          "start": "2019-03-15T16:19:19Z"
        },
        "vuln": {
          "finish": "2019-03-15T15:54:47Z",
          "start": "2019-03-15T16:28:08Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "ConnBand": "5GHz",
      "ConnNode": "7f74092b-c0e3-40ff-9b89-02aad83a8c73",
      "ConnVAP": "eap",
      "DHCPExpiry": "2019-03-21T17:32",
      "HumanName": "Pams-MBP",
      "HwAddr": "00:01:02:b4:fc:90",
      "IPv4Addr": "192.168.229.16",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:05:26Z",
          "start": "2019-03-20T18:05:22Z"
        },
        "udp": {
          "finish": "2019-03-15T16:31:58Z",
          "start": "2019-03-15T16:31:58Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:37:38Z",
          "start": "2019-03-20T17:37:38Z"
        }
      }
    },
    {
      "Active": false,
      "Confidence": 0,
      "ConnBand": "2.4GHz",
      "ConnNode": "abdc7ca6-2a7e-40e0-9988-d3a5d2489f6a",
      "ConnVAP": "eap",
      "DHCPExpiry": "2019-03-19T23:06",
      "HumanName": "Unnamed (64:9a:be:da:b1:9a)",
      "HwAddr": "00:01:02:da:b1:9a",
      "IPv4Addr": "192.168.229.124",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-18T23:20:30Z",
          "start": "2019-03-18T23:20:27Z"
        },
        "udp": {
          "finish": "2019-03-15T16:31:54Z",
          "start": "2019-03-15T16:31:53Z"
        },
        "vuln": {
          "finish": "2019-03-18T22:22:54Z",
          "start": "2019-03-18T22:22:53Z"
        }
      }
    },
    {
      "Active": false,
      "Confidence": 0,
      "ConnBand": "5GHz",
      "ConnNode": "abdc7ca6-2a7e-40e0-9988-d3a5d2489f6a",
      "ConnVAP": "eap",
      "DHCPExpiry": "2019-03-16T19:21",
      "HumanName": "Unnamed (6c:40:08:ab:75:da)",
      "HwAddr": "00:01:02:ab:75:da",
      "IPv4Addr": "192.168.229.48",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-15T22:39:06Z",
          "start": "2019-03-15T22:39:03Z"
        },
        "udp": {
          "finish": "2019-03-15T16:31:56Z",
          "start": "2019-03-15T16:31:56Z"
        },
        "vuln": {
          "finish": "2019-03-15T16:31:54Z",
          "start": "2019-03-15T16:31:54Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T16:35",
      "HumanName": "creedap",
      "HwAddr": "00:01:02:d9:a7:a7",
      "IPv4Addr": "192.168.229.188",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:12:02Z",
          "start": "2019-03-20T18:11:43Z"
        },
        "udp": {
          "finish": "2019-03-20T15:39:49Z",
          "start": "2019-03-20T14:44:04Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:34:17Z",
          "start": "2019-03-20T17:11:20Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T10:44",
      "HumanName": "island",
      "HwAddr": "00:01:02:d5:92:83",
      "IPv4Addr": "192.168.229.102",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:08:12Z",
          "start": "2019-03-20T18:07:57Z"
        },
        "udp": {
          "finish": "2019-03-20T14:37:40Z",
          "start": "2019-03-20T14:18:12Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:59:29Z",
          "start": "2019-03-20T17:37:39Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "DHCPExpiry": "2019-03-21T16:52",
      "HumanName": "OpenWrt",
      "HwAddr": "00:01:02:11:22:34",
      "IPv4Addr": "192.168.229.14",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T18:10:00Z",
          "start": "2019-03-20T18:09:47Z"
        },
        "udp": {
          "finish": "2019-03-20T14:18:11Z",
          "start": "2019-03-20T13:22:42Z"
        },
        "vuln": {
          "finish": "2019-03-20T17:35:36Z",
          "start": "2019-03-20T17:34:17Z"
        }
      }
    },
    {
      "Active": true,
      "Confidence": 0,
      "ConnBand": "2.4GHz",
      "ConnNode": "ead08092-26c6-4b6b-8488-e15f35d0d6b0",
      "ConnVAP": "eap",
      "DHCPExpiry": "2019-03-20T23:54",
      "HumanName": "Samsung-Galaxy-S7",
      "HwAddr": "00:01:02:66:6b:90",
      "IPv4Addr": "192.168.229.48",
      "Kind": "unknown",
      "Manufacturer": "unknown",
      "Model": "unknown (id=)",
      "Ring": "standard",
      "Scans": {
        "tcp": {
          "finish": "2019-03-20T00:29:55Z",
          "start": "2019-03-20T00:29:52Z"
        },
        "udp": {
          "finish": "2019-03-15T16:31:51Z",
          "start": "2019-03-15T16:31:51Z"
        },
        "vuln": {
          "finish": "2019-03-19T23:00:11Z",
          "start": "2019-03-19T23:00:09Z"
        }
      }
    }
  ]
};
