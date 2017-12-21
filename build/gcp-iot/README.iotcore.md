```
COPYRIGHT 2017 Brightgate Inc. All rights reserved.

This copyright notice is Copyright Management Information under 17 USC 1202
and is included to protect this work and deter copyright infringement.
Removal or alteration of this Copyright Management Information without the
express written permission of Brightgate Inc is prohibited, and any
such unauthorized removal or alteration will be a violation of federal law.
```

README.iotcore.md

This directory contains tools for use with Google Cloud IoT core.

# Device (Appliance) Registry

The topmost object in the IoT core is what Google calls the "device registry"--
for us, this is a registry of the Appliances in our fleet.  An arbitrary number
of such registries can be created (helpful for testing), and the
`mk_registry.sh` script will create a new one.  Once a registry is created,
devices (Appliances) can be provisioned to the registry.

Example:

```shell
$ ./mk_registry.sh ~/secrets/Engineering.json peppy-breaker-161717 my-appliance-registry
Activated service account credentials for: [bg-iot-core-administration@peppy-breaker-161717.iam.gserviceaccount.com]
Created topic [projects/peppy-breaker-161717/topics/iot-my-appliance-registry-events].
bindings:
- members:
  - serviceAccount:cloud-iot@system.gserviceaccount.com
  role: roles/pubsub.publisher
etag: BwVgPxEEEWA=
Created topic [projects/peppy-breaker-161717/topics/iot-my-appliance-registry-state].
bindings:
- members:
  - serviceAccount:cloud-iot@system.gserviceaccount.com
  role: roles/pubsub.publisher
etag: BwVgPxEioNk=
Created registry [my-appliance-registry].
---------- my-appliance-registry -------------------------------------
eventNotificationConfigs:
- pubsubTopicName: projects/peppy-breaker-161717/topics/iot-my-appliance-registry-events
httpConfig:
  httpEnabledState: HTTP_ENABLED
id: my-appliance-registry
mqttConfig:
  mqttEnabledState: MQTT_ENABLED
name: projects/peppy-breaker-161717/locations/us-central1/registries/my-appliance-registry
stateNotificationConfig:
  pubsubTopicName: projects/peppy-breaker-161717/topics/iot-my-appliance-registry-state
```

Where `Engineering.json` is credential fetched from a GCP service account with
sufficient authorization.  (In our Engineering environment this is called
`BG IOT Core Administration` or `bg-iot-core-administration@peppy-breaker-161717.iam.gserviceaccount.com`).

# Device (Appliance) Provisioning

The provisioning process involves creating a key pair, provisioning the public
side of the key pair to the IoT Core, and provisioning the private side of the
key pair to the appliance.  The private side of the key pair, as well as the
rest of the credential parameters, are bundled in a JSON envelope.

Example:

First, allocate the key pair and register with IoT core:

```shell
$ ./provision_device_to_registry.sh ~/Engineering.json peppy-breaker-161717 my-appliance-registry my-bg1
Generating Key/Pair and Certificate for my-bg1
Generating a 2048 bit RSA private key
..........................................................................+++
...............................................................................+++
writing new private key to 'my-bg1.rsa_private.pem'
-----
Adding my-bg1 to registry my-appliance-registry in region us-central1
Created device [my-bg1].
---------- my-appliance-registry -------------------------------------
config:
  cloudUpdateTime: '2017-12-13T21:02:16.747355Z'
  version: '1'
credentials:
- expirationTime: '1970-01-01T00:00:00Z'
  publicKey:
    format: RSA_X509_PEM
    key: |
      -----BEGIN CERTIFICATE-----
      MIIC+DCCAeCgAwIBAgIJAJQTWrkzOAZHMA0GCSqGSIb3DQEBCwUAMBExDzANBgNV
      BAMMBnVudXNlZDAeFw0xNzEyMTMyMTAyMTZaFw0xODAxMTIyMTAyMTZaMBExDzAN
      BgNVBAMMBnVudXNlZDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBANTf
      w0A4sqaFAkoOve88ORhcGreMtlAvVSUDbWP4lEfUHrSAcZTFlz6f7LbWkBiMVJiW
      JTzPMf+CI/NQHkxrKXZsNxolx8jxQVaK+1vQJSFAn4FrH6rhhuzN+ZPpEBleVCid
      MXcQEWcyTFcc3d112u7SH04w/In1GZr0i075Wl7RKvhpGG5uRZznzb7+j7VN9xig
      TsZr4ASx8JTwf+hLpnwsE+346ASkN+HzmUe9vsX/F6KngKwTXGjoLoMWdda155Zg
      G+rkbv4zdzHy6MJPT8kda6Z66KFeuOvDHhv2rGxbLj5Jr6vKbl9ec+5ZJexyXT7a
      xZyF7Q/FFVAsw2ct00sCAwEAAaNTMFEwHQYDVR0OBBYEFMMvoYiVpBDQPrXe79KM
      sUBAj+K+MB8GA1UdIwQYMBaAFMMvoYiVpBDQPrXe79KMsUBAj+K+MA8GA1UdEwEB
      /wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAGfQY574sHdHm2voo1CUBBXHXxHL
      3KAks5uYssXMjGga0MdAbsv/9UqiKG0w1hv3E/upIjpRx2M9FIKDAFqV3BQCVnoC
      +r9QtZlO1VX/rHIWuS/ZRKvZHKZLP1WrjGPhgOK1/e9IUX0Gbxr1LEfdPSnuEElf
      4slOMT2J/EDOgDpdwQcSSDeKAr02UJ3Jfy9kj2PgCKhsHYmRlxSP4S1bho0OZ29s
      vti96HJkq0ZYdCbybybIR1w3XZZ/aHdAeRT+dOwZUhZbswjjso3Tl5ExmwTQEBjO
      nVe6A2MsuaLiV9HTGcTPeekYExv8UU5qNkeVTn7WLAauaA4t6q5fAygoXbs=
      -----END CERTIFICATE-----
id: my-bg1
metadata:
  net_b10e_iot_device_uuid: c5ae5b88-6680-4458-af11-ad79a0514da0
name: projects/peppy-breaker-161717/locations/us-central1/registries/my-appliance-registry/devices/2602124588818925
numId: '2602124588818925'
-------------------------------------------------------------

All set.  Now provision my-bg1.iotcore.secret.json to the appliance: /opt/com.brightgate/etc/secret/iotcore/iotcore.secret.json
```

As directed, copy the `my-bg1.iotcore.secret.json` file to the appliance, and
name it `/opt/com.brightgate/etc/secret/iotcore/iotcore.secret.json`.
`chmod 600 /opt/com.brightgate/etc/secret/iotcore/iotcore.secret.json`
is a good practice.  At this point, `cron` should begin sending a very tiny
scrap of telemetry about your appliance every few minutes.  To confirm that
it's working, try:

```
pi@pi $ sudo /opt/com.brightgate/bin/ap-iot -root /opt/com.brightgate upbeat
2018-01-03T16:41:11.581-0800	INFO	ap-iot/iot.go:125	Sent upbeat	{"text": "{\"component_version\":[\"git:rPS@ebb494b-dirty\"],\"boot_time\":\"2018-01-03T11:33:32-08:00\",\"record_time\":\"2018-01-03T16:41:11-08:00\"}"}
```
