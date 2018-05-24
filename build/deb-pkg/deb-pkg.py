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

import argparse
import logging
import os
import shutil
import sys
import time

import sh

logging.basicConfig(level=logging.DEBUG,
                    format="%(asctime)s %(levelname)s %(message)s")

TEMPLATE = """
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

class WrongArchException(Exception):
    """ Indicates that the package cannot be constructed for this arch. """
    pass

class DebPackage:
    """ Base class for debian packages.  Subclass for each specific package"""
    name = None
    description = None
    arches = []
    depends = None
    maintainer = None
    proto_dir = None

    def __init__(self, arch, version):
        self.version = version
        assert len(self.arches) > 0
        if not arch in self.arches:
            raise WrongArchException("%s not a supported architecture" % arch)
        self.arch = arch
        self.work_dir = "%s_%s_%s" % (self.name, self.version, self.arch)
        self.package_name = "%s_%s_%s.deb" % (self.name, self.version, self.arch)
        self.debian_dir = os.path.join(self.work_dir, "DEBIAN")

    def rm_work_dir(self):
        """Delete the package's work directory."""
        try:
            if os.path.exists(self.work_dir):
                shutil.rmtree(self.work_dir)
        except Exception as e:
            logging.warning("rmtree %s: %s", self.work_dir, e)

    def mk_work_dir(self):
        """Make the package's work directory."""
        try:
            os.makedirs(self.debian_dir)
        except Exception as e:
            logging.warning("makedirs %s: %s", self.work_dir, e)

    def copy_tree(self):
        """Copy package contents, as subtree, from proto area."""
        shutil.copytree(self.proto_dir, self.work_dir)

    def emit_metadata(self):
        """Write the package metadata files."""
        depends = ",".join(self.depends)

        controlf = open(os.path.join(self.debian_dir, "control"), "w")
        print(TEMPLATE % (self.name, self.version, self.arch, depends,
                          self.maintainer, self.description),
              file=controlf)
        controlf.close()

        # copy files in build matching name-* to /DEBIAN/*
        for f in ["prerm", "postinst"]:
            src = "build/deb-pkg/%s-%s" % (self.name, f)
            if os.path.exists(src):
                dst = os.path.join(self.debian_dir, f)
                shutil.copyfile(src, dst)
                os.chmod(dst, 0o755)

    def collect_contents(self):
        """Set up the package's complete contents."""
        self.rm_work_dir()
        self.copy_tree()
        self.mk_work_dir()
        self.emit_metadata()

    def build_package(self, compresstype="xz", compresslevel=6):
        """Invoke the appropriate package build utility."""
        sh.fakeroot("dpkg-deb", "-Z", compresstype, "-z", compresslevel,
                    "--build", self.work_dir, _fg=True)
        self.rm_work_dir()

    def run_lint(self):
        """Run lintian against the constructed package."""
        try:
            sh.lintian("--no-tag-display-limit", self.package_name, _fg=True)
        except sh.ErrorReturnCode_1:
            logging.warning("lintian %s returned 1", self.package_name)

class CloudDebPackage(DebPackage):
    """Class representing bg-cloud debian package.  Packages cloud software."""
    name = "bg-cloud"
    arches = ["amd64"]
    description = """Cloud components."""
    maintainer = "Brightgate Software <contact_us@brightgate.com>"
    depends = [
        "libc6"
    ]

    def __init__(self, arch, version):
        super().__init__(arch, version)
        self.proto_dir = "proto.%s/cloud" % ARCH_MAPS[arch]

    def check_proto(self):
        pass

class ApplianceDebPackage(DebPackage):
    """Class representing bg-appliance debian package.  Packages all appliance
       software."""
    name = "bg-appliance"
    arches = ["armhf", "amd64"]
    description = """Appliance components."""
    maintainer = "Brightgate Software <contact_us@brightgate.com>"
    depends = [
        "bridge-utils",
        "chrony",
        "dhcpcd5",
        "hostapd-bg",
        "iproute2",
        "iptables",
        "iptables-persistent",
        "iw",
        "libc6",
        "libpcap-dev",
        "libzmq3-dev",
        "netfilter-persistent",
        "nmap",
        "procps",
        "vlan"
    ]

    def __init__(self, arch, version):
        super().__init__(arch, version)
        self.proto_dir = "proto.%s/appliance" % ARCH_MAPS[arch]

    def check_proto(self):
        # prevent packaging a proto area with an initialized config store
        ap_props = "opt/com.brightgate/etc/ap_props.json"
        if os.path.exists(os.path.join(self.proto_dir, ap_props)):
            raise Exception("proto area looks dirty (found %s)" % ap_props)

def main_func():
    """Main program logic"""
    packages = [CloudDebPackage, ApplianceDebPackage]

    parser = argparse.ArgumentParser()
    parser.add_argument('--arch', '-a', required=True)
    parser.add_argument('--lint', action='store_true')
    parser.add_argument('--compresslevel', '-z', type=int, default=5)
    parser.add_argument('--compresstype', '-Z', default="gzip",
                        choices=["none", "gzip", "xz"])

    opts = parser.parse_args(sys.argv[1:])
    calver = time.strftime("%y%m%d%H%M")
    version = "0.0.%s-1" % calver

    pkgs = []
    for pclass in packages:
        try:
            p = pclass(opts.arch, version)
        except WrongArchException as e:
            logging.info("skipping %s: %s", pclass.name, e)
            continue

        try:
            logging.info("pre-build check for %s", p.package_name)
            p.check_proto()
        except Exception as e:
            logging.fatal("failed proto area check: %s", e)
            sys.exit(1)
        pkgs.append(p)

    for p in pkgs:
        logging.info("begin %s package build", p.package_name)
        p.collect_contents()
        p.build_package(compresstype=opts.compresstype,
                        compresslevel=opts.compresslevel)
        logging.info("end %s package build", p.name)

        if opts.lint:
            p.run_lint()

if __name__ == "__main__":
    main_func()
