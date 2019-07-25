#!/usr/bin/python3
# -*- coding: utf-8 -*-
#
# COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

"""build Debian packages from product software"""
"""build tar archives from product software"""

import argparse
import logging
import os
import shutil
import sys
import time
import typing

import mako.exceptions
import mako.template
import sh

logging.basicConfig(level=logging.DEBUG,
                    format="%(asctime)s %(levelname)s %(message)s")

ARCH_MAPS = {
    "armhf": {
        "ipk": "arm_cortex-a7_neon-vfpv4",
    },
    "amd64": {
        "ipk": "amd64",
    },
}

deb_template = mako.template.Template("""\
Package: ${pkg_name}
Version: ${pkg_version}
Section: ${pkg_section}
Priority: ${pkg_priority}
Architecture: ${pkg_arch}
% if pkg_depends:
Depends: ${pkg_depends}
% endif
Maintainer: ${pkg_maintainer}
Description: ${pkg_description}""")

ipk_template = mako.template.Template("""\
Package: ${pkg_name}
Version: ${pkg_version}
Section: ${pkg_section}
Architecture: ${pkg_arch}
% if pkg_depends:
Depends: ${pkg_depends}
% endif
Maintainer: ${pkg_maintainer}
Description: ${pkg_description}""")


class WrongArchException(Exception):
    """ Indicates that the package cannot be constructed for this arch. """
    pass


def valid_distro_arch(distro, arch) -> bool:
    if distro == "debian" and arch in ["armhf", "amd64"]:
        return True

    if distro == "openwrt" and arch == "armhf":
        return True

    if distro == "archive":
        return True

    return False


class Package:
    """ Base class for packages/archives.  Subclass for each specific package"""
    distro = None
    name = None
    description = None
    arches = []
    depends = None
    maintainer = None
    proto_dir = None

    def __init__(self, distro, arch, version):
        self.distro = distro
        self.arch = arch
        self.version = version

        assert len(self.arches) > 0
        if not arch in self.arches:
            raise WrongArchException("%s not a supported architecture" % arch)

        self.deb_package_name = "%s_%s_%s.deb" % (self.name, self.version, self.arch)
        self.ipk_package_name = "%s_%s_%s.ipk" % (self.name, self.version, ARCH_MAPS[self.arch]["ipk"])

        self.set_distro(distro)

    def set_distro(self, distro):
        self.distro = distro

        if distro == "debian":
            self.package_name = self.deb_package_name

            self.pkg_arch = self.arch
            self.work_dir = "%s_%s_%s" % (self.name, self.version, self.pkg_arch)
            self.deb_dir = os.path.join(self.work_dir, "DEBIAN")
            self.control_dir = self.deb_dir

            self._depends = self.deb_depends
            self._template = deb_template
        elif distro == "openwrt":
            self.package_name = self.ipk_package_name

            self.pkg_arch = ARCH_MAPS[self.arch]["ipk"]
            self.work_dir = "%s_%s_%s" % (self.name, self.version, self.pkg_arch)
            self.ipk_dir = os.path.join(self.work_dir, "CONTROL")
            self.control_dir = self.ipk_dir

            self._depends = self.ipk_depends
            self._template = ipk_template
        elif distro == "archive":
            raise RuntimeError("archive not yet implemented")
        else:
            raise RuntimeError("unknown distro")

    def rm_work_dir(self):
        """Delete the package's work directory."""
        try:
            if os.path.exists(self.work_dir):
                shutil.rmtree(self.work_dir)
        except Exception as e:
            logging.warning("rmtree %s: %s", self.work_dir, e)

    def mk_work_dir(self):
        """Make the package's work and control directories."""
        try:
            os.makedirs(self.control_dir)
        except Exception as e:
            logging.warning("makedirs %s: %s", self.control_dir, e)

    def copy_tree(self):
        """Copy package contents, as subtree, from proto area."""
        shutil.copytree(self.proto_dir, self.work_dir, symlinks=True)

    def emit_metadata(self):
        """Write the package metadata files."""
        depends = ",".join(self._depends)

        controlf = open(os.path.join(self.control_dir, "control"), "w")
        try:
            controlmsg = self._template.render(
                pkg_name=self.name,
                pkg_version=self.version,
                pkg_section="opt",
                pkg_arch=self.pkg_arch,
                pkg_depends=depends,
                pkg_maintainer=self.maintainer,
                pkg_description=self.description,
                pkg_priority="optional",
            )
            print(controlmsg, file=controlf)
        except:
            logging.error("control template issue: %s",
                          mako.exceptions.text_error_template().render())

        controlf.close()

        if self.distro == "debian":
            for f in ["prerm", "postinst", "conffiles"]:
                src = "build/debian-deb/%s-%s" % (self.name, f)
                if os.path.exists(src):
                    dst = os.path.join(self.control_dir, f)
                    shutil.copyfile(src, dst)
                    if f != "conffiles":
                        os.chmod(dst, 0o755)
        elif self.distro == "openwrt":
            for f in ["prerm", "postinst", "conffiles"]:
                src = "build/openwrt-ipk/%s-%s" % (self.name, f)
                if os.path.exists(src):
                    dst = os.path.join(self.control_dir, f)
                    shutil.copyfile(src, dst)
                    if f != "conffiles":
                        os.chmod(dst, 0o755)

    def collect_contents(self):
        """Set up the package's complete contents."""
        self.rm_work_dir()
        self.copy_tree()
        self.mk_work_dir()
        self.emit_metadata()

    def build_package(self, compresstype="gzip", compresslevel=5):
        """Invoke the appropriate package build utility."""

        if self.distro == "debian":
            sh.fakeroot("dpkg-deb", "-Z", compresstype, "-z", compresslevel,
                        "--build", self.work_dir, _fg=True)
        elif self.distro == "openwrt":
            sh.fakeroot("./build/openwrt-ipk/ipkg-build", self.work_dir, _fg=True)
        elif self.distro == "archive":
            sh.fakeroot("tar", "zcvf", self.archive_name, self.work_dir, _fg=True)

        self.rm_work_dir()

    def deb_run_lint(self):
        """Run lintian against the constructed package."""
        try:
            sh.lintian("--no-tag-display-limit", self.package_name, _fg=True)
        except sh.ErrorReturnCode_1:
            logging.warning("lintian %s returned 1", self.package_name)


class CloudPackage(Package):
    """Class representing bg-cloud package.  Packages cloud software."""
    name = "bg-cloud"
    arches = ["amd64"]
    distros = ["archive", "debian"]
    description = """Cloud components."""
    maintainer = "Brightgate Software <contact_us@brightgate.com>"
    deb_depends = [
        "libc6"
    ]

    def __init__(self, distro, arch, version, proto):
        logging.debug("cloud package distro = %s, arch = %s, version = %s, proto = %s",
            distro, arch, version, proto)
        super().__init__(distro, arch, version)
        self.proto_dir = os.path.join(proto, "cloud")

    def check_proto(self):
        pass


class AppliancePackage(Package):
    """Class representing bg-appliance package.  Packages all appliance
       software."""
    name = "bg-appliance"
    arches = ["armhf", "amd64"]
    distros = ["archive", "debian", "openwrt"]
    description = """Appliance components."""
    maintainer = "Brightgate Software <contact_us@brightgate.com>"
    deb_depends = [
        "bridge-utils",
        "chrony",
        "dhcpcd5",
        "iproute2",
        "iptables",
        "iptables-persistent",
        "iw",
        "libc6",
        "libpcap-dev",
        "netfilter-persistent",
        "nmap",
        "procps",
        "vlan",

        "bg-hostapd",
    ]
    ipk_depends = [
        "libgcc",
        "libpcap",
        "libsodium",
        "uclibcxx",

        "chrony",
        "iw-full",
        "logrotate",
        "nmap-ssl",
        "rsyslog",

        "bg-hostapd",
    ]

    def __init__(self, distro, arch, version, proto):
        logging.debug("appliance package distro = %s, arch = %s, version = %s, proto = %s",
            distro, arch, version, proto)
        super().__init__(distro, arch, version)
        self.proto_dir = os.path.join(proto, "appliance")

    def check_proto(self):
        # prevent packaging a proto area with an initialized config store
        ap_props = [
            "opt/com.brightgate/etc/ap_props.json"
            "data/configd/ap_props.json"
        ]
        for p in ap_props:
            if os.path.exists(os.path.join(self.proto_dir, p)):
                raise Exception("proto area looks dirty (found %s)" % p)


def main_func():
    """Main program logic"""
    packages = [CloudPackage, AppliancePackage]

    parser = argparse.ArgumentParser()
    parser.add_argument('--arch', '-A', required=True)
    parser.add_argument('--distro', '-D', required=True)
    parser.add_argument('--proto', '-p', required=True)
    parser.add_argument('--lint', action='store_true')
    parser.add_argument('--compresslevel', '-z', type=int, default=5)
    parser.add_argument('--compresstype', '-Z', default="gzip",
                        choices=["none", "gzip", "xz"])

    opts = parser.parse_args(sys.argv[1:])
    calver = time.strftime("%y%m%d%H%M")
    version = "0.0.%s-1" % calver

    if opts.distro == "raspbian":
        logging.info("auto-correcting 'raspbian' distro to 'debian'")
        opts.distro = "debian"

    if not valid_distro_arch(opts.distro, opts.arch):
        logging.info("no package output for %s on %s", opts.arch, opts.distro)
        sys.exit(0)

    pkgs = []
    for pclass in packages:
        try:
            p = pclass(opts.distro, opts.arch, version, opts.proto)
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
