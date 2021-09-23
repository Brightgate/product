<!--
Copyright 2020 Brightgate Inc.

This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

# Brightgate Product Software

# Directories

| Directory                       | Description
| ------------------------------- | -----------
| [base/](base)                   | Resource and Protocol Buffer message definitions
| [build/](build)                 | Scripts to do with building and provisioning
| [client-web/](client-web)       | Web-based frontend
| [doc/](doc)                     | HTML product documentation
| [golang/src/bg/](golang/src/bg) | Golang-based command, daemon, and library implementations

## Directory naming

Name directories in snake case, as Python module names may not include
`-` characters.

## The "proto" area

Objects created by the `install` target are put into a per-platform directory
called a proto area.  The directory name starts with `proto.` and has the output
of `uname -m` appended.

When dealing with appliance components, `$ROOT` is equivalent to
`./proto.$(uname -m)/appliance/`.  On an imaged system, `$ROOT` is
equivalent to `/`.  Brightgate specific components are in
`$ROOT/opt/com.brightgate`.

When dealing with cloud components, `$ROOT` is equivalent to
`./proto.$(uname -m)/cloud/`.  Brightgate specific components are in
`$ROOT/opt/net.b10e/`.

# Installing Tools

Go (golang) and Node.js are needed to build.  Current versions are:

  | Software    | Version
  | ----------- | -------
  | Go          | 1.14.6 (any 1.14.x is likely to work)
  | Node.js     | 8.11.2 (any 8.x is likely to work)

# Building Product Software

GNU `make` is used to build the software components.  Useful targets:

```shellsession
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

The build system expects Go to be installed in `/opt/net.b10e/go-<version>` on
Linux, and in `/usr/local/go` on macOS.  If Golang is not installed in the
location corresponding to your build platform, you may need to set `$GOROOT` in
your environment before running `make`.

Building on macOS is unsupported, and unlikely to work fully, if at all, at any
given point in time.  It is recommended to build on Linux, where Debian 9
(Stretch) is the only regularly tested distribution.

## Go Dependencies

Go dependencies, recorded in `go.mod` and `go.sum`, are supposed to be
relatively self-maintaining.  The `go` executable will typically update the
files with new modules when it needs to, and a run of `go mod tidy` will remove
stale ones.

When adding a new dependency, the simplest thing to do is to make the requisite
changes to your sources (import the new package, do whatever with it) and then
override `GO_MOD_FLAG` to nothing in order to allow `go` to update `go.mod` and
`go.sum` appropriately:
```shellsession
$ make GO_MOD_FLAG=
```
Without that override, you'll get the error message
`import lookup disabled by -mod=readonly`.

To update a dependency, there's a convenience target using the `DEPNAME`
variable:
```shellsession
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

```shellsession
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
```shellsession
$ make packages GOARCH=arm
```
See [`build/cross-compile/README.md`](build/cross-compile/README.md) for more
details.

## Connecting to Cloud Backend

Our appliances talk to the cloud using a mixture of methods, primarily gRPC.  At
a minimum, request that a cloud secret be provisioned for your appliance.
Install that to `$APSECRET/rpcd/cloud.secret.json` in order for your cloud
connectivity to work.  See below for the definition of `$APSECRET`, and
[`build/gcp-appliance-reg/README.md`](build/gcp-appliance-reg/README.md) for
more details.

# Appliance paths

On Debian-based appliances, we assume we can use apt and LSB directory
hierarchies to organize our files in an LSB-compatible fashion.  On
OpenWrt-based appliances, we use a shared partition (`/data`) so that we can
use an "update the inactive partition" approach.

|            | Debian                     | OpenWrt               | Proto area
|------------|----------------------------|-----------------------|-------------
| `APROOT`   | `/`                        | `/`                   | `$GITROOT/proto.$ARCH/appliance`
| `APDATA`   | `$APROOT/var/spool`        | `$APROOT/data`        | `$APROOT/var/spool`
| `APSECRET` | `$APROOT/var/spool/secret` | `$APROOT/data/secret` | `$APROOT/var/spool/secret`

`APPACKAGE` is equal to `$APROOT/opt/com.brightgate` on all platforms.

Note that `APSECRET` is a placeholder for eventual use of a TPM or other secure
storage.

# Running Appliance Software

## Running From the Proto Area

`sudo` is used to acquire privilege from the developer during testing.

The `ap.relayd` daemon forwards UDP broadcast requests between security rings.
To allow mDNS forwarding to work correctly, the Linux mDNS responder
(`avahi-daemon`) must be disabled before launching our daemons:

```shellsession
$ sudo systemctl disable avahi-daemon
$ sudo systemctl stop avahi-daemon
```

Components are installed in `$(ROOT)/opt/com.brightgate/bin`.  `ap.mcp` in that
directory is the 'master control process', and is responsible for launching and
monitoring all of our other daemons.  Unless you're running in `http-dev` mode,
`ap.mcp` must run as root for full functionality:

```shellsession
$ sudo ./proto.armv7l/appliance/opt/com.brightgate/bin/ap.mcp
aproot not set - using '/home/pi/Product/proto.armv7l/appliance/opt/com.brightgate'
2018/06/08 21:17:36 mcp.go:763: ap.mcp (3711) coming online...
2018/06/08 21:17:36 mcp.go:544: MCP online
```

Then switch to another window (so the log messages don't clutter up a terminal
you're actively using; preferably, use the `-l` flag to `ap.mcpd` to send the
log messages to the file named by its argument) and launch the remaining
daemons:

```shellsession
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

## Running From Installed Packages

We deliver one new service on the appliance: `ap.mcp`, which starts the master
control process, which then launches all the daemons.

On the Raspberry Pi development platform (running Raspbian), it is controlled by
systemd; thus:

```shellsession
$ sudo systemctl start ap.mcp
```

and any other `systemd` subcommand.

The Brightgate Model 100 runs an OpenWrt-based distribution, which uses its own
`init.d` system for managing system services.  On it, use:

```shellsession
$ sudo /etc/init.d/ap.mcp start
```

or any other valid subcommand.

Note that the package postinstall scripts automatically start the services, as
well as stop `avahi-daemon`, so you don't need to do any of that manually as you
do when running from the proto area.

The services set up `ap.mcp` to log to `$APDATA/mcp.log`.  It will be created
read-only by root, so you'll either have to use `sudo` to read it, or use
`chmod` to change its permissions once it's been created.

All the executables are installed in `/opt/com.brightgate/bin`, which can be
appended to `$PATH` for convenience.

## Appliance Configuration

Configuring the appliance is beyond the scope of this document.

# External Assets

- Images:
  - [Exclamation Mark](https://pixabay.com/en/attention-warning-exclamation-mark-98513/)
    ([client-web/public/img/attention-98513_640.png](client-web/public/img/attention-98513_640.png))
        Creative Commons: Free for commercial use, no attribution required
  - [Raspberry Pi Glamour](https://commons.wikimedia.org/wiki/File:Raspberry_Pi_3_illustration.svg)
    ([client-web/public/img/rpi3-glamour.png](client-web/public/img/rpi3-glamour.png))
        Creative Commons Attribution-Share Alike 4.0 International license.
  - [Device Icons](https://materialdesignicons.com/)
    ([client-web/public/img/devid](client-web/public/img/devid))
        [Pictogrammers Free License](https://github.com/Templarian/MaterialDesign/blob/master/LICENSE)
- Code in client-web:
  - `(cd client-web && npm run licenses)`
