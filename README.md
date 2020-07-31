
COPYRIGHT 2020 Brightgate Inc. All rights reserved.

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

doc/                    HTML product documentation

golang/src/bg/          Golang-based command, daemon, and library
                        implementations
```

## Directory naming

Name directories in snake case, as Python module names may not include
`-` characters.

## The "proto" area

When dealing with appliance components, `$ROOT` is equivalent to
`./proto.$(uname -m)/appliance/`.  On an imaged system, `$ROOT` is
equivalent to `/`.  Brightgate specific components are in
`$ROOT/opt/com.brightgate`.

When dealing with cloud components, `$ROOT` is equivalent to
`./proto.$(uname -m)/cloud/`.  Brightgate specific components are in
`$ROOT/opt/net.b10e/`.

# Installing Tools

The Wiki has instructions on installing Go and Node.js, which are needed to
build.  All the tools should be available for you if you're building on one of
the cloud VMs, but that will not be the case on your Raspberry Pi or other
machine you maintain.  Current versions are:

  | Software    | Version
  | ----------- | -------
  | Go (golang) | 1.14.6 (any 1.14.x is likely to work)
  | Node.js     | 8.11.2 (any 8.x is likely to work)

# Building Product Software

`make` is used to build the software components.  Useful targets:

```
$ make install
$ make client-web
$ make client-web-dev
$ make test
$ make coverage
$ make check-go   # runs vet-go, lint-go, fmt-go
$ make packages
$ make doc
$ make doc-check

$ make clean
$ make clobber
```

If Golang is not installed in the expected places as described above, you may
need to set `$GOROOT` in your environment before running `make`.

## Go Dependencies

Go dependencies, recorded in `go.mod` and `go.sum`, are supposed to be
relatively self-maintaining.  The `go` executable will typically update the
files with new modules when it needs to, and a run of `go mod tidy` will remove
stale ones.

When adding a new dependency, the simplest thing to do is to make the requisite
changes to your sources (import the new package, do whatever with it) and then
override `GO_MOD_FLAG` to nothing in order to allow `go` to update `go.mod` and
`go.sum` appropriately:
```
$ make GO_MOD_FLAG=
```
Without that override, you'll get the error message
`import lookup disabled by -mod=readonly`.

To update a dependency, there's a convenience target using the `DEPNAME`
variable:
```
$ make godeps-update DEPNAME=path/to/module@version
```
The version tag can be any commit hash, branch, or tag.  If you specify no
`@version` (or `@latest`), then you get the latest tagged version.

A more aggressive update target is `godeps-update-all`, which upgrades the
dependency and all of its dependencies.  Normally, the latter are specified by
the direct dependency.  The default is to update the indirect dependencies to
the latest patch of the same minor.  Specify `UPDATE_MINOR=1` to upgrade
further.  Specify `UPDATE_TEST=1` to update test dependencies as well.

To remove a dependency, remove the use of the package from the sources and run
`make godeps-tidy`.

When using the `make` targets, there is no need to or unset any `$GO...`
environment variables.  But when running `go` commands independently, you must
`cd` to `golang/src/bg` and make sure that `$GO111MODULE` is set to `on` (unless
you have `$GOPATH` set, which is largely no longer useful).  This includes `go doc`
(for docs of dependencies) and `go test`.

There is much more information at `go help modules`, `go help module-get`,
`go help mod edit` (and other `mod` subcommands), as well as at the
[Golang Module Wiki](https://github.com/golang/go/wiki/Modules).

## Building Debian packages

While not required (you may run from the `proto` area), you may wish to build
installable packages for ARM:

```
$ make packages
...
dpkg-deb: building package 'bg-appliance' in 'bg-appliance_0.0.1803052236-1_armhf.deb'.
...

$ sudo dpkg -i bg-appliance_0.0.1803052236-1_armhf.deb (use name from above)

```

If you replace an existing `bg-appliance` package, some amount of restarting, up
to and including rebooting will be needed.

## Cross-building from x86 to ARM

You can build the ARM bits on a (Linux) x86 platform, including packaging.
Simply put `GOARCH=arm` into the environment, or add it to the end of the `make`
commandline:
```
$ make packages GOARCH=arm
```
See `build/cross-compile/README.md` for more details.

## Shared workspaces

Because each platform generates local tools, you will want to use `make clobber`
to switch a repository from one platform to another, such as from Linux to
macOS, or from x86 to ARM.  (An easier way is to keep parallel workspaces in
sync by making one the git upstream of the other).

## Connecting to Cloud Backend

Our appliances talk to the cloud using a mixture of methods, primarily GRPC.
At a minimum, request that a cloud secret be provisioned for your
appliance.  Install that to `$APSECRET/rpcd/cloud.secret.json` in order
for your cloud connectivity to work.  See below for the definition of
`$APSECRET`, and `build/gcp-appliance-reg/README.registry.md` for more details.

# Appliance paths

On Debian-based appliances, we assume we can use apt and LSB directory
hierarchies to organize our files in an LSB-compatible fashion.  On
OpenWrt-based appliances, we use a shared partition (`/data`) so that we can
use an "update the inactive partition" approach.

             | Debian                   | OpenWrt             | Proto area
  --------------------------------------------------------------------------
  | APROOT   | /                        | /                   | $GITROOT/proto.$ARCH/appliance
  | APDATA   | $APROOT/var/spool        | $APROOT/data        | $APROOT/var/spool
  | APSECRET | $APROOT/var/spool/secret | $APROOT/data/secret | $APROOT/var/spool/secret

APPACKAGE is equal to $APROOT/opt/com.brightgate on all platforms.

Note that APSECRET is a placeholder for eventual use of TPM or other secure
storage.

# Running from the Proto Area

`sudo`(8) is used to acquire privilege from the developer during testing.

The `ap.relayd` daemon forwards UDP broadcast requests between security rings.
To allow mDNS forwarding to work correctly, the Linux mDNS responder
(avahi-daemon) must be disabled before launching our daemons:

```
$ sudo systemctl disable avahi-daemon
$ sudo systemctl stop avahi-daemon
```

Components are installed in `$(ROOT)/opt/com.brightgate/bin`.  `ap.mcp` in that
directory is the 'master control process', and is responsible for launching and
monitoring all of our other daemons.  Unless you're running in `http-dev` mode,
`ap.mcp` must run as root for full functionality:

```
$ sudo ./proto.armv7l/appliance/opt/com.brightgate/bin/ap.mcp
aproot not set - using '/home/pi/Product/proto.armv7l/appliance/opt/com.brightgate'
2018/06/08 21:17:36 mcp.go:763: ap.mcp (3711) coming online...
2018/06/08 21:17:36 mcp.go:544: MCP online
```

Then switch to another window (so the log messages don't clutter up a terminal
you're actively using; preferably, use the `-l` flag to `ap.mcpd` to send the
log messages to the file named by its argument) and launch the remaining
daemons:

```
$ ./proto.armv7l/appliance/opt/com.brightgate/bin/ap-ctl start all
$ ./proto.armv7l/appliance/opt/com.brightgate/bin/ap-ctl status all
      DAEMON   PID     RSS   SWAP   TIME        STATE               SINCE NODE
      ap.mcp 14484   25 MB   0 MB     0s       online  Fri Jun 8 21:17:36 gateway
     brokerd 14564   26 MB   0 MB     0s       online  Fri Jun 8 21:17:51 gateway
     configd 14581   29 MB   0 MB     0s       online  Fri Jun 8 21:17:53 gateway
      dhcp4d 14706   27 MB   0 MB     0s       online  Fri Jun 8 21:17:59 gateway
       dns4d 14619   34 MB   0 MB     0s       online  Fri Jun 8 21:17:57 gateway
     dnsmasq 14703    3 MB   0 MB     0s       online  Fri Jun 8 21:17:56 gateway
       httpd 14701   34 MB   0 MB     0s       online  Fri Jun 8 21:18:00 gateway
 identifierd 14620   61 MB   0 MB     0s       online  Fri Jun 8 21:17:59 gateway
        iotd 14582   39 MB   0 MB     0s       online  Fri Jun 8 21:17:55 gateway
        logd 14583   25 MB   0 MB     0s       online  Fri Jun 8 21:17:53 gateway
    networkd 14618   29 MB   0 MB     0s       online  Fri Jun 8 21:17:56 gateway
      relayd 14702   28 MB   0 MB     0s       online  Fri Jun 8 21:17:59 gateway
     updated 14621   39 MB   0 MB     0s       online  Fri Jun 8 21:17:56 gateway
   userauthd 14622   28 MB   0 MB     0s       online  Fri Jun 8 21:17:56 gateway
      watchd 14704   46 MB   0 MB     0s       online  Fri Jun 8 21:17:59 gateway
```

It may take a little while for all of the services to come online; some will
pause in the `blocked` state waiting for a dependency.

# Running from Installed Packages, Debian/Raspbian

We deliver one new service on the appliance: `ap.mcp`, which starts the master
control process, which then launches all the daemons.  It is controlled by
systemd; thus:


```
$ sudo systemctl start ap.mcp
```

and any other systemd subcommand.  Note that the package postinstall scripts
automatically start the services, as well as stop `avahi-daemon`, so you don't
need to do any of that manually as you do when running from the proto area.

The services set up `ap.mcp` to log to `$APDATA/mcp.log`.  It will be created
read-only by root, so you'll either have to use `sudo` to read it, or use
`chmod` to change its permissions once it's been created.

All the executables are installed in `/opt/com.brightgate/bin`, which can be
appended to `$PATH` for convenience.

# Return to the Wiki

Once this is working, return to the Wiki to learn how to configure the AP;
for example, enabling WAN-facing SSH and configuring SSIDs and EAP users.

# External assets

- Images
  - [Exclamation Mark](https://pixabay.com/en/attention-warning-exclamation-mark-98513/)
        Creative Commons: Free for commercial use, no attribution required
  - [Raspberry Pi Glamour](https://commons.wikimedia.org/wiki/File:Raspberry_Pi_3_illustration.svg)
    client-web/public/img/rpi3-glamour.png
        Creative Commons Attribution-Share Alike 4.0 International license.
- Assets in client-web:
  - `(cd client-web && npm run licenses)`
