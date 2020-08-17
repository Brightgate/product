
Copyright 2020 Brightgate Inc.

This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.


# Cross-compilation README

Cross-compilation allows you to build software for one architecture on another.
In our case, we'd like to build for ARM from x86, to facilitate continuous
integration (jenkins).  This supports testing and release engineering.

## Prerequisites

To cross-compile, you need a few things.  You need:
- Cross-aware toolchain.  In our case, golang supports cross-compilation
  out-of-the-box.  Our use of `cgo` however requires that a C/C++ cross-aware
  toolchain is also available.
- A "sysroot".  This is a filesystem image of the root of a system.  In some
  cases, go needs to use a C compiler, and in turn a sysroot is required.
  To get a sysroot, you need to build one.

The toolchain should be installed on build machines by default; if the tools
are not available, the build will complain and fail.  The sysroot is built
automatically and stored in the cloud; it will be downloaded and used as
required.

## Building an OpenWrt sysroot

An output product of the OpenWrt build is a root filesystem tree, which is
suitable for use as a sysroot.  The cross-compiling C/C++ toolchain (for
`arm-openwrt-gnueabi`) is also an output product of the full OpenWrt build.

Instructions for building the Brightgate OpenWrt distribution are given in the
README.md for the rWRT repository.

## Building a Raspbian sysroot [OBSOLETE]

```
$ sudo apt-get install multistrap
$ cd build/cross-compile
$ ./build-multistrap-sysroot.sh raspbian-stretch.multistrap
...
```

When this is done, you will have a directory called `sysroot.raspbian-stretch`.
You can do this in one step, from the top-level directory:
```
$ make build-sysroot
```

## Cross-compiling

Pass the target distribution to the build via the `DISTRO` flag, giving the
appropriate `GOARCH` and `GOARM` flags.  In addition to using the appropriate
sysroot, the target distro is used to build the appropriate packages.

For the OpenWrt build, use 'openwrt' as the distro:

```
$ make DISTRO=openwrt GOARCH=arm GOARM=7 packages
```

For Raspbian, use 'debian' as the distro:

```
$ make DISTRO=debian GOARCH=arm GOARM=7 packages
```

## External resources

- https://wiki.debian.org/Multistrap
- https://dave.cheney.net/2015/08/22/cross-compilation-with-go-1-5

## Future work

- Investigate https://github.com/karalabe/xgo
- Set GOARM?  Needs research.
