
COPYRIGHT 2018 Brightgate Inc. All rights reserved.

This copyright notice is Copyright Management Information under 17 USC 1202
and is included to protect this work and deter copyright infringement.
Removal or alteration of this Copyright Management Information without the
express written permission of Brightgate Inc is prohibited, and any
such unauthorized removal or alteration will be a violation of federal law.


# Cross-compilation README

Cross-compilation allows you to build software for one architecture on another.
In our case, we'd like to build for ARM from x86, to facilitate continuous
integration (jenkins).  This supports testing and release engineering.

## Prerequisites

To cross-compile, you need a few things.  You need:
- Cross-aware toolchain.  In our case, golang supports this out-of-the-box.
- A "sysroot".  This is a filesystem image of the root of a system.  In some
  cases, go needs to use a C compiler, and in turn a sysroot is required.
  To get a sysroot, you need to build one.

## Building a sysroot

```
$ sudo apt-get install multistrap
$ cd build/cross-compile
$ ./build-multistrap-sysroot.sh raspbian-stretch.multistrap
...
```

When this is done, you will have a directory called `sysroot.raspbian-stretch`.

## Cross-compiling

Pass the sysroot to the build, giving the appropriate `GOARCH` and `GOARM` flags.

```
$ make SYSROOT=build/cross-compile/sysroot.raspbian-stretch GOARCH=arm GOARM=7 packages
```

## External resources

- https://wiki.debian.org/Multistrap
- https://dave.cheney.net/2015/08/22/cross-compilation-with-go-1-5

## Future work

- Investigate https://github.com/karalabe/xgo
- Build and publish a sysroot which can be downloaded as needed
- Set GOARM?  Needs research.
