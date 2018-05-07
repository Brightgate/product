
COPYRIGHT 2018 Brightgate Inc. All rights reserved.

This copyright notice is Copyright Management Information under 17 USC 1202
and is included to protect this work and deter copyright infringement.
Removal or alteration of this Copyright Management Information without the
express written permission of Brightgate Inc is prohibited, and any
such unauthorized removal or alteration will be a violation of federal law.


# Product Software README

# Directories

```
base/                   Resource and Protocol Buffer message definitions

build/                  Scripts to do with building and provisioning

client-web/             Web-based frontend

golang/src/bg/          Golang-based command, daemon, and library
                        implementations.
```

## Directory naming

Name directories in snake case, as Python module names may not include
'-' characters.

## The "proto" area

When dealing with appliance components, $ROOT is equivalent to
`./proto.$(uname -m)/appliance/`.  On an imaged system, $ROOT is
equivalent to /.  Brightgate specific components are in
`$ROOT/opt/com.brightgate`.

When dealing with cloud components, $ROOT is equivalent to
`./proto.$(uname -m)/cloud/`.  Brightgate specific components are in
`$ROOT/opt/net.b10e/`.

# Installing Tools

## Installing Golang (Go) Language Tools

We typically write "Golang" to improve search uniqueness.

If you are using our cloud systems to build, Golang is installed at
/opt/net.b10e/go-1.10.2

To retrieve the Golang SDK for Linux x64, use

```
$ curl -O https://storage.googleapis.com/golang/go1.8.3.linux-amd64.tar.gz
```

To retrieve the Golang SDK for Linux ARM, use

```
$ curl -O https://storage.googleapis.com/golang/go1.8.3.linux-armv6l.tar.gz
```

For Linux systems, we recommend unpacking these archives in $HOME.  (If
you wish to keep multiple Golang versions, then `mv go go-1.8.3; ln -s
go-1.8.3 go` or the equivalent may be helpful after unpacking.  You will
want to remove the go symbolic link before unpacking a new version.)

To retrieve the Golang SDK for macOS x64, use

```
$ curl -O https://storage.googleapis.com/golang/go1.8.3.darwin-amd64.tar.gz
```

Installation of this package creates `/usr/local/go`.

Because each platform generates local tools, you will want to use "make
plat-clobber" to switch a repository from one platform to another, such as from
Linux to macOS, or from x86 to ARM.  (An easier way is to keep parallel
workspaces in sync by making one the git upstream of the other).

## Installing node.js

On our x86 cloud dev systems, node.js 8.x should be installed; if not, use
ansible to do so.  For ARM systems, you need to install node.js 8.x.  Here is a
reasonable way to do so:

```
pi$ curl -sL https://deb.nodesource.com/setup_8.x | sudo -E bash -

(should connect you to nodesource's pkg repo)

pi$ sudo apt-get install -y nodejs

pi$ nodejs --version
v8.10.0  (may be higher than this)
```

## Installing Prometheus

Install Prometheus for ARM:

```
pi$ curl -LO https://github.com/prometheus/prometheus/releases/download/v1.8.0/prometheus-1.8.0.linux-armv7.tar.gz
pi$ tar -xf prometheus-1.8.0.linux-armv7.tar.gz
pi$ sudo cp prometheus-1.8.0.linux-armv7/prometheus /usr/local/bin/
```

# Building Product Software

`make` is used to build the software components.  Useful targets:

```
$ make install
$ make client-web
$ make test
$ make coverage
$ make lint-go
$ make packages

$ make clean
$ make clobber
$ make plat-clobber
```

If Golang is not installed in the expected places as described above, you may
need to set `$GOROOT` in your environment before running `make`.

## Building Debian packages

While not required (you may run from the `proto` area), you may wish to build
installable packages for ARM:

```
$ make packages
...
dpkg-deb: building package 'bg-appliance' in 'bg-appliance_0.0.1803052236-1_amd64.deb'.
...

$ sudo dpkg -i bg-appliance_0.0.1803052236-1_amd64.deb (use name from above)

```

If you replace an existing bg-appliance package, some amount of restarting, up
to and including rebooting will be needed.

## Cross-building from x86 to ARM

You can build the ARM bits on a (Linux) x86 platform, including packaging.
Simply put `GOARCH=arm` into the environment, or add it to the end of the `make`
commandline:
```
$ make packages GOARCH=arm
```
See `build/cross-compile/README.md` for more details.

## Connecting to Google IoT Core

Our appliances talk to the cloud using Google IoT Core.  At a minimum, request
that an IoT Core secret be provisioned for your appliance.  Install that to
`$ROOT/opt/com.brightgate/etc/secret/iotcore/iotcore.secret.json`
in order for your IoT connectivity to work.  See
`build/gcp-iot/README.iotcore.md` for more details.

## TLS Certificates

Our appliances use LetsEncrypt TLS certificates.  Ensure that you have
correct certificates installed for your appliance in `/etc/letsencrypt`.

# Running from the Proto Area

`sudo(8)` is used to acquire privilege from the developer during testing.

The ap.relayd daemon forwards UDP broadcast requests between security rings.
To allow mDNS forwarding to work correctly, the Linux mDNS responder
(avahi-daemon) must be disabled before launching our daemons:

```
$ sudo systemctl disable avahi-daemon
$ sudo systemctl stop avahi-daemon
```

(If you build and install packages, the package scripting will take care of
this).

Components are installed in `$(ROOT)/opt/com.brightgate/bin`.  ap.mcp in that
directory is the 'master control process', and is responsible for launching and
monitoring all of our other daemons.  To run our software, mcp should be run as
root:

```
$ sudo ./proto.armv7l/appliance/opt/com.brightgate/bin/ap.mcp
aproot not set - using '/home/pi/Product.slave/proto.armv7l/appliance/opt/com.brightgate'
2018/03/05 15:21:27 mcp.go:763: ap.mcp (3711) coming online...
2018/03/05 15:21:27 mcp.go:544: MCP online
```

Or, if you are using packages (logs to `/var/log/mcp.log`):
```
$ sudo systemctl restart ap.mcp
```

Either put ap.mcp in the background, or switch to another window, and launch
the remaining daemons:

```
$ ./proto.armv7l/appliance/opt/com.brightgate/bin/ap-ctl start all
$ ./proto.armv7l/appliance/opt/com.brightgate/bin/ap-ctl status all
      DAEMON      PID          STATE    SINCE
     brokerd     3518         online    Sat Sep 16 13:59:14
      dhcp4d     3721         online    Sat Sep 16 13:59:31
     filterd     3710         online    Sat Sep 16 13:59:31
       httpd     3730         online    Sat Sep 16 13:59:31
 identifierd     3554         online    Sat Sep 16 13:59:25
    networkd     3552         online    Sat Sep 16 13:59:25
     sampled     3724         online    Sat Sep 16 13:59:29
     configd     3535         online    Sat Sep 16 13:59:20
       dns4d     3553         online    Sat Sep 16 13:59:22
     dnsmasq     3715         online    Sat Sep 16 13:59:26
        logd     3536         online    Sat Sep 16 13:59:18
      relayd     3709         online    Sat Sep 16 13:59:31
       scand     3711         online    Sat Sep 16 13:59:31
```

(It may take a little while for all of the services to come online)

# Return to the Wiki

Once this is working, return to the Wiki to learn how to configure the AP,
for example, enabling WAN-facing SSH and configuring SSIDs and EAP users.

# External assets

- Images
  - [Exclamation Mark](https://pixabay.com/en/attention-warning-exclamation-mark-98513/)
	Creative Commons: Free for commercial use, no attribution required
- Assets in client-web:
  - `(cd client-web && npm run licenses)`
