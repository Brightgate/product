#!/usr/bin/python3
# -*- coding: utf-8 -*-
#
# COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

"""build Debian packages from product software"""

import getopt
import logging
import os
import shutil
import sys
import time

import sh

logging.basicConfig(level=logging.DEBUG,
                    format="%(asctime)s %(levelname)s %(message)s")

template = """
Package: %s
Version: %s
Section: non-free/embedded
Priority: optional
Architecture: %s
Depends: %s
Maintainer: %s
Description: %s
"""

ARCH_MAPS = {
    "armhf": "armv7l",
    "amd64": "x86_64"
}

class DebPackage:
    """Operations for building a Debian binary package."""

    def __init__(self, name, version, arches, tree_fmt, depends, description):
        self.name = name
        self.version = version
        self.arches = arches
        self.tree_fmt = tree_fmt
        self.depends = depends
        self.description = description

    def work_dir(self, arch):
        """Generate the package's work directory."""
        return "%s_%s_%s" % (self.name, self.version, arch)

    def package_name(self, arch):
        """Generate the package's filename."""
        return "%s_%s_%s.deb" % (self.name, self.version, arch)

    def rm_work_dir(self, arch):
        """Delete the package's work directory."""
        try:
            shutil.rmtree(self.work_dir(arch))
        except Exception as e:
            logging.warning("rmtree %s: %s", self.work_dir(arch), e)

    def mk_work_dir(self, arch):
        """Make the package's work directory."""
        try:
            os.makedirs(self.work_dir(arch) + "/DEBIAN")
        except Exception as e:
            logging.warning("makedirs %s: %s", self.work_dir(arch), e)

    def copy_tree(self, arch):
        """Copy package contents, as subtree, from proto area."""
        shutil.copytree(self.tree_fmt % ARCH_MAPS[arch], self.work_dir(arch))

    def emit_metadata(self, arch):
        """Write the package metadata files."""
        depends = ",".join(self.depends)

        controlf = open(self.work_dir(arch) + "/DEBIAN/control", "w")
        print(template % (self.name, self.version, arch, depends,
                          "Brightgate Software <contact_us@brightgate.com>",
                          self.description),
              file=controlf)
        controlf.close()

        # copy files in build matching name-* to /DEBIAN/*
        for f in ["prerm", "postinst"]:
            src = "build/%s-%s" % (self.name, f)
            if os.path.exists(src):
                dst = self.work_dir(arch) + "/DEBIAN/" + f
                shutil.copyfile(src, dst)
                os.chmod(dst, 0o755)

    def collect_contents(self, arch=None):
        """Set up the package's complete contents."""
        self.rm_work_dir(arch)
        self.copy_tree(arch)
        self.mk_work_dir(arch)
        self.emit_metadata(arch)

    def build_package(self, arch=None, compresstype="xz", compresslevel=6):
        """Invoke the appropriate package build utility."""
        sh.fakeroot("dpkg-deb", "-Z", compresstype, "-z", compresslevel,
                    "--build", self.work_dir(arch), _fg=True)
        self.rm_work_dir(arch)

    def run_lint(self, arch):
        """Run lintian against the constructed package."""
        try:
            sh.lintian("--no-tag-display-limit", self.package_name(arch),
                       _fg=True)
        except sh.ErrorReturnCode_1:
            logging.warning("lintian %s returned 1", self.package_name(arch))

# Package definitions.
calver = time.strftime("%y%m%d%H%M")

packages = [
    DebPackage("bg-cloud", "0.0.%s-1" % calver, "amd64", "proto.%s/cloud",
        ["libc6"],
        """Cloud components."""),
    DebPackage("bg-appliance", "0.0.%s-1" % calver, ["armhf", "amd64"],
        "proto.%s/appliance",
        ["bridge-utils", "hostapd", "libc6", "libzmq3-dev", "libpcap-dev"],
        """Appliance components.""")
    ]

if __name__ == "__main__":
    do_lint = False
    compresstype = "gzip"
    compresslevel = 5

    opts, pargs = getopt.getopt(sys.argv[1:], "a:Z:z:",
            longopts=["arch=", "compresstype=", "compresslevel=", "lint"])

    for opt, arg in opts:
        if opt == "-a" or opt == "--arch":
            arch = arg
        elif opt == "--lint":
            do_lint = True
        elif opt == "-z" or opt == "--compresslevel":
            compresslevel = arg
        elif opt == "-Z" or opt == "--compresstype":
            if arg not in ["none", "gzip", "xz"]:
                logging.error("unrecognized compression type '%s'", arg)
                sys.exit(2)
            compresstype = arg

    for p in packages:
        if arch not in p.arches:
            logging.info("skipping %s for %s (supports %s)", arch, p.name,
                         p.arches)
            continue

        logging.info("begin %s package build", p.name)
        p.collect_contents(arch=arch)
        p.build_package(arch=arch, compresstype=compresstype,
                        compresslevel=compresslevel)
        logging.info("end %s package build", p.name)

        if do_lint:
            p.run_lint(arch)
