#
# COPYRIGHT 2018 Brightgate Inc. All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

# 1. (a) On MacOS
#
#	 $ sudo brew install protobuf zmq
#	 [Retrieve and install Go pkg from golang.org]
#
#	 You will not be able to build the packages target on MacOS.
#
#    (b) On Debian/Ubuntu
#
#	 $ make tools
#	 [Follow the directions]
#	 [If in the Brightgate cloud, all tools should be installed.  Else,
#	  follow the directions at
#	  https://ph0.b10e.net/w/testing-raspberry-pi/#installing-prerequisite
#	  (and modify from ARM as necessary) to install other build requirements]
#
#    (c) On Raspberry Pi
#
#	 $ make tools
#	 [Follow the directions]
#	 [Follow the directions at
#	  https://ph0.b10e.net/w/testing-raspberry-pi/#installing-prerequisite
#	  to install other build requirements]
#
# 2. To clean out local binaries, use
#
#	 $ make clobber
#
# 3. On x86_64, the build constructs all components, whether for appliance or
#    for cloud.  On ARM, only appliance components are built.

#
# OS definitions
#
# note: These are constants; := avoids repeated shell invocations
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

#
# Git related definitions
#
# note: These are constants; := avoids repeated shell invocations
GITROOT := $(shell git rev-parse --show-toplevel)
GITHASH := $(shell git describe --always --long --dirty)
# Needs deferred expansion, can't use :=
GITCHANGED = $(shell grep -s -q '"$(GITHASH)"' $(GOSRCBG)/common/version.go || echo FRC)

export GOPATH=$(GITROOT)/golang

#
# Go environment setup
#
ifeq ("$(GOROOT)","")
ifeq ("$(UNAME_S)","Darwin")
# On macOS, install the .pkg provided by golang.org.
export GOROOT=/usr/local/go
GOROOT_SEARCH += /usr/local/go
$(info operating-system macOS)
else
# On Linux
export GOROOT=$(wildcard /opt/net.b10e/go-1.10.2)
GOROOT_SEARCH += /opt/net.b10e/go-1.10.2
ifeq ("$(GOROOT)","")
export GOROOT=$(HOME)/go
GOROOT_SEARCH += $(HOME)/go
endif
$(info operating-system Linux)
endif
endif

export PATH=$(GOPATH)/bin:$(GOROOT)/bin:$(shell echo $$PATH)

GO = $(GOROOT)/bin/go
GOFMT = $(GOROOT)/bin/gofmt
GOLINT = $(GOROOT)/bin/golint
GO_CLEAN_FLAGS = -i -x
GO_GET_FLAGS = -v

ifeq ("$(wildcard $(GO))","")
ifeq ("$(findstring $(origin GOROOT), "command line|environment")","")
$(error go does not exist in any known $$GOROOT: $(GOROOT_SEARCH))
else
$(error go does not exist at specified $$GOROOT: $(GOROOT))
endif
endif

# note: These are constants; := avoids repeated invocation of shell
GOOS := $(shell GOROOT=$(GOROOT) $(GO) env GOOS)
GOARCH := $(shell GOROOT=$(GOROOT) $(GO) env GOARCH)
GOHOSTARCH := $(shell GOROOT=$(GOROOT) $(GO) env GOHOSTARCH)
GOVERSION := $(shell GOROOT=$(GOROOT) $(GO) version)

GOWS = golang
GOSRC = $(GOWS)/src
GOSRCBG = $(GOSRC)/bg
# Vendoring directory, where external deps are placed
GOSRCBGVENDOR = $(GOSRCBG)/vendor
# Where we stick build tools
GOBIN = $(GOPATH)/bin

#
# Miscellaneous environment setup
#
INSTALL = install
ifeq ("$(UNAME_S)","Darwin")
SHA256SUM = shasum -a 256
else
SHA256SUM = sha256sum
endif
MKDIR = mkdir
RM = rm

PYTHON3 = python3
PYTHON3VERSION = $(shell $(PYTHON3) -V)

NODE = node
NODEVERSION = $(shell $(NODE) --version)

#
# ARCH dependent setup
# - Select proto area name
# - Select default target list
#
ifeq ("$(GOARCH)","amd64")
ROOT=proto.$(UNAME_M)
PKG_DEB_ARCH=amd64
TARGETS=appliance cloud
endif

ifeq ("$(GOARCH)","arm")
# UNAME_M will read armv7l on Raspbian and on Ubuntu for Banana Pi.
# Both use armhf as the architecture for .deb files.
ROOT=proto.armv7l
PKG_DEB_ARCH=armhf
TARGETS=appliance
endif

#
# Cross compilation setup
#
SYSROOT_CFG = build/cross-compile/raspbian-stretch.multistrap
SYSROOT_CFG_LOCAL = $(subst build/cross-compile/,,$(SYSROOT_CFG))
ifneq ($(GOHOSTARCH),$(GOARCH))
SYSROOT = build/cross-compile/sysroot.$(GOARCH).$(SYSROOT_SUM)
ifeq ($(GOARCH),arm)
BUILDTOOLS_CROSS = crossbuild-essential-armhf
else
$(error 'arm' is the only supported cross target)
endif

# SYSROOT doesn't work right if isn't an absolute path.  We need to use the
# external realpath so that we can tell it to ignore the possibility that the
# path and its components don't exist.
CROSS_SYSROOT = $(shell realpath -m $(SYSROOT))
CROSS_CC = /usr/bin/arm-linux-gnueabihf-gcc
CROSS_CGO_LDFLAGS = --sysroot $(CROSS_SYSROOT) -Lusr/local/lib
CROSS_CGO_CFLAGS = --sysroot $(CROSS_SYSROOT) -Iusr/local/include

CROSS_ENV = \
	SYSROOT=$(CROSS_SYSROOT) \
	CC=$(CROSS_CC) \
	CGO_LDFLAGS="$(CROSS_CGO_LDFLAGS)" \
	CGO_CFLAGS="$(CROSS_CGO_CFLAGS)" \
	CGO_ENABLED=1
CROSS_DEP = $(SYSROOT)/.$(SYSROOT_SUM)
endif

# This is the checksum of the sysroot blob to be used; it's outside the
# cross-compile conditional because the build-sysroot target uses this to see
# whether the sysroot has changed and needs to be re-uploaded.
SYSROOT_SUM=a66af97cd6bbab3fc23eba227b663789b266be7c6efa1da68160d3b46c2d1f44

# The command used to build the sysroot.
BUILD_SYSROOT_CMD = \
	cd build/cross-compile && \
	SYSROOT=$(SYSROOT) SYSROOT_SUM=$(SYSROOT_SUM) \
	    ./build-multistrap-sysroot.sh -f $(SYSROOT_CFG_LOCAL)

SYSROOT_BLOB_NAME = $(shell $(BUILD_SYSROOT_CMD) name)

BUILDTOOLS = \
	$(BUILDTOOLS_CROSS) \
	protobuf-compiler \
	libprotobuf-dev \
	libzmq3-dev \
	libpcap-dev \
	lintian \
	python3 \
	python3-pip \
	mercurial

#
# Announce some things about the build
#
define report
#        TARGETS: $(TARGETS)
#         KERNEL: UNAME_S=$(UNAME_S) UNAME_M=$(UNAME_M)
#        GITHASH: $(GITHASH)
#             GO: $(GO)
#      GOVERSION: $(GOVERSION)
#         GOROOT: $(GOROOT)
#         GOPATH: $(GOPATH)
#           GOOS: $(GOOS)
#     GOHOSTARCH: $(GOHOSTARCH)
#         GOARCH: $(GOARCH)
# PYTHON3VERSION: $(PYTHON3VERSION)
#    NODEVERSION: $(NODEVERSION)
endef
$(info $(report))
undefine report
ifneq ($(GOHOSTARCH),$(GOARCH))
define report
#     CROSSBUILD: $(GOHOSTARCH) -> $(GOARCH)
#        SYSROOT: $(SYSROOT)
#  CROSS_SYSROOT: $(CROSS_SYSROOT)
#    SYSROOT_SUM: $(SYSROOT_SUM)
endef
$(info $(report))
undefine report
endif

#
# Appliance components and supporting definitions
#

APPROOT=$(ROOT)/appliance
APPBASE=$(APPROOT)/opt/com.brightgate
APPBIN=$(APPBASE)/bin
APPSNMAP=$(APPBASE)/share/nmap/scripts
# APPCSS
# APPJS
# APPHTML
APPETC=$(APPBASE)/etc
APPROOTLIB=$(APPROOT)/lib
APPVAR=$(APPBASE)/var
APPSECRET=$(APPETC)/secret
APPSECRETCLOUD=$(APPSECRET)/cloud
APPSECRETSSL=$(APPSECRET)/ssl
APPSPOOL=$(APPVAR)/spool
APPSPOOLANTIPHISH=$(APPVAR)/spool/antiphishing
APPSPOOLWATCHD=$(APPVAR)/spool/watchd
APPRULES=$(APPETC)/filter.rules.d
APPMODEL=$(APPETC)/device_model

ROOTETC=$(APPROOT)/etc
ROOTETCIPTABLES=$(ROOTETC)/iptables
ROOTETCLOGROTATED=$(ROOTETC)/logrotate.d
ROOTETCRSYSLOGD=$(ROOTETC)/rsyslog.d

HTTPD_CLIENTWEB_DIR=$(APPVAR)/www/client-web
NETWORKD_TEMPLATE_DIR=$(APPETC)/templates/ap.networkd
USERAUTHD_TEMPLATE_DIR=$(APPETC)/templates/ap.userauthd

COMMON_GOPKGS = \
	bg/common/archive \
	bg/common/briefpg \
	bg/common/cfgapi \
	bg/common/cfgmsg \
	bg/common/cfgtree \
	bg/common/configctl \
	bg/common/grpcutils \
	bg/common/urlfetch

COMMON_SRCS = \
	$(GOSRCBG)/common/archive/archive.go \
	$(GOSRCBG)/common/cfgapi/cfgapi.go \
	$(GOSRCBG)/common/cfgapi/users.go \
	$(GOSRCBG)/common/cfgmsg/cfgmsg.go \
	$(GOSRCBG)/common/cfgmsg/cfgmsg.pb.go \
	$(GOSRCBG)/common/cfgtree/cfgtree.go \
	$(GOSRCBG)/common/grpcutils/client.go \
	$(GOSRCBG)/common/grpcutils/cred.go \
	$(GOSRCBG)/common/urlfetch/urlfetch.go

APPCOMMON_GOPKGS = \
	$(COMMON_GOPKGS) \
	bg/ap_common/apcfg \
	bg/ap_common/aptest \
	bg/ap_common/aputil \
	bg/ap_common/broker \
	bg/ap_common/certificate \
	bg/ap_common/device \
	bg/ap_common/mcp \
	bg/ap_common/model \
	bg/ap_common/network \
	bg/ap_common/platform \
	bg/ap_common/wificaps

APPCOMMAND_GOPKGS = \
	bg/ap-arpspoof \
	bg/ap-certcheck \
	bg/ap-complete \
	bg/ap-configctl \
	bg/ap-ctl \
	bg/ap-defaultpass \
	bg/ap-diag \
	bg/ap-inspect \
	bg/ap-factory \
	bg/ap-msgping \
	bg/ap-ouisearch \
	bg/ap-userctl \
	bg/ap-vuln-aggregate \
	bg/ap-watchctl

APPDAEMON_GOPKGS = \
	bg/ap.brokerd \
	bg/ap.configd \
	bg/ap.dhcp4d \
	bg/ap.dns4d \
	bg/ap.httpd \
	bg/ap.identifierd \
	bg/ap.logd \
	bg/ap.mcp \
	bg/ap.networkd \
	bg/ap.relayd \
	bg/ap.rpcd \
	bg/ap.updated \
	bg/ap.userauthd \
	bg/ap.watchd

ALL_GOPKGS = $(APP_GOPKGS) $(CLOUD_GOPKGS)

APP_GOPKGS = $(APPCOMMON_GOPKGS) $(APPCOMMAND_GOPKGS) $(APPDAEMON_GOPKGS)

MISCCOMMANDS = \
	ap-rpc

APPBINARIES = \
	$(APPCOMMAND_GOPKGS:bg/%=$(APPBIN)/%) \
	$(APPDAEMON_GOPKGS:bg/%=$(APPBIN)/%) \
	$(MISCCOMMANDS:%=$(APPBIN)/%)

# XXX Common configurations?

GO_AP_TESTABLES = \
	bg/ap_common/certificate \
	bg/ap_common/network \
	bg/ap.configd \
	bg/ap-defaultpass\
	bg/ap.networkd \
	bg/ap.rpcd \
	bg/ap.updated \
	bg/ap.userauthd \
	bg/common/grpcutils

GO_CLOUD_TESTABLES = \
	bg/cl_common/auth/m2mauth \
	bg/cl_common/daemonutils \
	bg/cloud_models/appliancedb \
	bg/cloud_models/sessiondb \
	bg/cl.httpd

NETWORKD_TEMPLATE_FILES = \
	hostapd.conf.got \
	chrony.conf.got

USERAUTHD_TEMPLATE_FILES = \
	hostapd.radius.got \
	hostapd.radius_clients.got \
	hostapd.users.got

NETWORKD_TEMPLATES = $(NETWORKD_TEMPLATE_FILES:%=$(NETWORKD_TEMPLATE_DIR)/%)
USERAUTHD_TEMPLATES = $(USERAUTHD_TEMPLATE_FILES:%=$(USERAUTHD_TEMPLATE_DIR)/%)
APPTEMPLATES = $(NETWORKD_TEMPLATES) $(USERAUTHD_TEMPLATES)

FILTER_RULES = \
	$(APPRULES)/base.rules \
	$(APPRULES)/local.rules \
	$(APPRULES)/relayd.rules

APPCONFIGS = \
	$(APPETC)/ap_identities.csv \
	$(APPETC)/vendordefaults.csv \
	$(APPETC)/ap_mfgid.json \
	$(APPETC)/configd.json \
	$(APPETC)/devices.json \
	$(APPETC)/mcp.json \
	$(APPETC)/oui.txt \
	$(APPETC)/prometheus.yml \
	$(APPROOTLIB)/systemd/system/ap.mcp.service \
	$(APPROOTLIB)/systemd/system/brightgate-appliance.service \
	$(APPSNMAP)/smb-vuln-ms17-010.nse \
	$(APPSPOOLWATCHD)/vuln-db.json \
	$(ROOTETCIPTABLES)/rules.v4 \
	$(ROOTETCRSYSLOGD)/com-brightgate-rsyslog.conf \
	$(ROOTETCLOGROTATED)/com-brightgate-logrotate-logd \
	$(ROOTETCLOGROTATED)/com-brightgate-logrotate-mcp

APPDIRS = \
	$(APPBIN) \
	$(APPETC) \
	$(APPROOTLIB) \
	$(APPRULES) \
	$(APPSECRET) \
	$(APPSECRETCLOUD) \
	$(APPSECRETSSL) \
	$(APPSNMAP) \
	$(APPSPOOL) \
	$(APPVAR) \
	$(APPSPOOLANTIPHISH) \
	$(APPSPOOLWATCHD) \
	$(HTTPD_CLIENTWEB_DIR) \
	$(NETWORKD_TEMPLATE_DIR) \
	$(ROOTETC) \
	$(ROOTETCIPTABLES) \
	$(ROOTETCLOGROTATED) \
	$(ROOTETCRSYSLOGD) \
	$(USERAUTHD_TEMPLATE_DIR)

APPCOMPONENTS = \
	$(APPBINARIES) \
	$(APPCONFIGS) \
	$(APPDIRS) \
	$(APPMODEL) \
	$(APPTEMPLATES) \
	$(FILTER_RULES)

APP_COMMON_SRCS = \
	$(COMMON_SRCS) \
	$(GOSRCBG)/ap_common/apcfg/apcfg.go \
	$(GOSRCBG)/ap_common/apcfg/events.go \
	$(GOSRCBG)/ap_common/aputil/aputil.go \
	$(GOSRCBG)/ap_common/aputil/cred.go \
	$(GOSRCBG)/ap_common/aputil/logging.go \
	$(GOSRCBG)/ap_common/broker/broker.go \
	$(GOSRCBG)/ap_common/device/device.go \
	$(GOSRCBG)/ap_common/mcp/mcp_client.go \
	$(GOSRCBG)/ap_common/model/model.go \
	$(GOSRCBG)/ap_common/network/dhcp.go \
	$(GOSRCBG)/ap_common/network/network.go \
	$(GOSRCBG)/ap_common/platform/platform.go \
	$(GOSRCBG)/base_def/base_def.go \
	$(GOSRCBG)/base_msg/base_msg.pb.go

# Miscellaneous utilities

UTILROOT=$(ROOT)/util
UTILBIN=$(UTILROOT)/bin

UTILCOMMON_SRCS = \
	$(GOSRCBG)/ap_common/device/device.go \
	$(GOSRCBG)/ap_common/model/model.go \
	$(GOSRCBG)/ap_common/network/network.go \
	$(GOSRCBG)/ap_common/platform/platform.go \
	$(GOSRCBG)/base_msg/base_msg.pb.go \
	$(GOSRCBG)/util/deviceDB/device_db.go

UTILCOMMAND_SRCS = \
	bg/util/build_device_db.go \
	bg/util/model-merge.go \
	bg/util/model-sim.go \
	bg/util/model-train.go

UTILBINARIES = $(UTILCOMMAND_SRCS:bg/util/%.go=$(UTILBIN)/%)

UTILDIRS = $(UTILBIN)

UTILCOMPONENTS = $(UTILBINARIES) $(UTILDIRS)

# Cloud components and supporting definitions.

CLOUDROOT=$(ROOT)/cloud
CLOUDBASE=$(CLOUDROOT)/opt/net.b10e
CLOUDBIN=$(CLOUDBASE)/bin
CLOUDETC=$(CLOUDBASE)/etc
CLOUDLIB=$(CLOUDBASE)/lib
CLOUDLIBCLHTTPDWEB=$(CLOUDLIB)/cl.httpd-web
CLOUDLIBCLHTTPDWEBCLIENTWEB=$(CLOUDLIBCLHTTPDWEB)/client-web
CLOUDETCSCHEMA=$(CLOUDETC)/schema
CLOUDETCSCHEMAAPPLIANCEDB=$(CLOUDETCSCHEMA)/appliancedb
CLOUDROOTLIB=$(CLOUDROOT)/lib
CLOUDROOTLIBSYSTEMDSYSTEM=$(CLOUDROOTLIB)/systemd/system
CLOUDVAR=$(CLOUDBASE)/var
CLOUDSPOOL=$(CLOUDVAR)/spool

CLOUDDAEMON_GOPKGS = \
	bg/cl.configd \
	bg/cl.eventd \
	bg/cl.httpd \
	bg/cl.rpcd

CLOUDCOMMON_GOPKGS = \
	$(COMMON_GOPKGS) \
	bg/cl_common/auth/m2mauth \
	bg/cl_common/clcfg \
	bg/cl_common/daemonutils \
	bg/cl_common/pgutils \
	bg/cl_common/registry \
	bg/cloud_models/appliancedb \
	bg/cloud_models/sessiondb

CLOUDCOMMAND_GOPKGS = \
	bg/cl-aggregate \
	bg/cl-configctl \
	bg/cl-dtool \
	bg/cl-reg

CLOUD_GOPKGS = $(CLOUDCOMMON_GOPKGS) $(CLOUDDAEMON_GOPKGS) $(CLOUDCOMMAND_GOPKGS)

CLOUDDAEMONS = $(CLOUDDAEMON_GOPKGS:bg/%=%)

CLOUDCOMMANDS = $(CLOUDCOMMAND_GOPKGS:bg/%=%)

CLOUDSERVICES = \
	cl.configd.service \
	cl.eventd.service \
	cl.httpd.service \
	cl.rpcd.service

CLOUDSYSTEMDSERVICES = $(CLOUDSERVICES:%=$(CLOUDROOTLIBSYSTEMDSYSTEM)/%)

CLOUDSCHEMAS = \
	$(CLOUDETCSCHEMAAPPLIANCEDB)/schema000.sql \
	$(CLOUDETCSCHEMAAPPLIANCEDB)/schema001.sql \
	$(CLOUDETCSCHEMAAPPLIANCEDB)/schema002.sql \
	$(CLOUDETCSCHEMAAPPLIANCEDB)/schema003.sql \
	$(CLOUDETCSCHEMAAPPLIANCEDB)/schema004.sql

CLOUDBINARIES = $(CLOUDCOMMANDS:%=$(CLOUDBIN)/%) $(CLOUDDAEMONS:%=$(CLOUDBIN)/%)

CLOUDDIRS = \
	$(CLOUDBIN) \
	$(CLOUDETC) \
	$(CLOUDLIB) \
	$(CLOUDLIBCLHTTPDWEB) \
	$(CLOUDLIBCLHTTPDWEBCLIENTWEB) \
	$(CLOUDETCSCHEMA) \
	$(CLOUDETCSCHEMAAPPLIANCEDB) \
	$(CLOUDROOTLIB) \
	$(CLOUDSPOOL) \
	$(CLOUDVAR)

CLOUDCOMPONENTS = $(CLOUDBINARIES) $(CLOUDSYSTEMDSERVICES) $(CLOUDDIRS) $(CLOUDSCHEMAS)

CLOUD_COMMON_SRCS = \
	$(COMMON_SRCS) \
	$(GOSRCBG)/cloud_rpc/cloud_rpc.pb.go \
	$(GOSRCBG)/cloud_models/appliancedb/appliancedb.go \
	$(GOSRCBG)/cloud_models/sessiondb/sessiondb.go \
	$(GOSRCBG)/cl_common/auth/m2mauth/middleware.go \
	$(GOSRCBG)/cl_common/clcfg/clcfg.go \
	$(GOSRCBG)/cl_common/daemonutils/utils.go \
	$(GOSRCBG)/cl_common/pgutils/utils.go \
	$(GOSRCBG)/cl_common/registry/registry.go

COVERAGE_DIR = coverage

#
# Go Tools: Install versioned binaries for 'dep', 'mockery', etc.
#
include ./Makefile.gotools

#
# Go Dependencies: Pull in definitions for 'dep'
#
include ./Makefile.godeps

.DEFAULT_GOAL = install

install: tools mocks $(TARGETS)

appliance: $(APPCOMPONENTS)

cloud: $(CLOUDCOMPONENTS)

util: $(UTILCOMPONENTS)

# This will create the sysroot.
build-sysroot:
	$(BUILD_SYSROOT_CMD) build

# This will create the sysroot and, if it's new, upload it as a blob, given a
# credential file in $KEY_SYSROOT_UPLOADER.
upload-sysroot:
	$(BUILD_SYSROOT_CMD) build -u

download-sysroot: build/cross-compile/$(SYSROOT_BLOB_NAME)

unpack-sysroot: $(SYSROOT)/.$(SYSROOT_SUM)

build/cross-compile/$(SYSROOT_BLOB_NAME):
	$(BUILD_SYSROOT_CMD) download

$(SYSROOT)/.$(SYSROOT_SUM): build/cross-compile/$(SYSROOT_BLOB_NAME)
	$(BUILD_SYSROOT_CMD) unpack -d $(subst build/cross-compile/,,$(@D))
	touch --no-create $@

packages: install client-web
	$(PYTHON3) build/deb-pkg/deb-pkg.py --arch $(PKG_DEB_ARCH)

packages-lint: install client-web
	$(PYTHON3) build/deb-pkg/deb-pkg.py --lint --arch $(PKG_DEB_ARCH)

GO_MOCK_CLOUDRPC_SRCS = \
	$(GOSRCBG)/cloud_rpc/cloud_rpc.pb.go \
	$(GOSRCBG)/base_def/base_def.go \
	$(GOSRCBG)/base_msg/base_msg.pb.go \
	$(GOSRCBG)/common/cfgmsg/cfgmsg.go \
	$(GOSRCBG)/common/cfgmsg/cfgmsg.pb.go
GO_MOCK_APPLIANCEDB = $(GOSRCBG)/cloud_models/appliancedb/mocks/DataStore.go
GO_MOCK_CLOUDRPC = $(GOSRCBG)/cloud_rpc/mocks/EventClient.go
GO_MOCK_SRCS = \
	$(GO_MOCK_APPLIANCEDB) \
	$(GO_MOCK_CLOUDRPC)

mocks: $(GO_MOCK_SRCS)

# Mock rules-- not sure how to make this work with pattern substitution
# The use of 'realpath' avoids an issue in mockery for workspaces with
# symlinks (https://github.com/vektra/mockery/issues/157).
$(GO_MOCK_CLOUDRPC): $(GO_MOCK_CLOUDRPC_SRCS) $(GOTOOLS) $(GODEPS_ENSURED)
	cd $(realpath $(dir $<)) && GOPATH=$(realpath $(GOPATH)) $(GOTOOLS_BIN_MOCKERY) -name 'EventClient'

$(GO_MOCK_APPLIANCEDB): $(GOSRCBG)/cloud_models/appliancedb/appliancedb.go $(GOTOOLS) $(GODEPS_ENSURED)
	cd $(realpath $(dir $<)) && GOPATH=$(realpath $(GOPATH)) $(GOTOOLS_BIN_MOCKERY) -name 'DataStore'

test: test-go

# The user might set GO_TESTABLES, in which case, honor it
ifeq ("$(GO_TESTABLES)","")
  ifeq ("$(filter appliance, $(TARGETS))", "appliance")
    GO_TESTABLES += $(GO_AP_TESTABLES)
  endif
  ifeq ("$(filter cloud, $(TARGETS))", "cloud")
    GO_TESTABLES += $(GO_CLOUD_TESTABLES)
  endif
endif

test-go: install
	$(GO) test $(GO_TESTFLAGS) $(GO_TESTABLES)

coverage: coverage-go

coverage-go: install
	$(MKDIR) -p $(COVERAGE_DIR)
	$(GO) test $(GO_TESTFLAGS) -cover -coverprofile $(COVERAGE_DIR)/cover.out $(GO_TESTABLES)
	$(GO) tool cover -html=$(COVERAGE_DIR)/cover.out -o $(COVERAGE_DIR)/coverage.html

vet-go:
	$(GO) vet $(APP_GOPKGS)
	$(GO) vet $(CLOUD_GOPKGS)

lint-go:
	$(GOLINT) -set_exit_status $(ALL_GOPKGS)

# Installation of appliance configuration files

$(APPETC)/vendordefaults.csv: $(GOSRCBG)/ap-defaultpass/vendordefaults.csv | $(APPETC)
	$(INSTALL) -m 0644 $< $@

$(APPETC)/ap_identities.csv: ap_identities.csv | $(APPETC)
	$(INSTALL) -m 0644 $< $@

$(APPETC)/ap_mfgid.json: ap_mfgid.json | $(APPETC)
	$(INSTALL) -m 0644 $< $@

$(APPROOTLIB)/systemd/system/ap.mcp.service: ap.mcp.service | $(APPROOTLIB)/systemd/system
	$(INSTALL) -m 0644 $< $@

$(APPROOTLIB)/systemd/system/brightgate-appliance.service: brightgate-appliance.service | $(APPROOTLIB)/systemd/system
	$(INSTALL) -m 0644 $< $@

$(APPETC)/configd.json: $(GOSRCBG)/ap.configd/configd.json | $(APPETC)
	$(INSTALL) -m 0644 $< $@

$(APPETC)/devices.json: $(GOSRCBG)/ap.configd/devices.json | $(APPETC)
	$(INSTALL) -m 0644 $< $@

$(APPETC)/mcp.json: $(GOSRCBG)/ap.mcp/mcp.json | $(APPETC)
	$(INSTALL) -m 0644 $< $@

$(APPETC)/oui.txt: | $(APPETC)
	cd $(APPETC) && curl -s -S -O http://standards-oui.ieee.org/oui.txt

$(APPETC)/datasources.json: datasources.json | $(APPETC)
	$(INSTALL) -m 0644 $< $@

$(APPETC)/prometheus.yml: prometheus.yml | $(APPETC)
	$(INSTALL) -m 0644 $< $@

$(ROOTETCIPTABLES)/rules.v4: $(GOSRCBG)/ap.networkd/rules.v4 | $(ROOTETCIPTABLES)
	$(INSTALL) -m 0644 $< $@

$(ROOTETCLOGROTATED)/com-brightgate-logrotate-logd: $(GOSRCBG)/ap.logd/com-brightgate-logrotate-logd | $(ROOTETCLOGROTATED)
	$(INSTALL) -m 0644 $< $@

$(ROOTETCLOGROTATED)/com-brightgate-logrotate-mcp: $(GOSRCBG)/ap.mcp/com-brightgate-logrotate-mcp | $(ROOTETCLOGROTATED)
	$(INSTALL) -m 0644 $< $@

$(ROOTETCRSYSLOGD)/com-brightgate-rsyslog.conf: $(GOSRCBG)/ap.watchd/com-brightgate-rsyslog.conf | $(ROOTETCRSYSLOGD)
	$(INSTALL) -m 0644 $< $@

$(APPSNMAP)/smb-vuln-ms17-010.nse: $(GOSRCBG)/ap-vuln-aggregate/smb-vuln-ms17-010.nse | $(APPSNMAP)
	$(INSTALL) -m 0644 $< $@

$(APPSPOOLWATCHD)/vuln-db.json: $(GOSRCBG)/ap-vuln-aggregate/sample-db.json | $(APPSPOOLWATCHD)
	$(INSTALL) -m 0644 $< $@

$(NETWORKD_TEMPLATE_DIR)/%: $(GOSRCBG)/ap.networkd/% | $(APPETC)
	$(INSTALL) -m 0644 $< $@

$(USERAUTHD_TEMPLATE_DIR)/%: $(GOSRCBG)/ap.userauthd/% | $(APPETC)
	$(INSTALL) -m 0644 $< $@

$(APPRULES)/%: $(GOSRCBG)/ap.networkd/% | $(APPRULES)
	$(INSTALL) -m 0644 $< $@

$(APPMODEL): $(GOSRCBG)/ap.identifierd/linear_model_deviceID/* | $(DIRS)
	$(MKDIR) -p $@
	cp -r $^ $@
	touch $@

$(APPROOTLIB)/systemd/system: | $(APPROOTLIB)
	$(MKDIR) -p $(APPROOTLIB)/systemd/system

$(APPDIRS):
	$(MKDIR) -p $@

$(APPBINARIES): $(APP_COMMON_SRCS) $(GODEPS_ENSURED) | $(APPBIN)

# Build rules for go binaries.

# As of golang 1.10, 'go build' and 'go install' both cache their results, so
# the latter isn't any faster.  We use 'go build' because because 'go install'
# refuses to install cross-compiled binaries into GOBIN.
$(APPBIN)/%: $(CROSS_DEP)
	$(CROSS_ENV) $(GO) build -o $(@) bg/$*

$(GOSRCBG)/common/version.go: $(GITCHANGED)
	sed "s/GITHASH/$(GITHASH)/" $(GOSRCBG)/common/version.base > $@

$(APPBIN)/ap.brokerd: $(GOSRCBG)/ap.brokerd/brokerd.go
$(APPBIN)/ap.configd: \
	$(GOSRCBG)/common/version.go \
	$(GOSRCBG)/ap.configd/configd.go \
	$(GOSRCBG)/ap.configd/devices.go \
	$(GOSRCBG)/ap.configd/expiration.go \
	$(GOSRCBG)/ap.configd/file.go \
	$(GOSRCBG)/ap.configd/upgrade_v13.go \
	$(GOSRCBG)/ap.configd/upgrade_v14.go \
	$(GOSRCBG)/ap.configd/upgrade_v15.go \
	$(GOSRCBG)/ap.configd/upgrade_v16.go \
	$(GOSRCBG)/ap.configd/validate.go
$(APPBIN)/ap.dhcp4d: $(GOSRCBG)/ap.dhcp4d/dhcp4d.go
$(APPBIN)/ap.dns4d: \
	$(GOSRCBG)/ap.dns4d/dns4d.go \
	$(GOSRCBG)/ap_common/data/dns.go
$(APPBIN)/ap.httpd: \
	$(GOSRCBG)/ap.httpd/ap.httpd.go \
	$(GOSRCBG)/ap.httpd/api-demo.go \
	$(GOSRCBG)/ap_common/certificate/certificate.go \
	$(GOSRCBG)/ap_common/data/dns.go
$(APPBIN)/ap.identifierd: $(GOSRCBG)/ap.identifierd/identifierd.go
$(APPBIN)/ap.logd: $(GOSRCBG)/ap.logd/logd.go
$(APPBIN)/ap.mcp: $(GOSRCBG)/ap.mcp/mcp.go
$(APPBIN)/ap.networkd: \
	$(GOSRCBG)/ap.networkd/filterd.go \
	$(GOSRCBG)/ap.networkd/hostapd.go \
	$(GOSRCBG)/ap.networkd/networkd.go \
	$(GOSRCBG)/ap.networkd/ntpd.go \
	$(GOSRCBG)/ap.networkd/parse.go \
	$(GOSRCBG)/ap.networkd/wifi.go \
	$(GOSRCBG)/ap_common/wificaps/wificaps.go
$(APPBIN)/ap.relayd: $(GOSRCBG)/ap.relayd/relayd.go
$(APPBIN)/ap.rpcd: \
	$(GOSRCBG)/ap.rpcd/rpcd.go \
	$(GOSRCBG)/ap.rpcd/config.go \
	$(GOSRCBG)/ap.rpcd/heartbeat.go \
	$(GOSRCBG)/ap.rpcd/inventory.go
$(APPBIN)/ap.updated: $(GOSRCBG)/ap.updated/update.go
$(APPBIN)/ap.userauthd: $(GOSRCBG)/ap.userauthd/userauthd.go \
	$(GOSRCBG)/ap_common/certificate/certificate.go
$(APPBIN)/ap.watchd: \
	$(GOSRCBG)/ap.watchd/api.go \
	$(GOSRCBG)/ap.watchd/block.go \
	$(GOSRCBG)/ap.watchd/droplog.go \
	$(GOSRCBG)/ap.watchd/metrics.go \
	$(GOSRCBG)/ap.watchd/sampler.go \
	$(GOSRCBG)/ap.watchd/scanner.go \
	$(GOSRCBG)/ap.watchd/watchd.go

$(APPBIN)/ap-arpspoof: $(GOSRCBG)/ap-arpspoof/arpspoof.go
$(APPBIN)/ap-certcheck: $(GOSRCBG)/ap-certcheck/certcheck.go \
	$(GOSRCBG)/ap_common/certificate/certificate.go
$(APPBIN)/ap-complete: $(GOSRCBG)/ap-complete/complete.go
$(APPBIN)/ap-configctl:	\
	$(GOSRCBG)/ap-configctl/configctl.go \
	$(GOSRCBG)/common/configctl/configctl.go
$(APPBIN)/ap-ctl: $(GOSRCBG)/ap-ctl/ctl.go
$(APPBIN)/ap-defaultpass: $(GOSRCBG)/ap-defaultpass/defaultpass.go
$(APPBIN)/ap-diag: \
	$(GOSRCBG)/ap-diag/diag.go \
	$(GOSRCBG)/ap-diag/wifi.go \
	$(GOSRCBG)/ap_common/wificaps/wificaps.go
$(APPBIN)/ap-inspect: $(GOSRCBG)/ap-inspect/inspect.go
$(APPBIN)/ap-factory: $(GOSRCBG)/ap-factory/factory.go
$(APPBIN)/ap-msgping: $(GOSRCBG)/ap-msgping/msgping.go
$(APPBIN)/ap-ouisearch: $(GOSRCBG)/ap-ouisearch/ouisearch.go
$(APPBIN)/ap-rpc: $(APPBIN)/ap.rpcd
	ln -f $< $@
$(APPBIN)/ap-userctl: $(GOSRCBG)/ap-userctl/userctl.go
$(APPBIN)/ap-vuln-aggregate: \
	$(GOSRCBG)/ap-vuln-aggregate/ap-defaultpass.go \
	$(GOSRCBG)/ap-vuln-aggregate/ap-inspect.go \
	$(GOSRCBG)/ap-vuln-aggregate/nmap.go \
	$(GOSRCBG)/ap-vuln-aggregate/aggregate.go
$(APPBIN)/ap-watchctl:	$(GOSRCBG)/ap-watchctl/watchctl.go

LOCAL_BINARIES=$(APPBINARIES:$(APPBIN)/%=$(GOBIN)/%)

# Miscellaneous utility components

$(UTILBINARIES): $(UTILCOMMON_SRCS) $(GODEPS_ENSURED)

$(UTILDIRS):
	$(MKDIR) -p $@

$(UTILBIN)/%: $(GOSRCBG)/util/%.go | $(UTILBIN)
	$(GO) build -o $(@) $(GOSRCBG)/util/$*.go

# Cloud components

# Installation of cloud configuration files

$(CLOUDETC)/datasources.json: datasources.json | $(CLOUDETC)
	$(INSTALL) -m 0644 $< $(CLOUDETC)

# Install database schema files
$(CLOUDETCSCHEMAAPPLIANCEDB)/%: golang/src/bg/cloud_models/appliancedb/schema/% | $(CLOUDETCSCHEMAAPPLIANCEDB)
	$(INSTALL) -m 0644 $< $@

# Install service descriptions
$(CLOUDROOTLIBSYSTEMDSYSTEM)/%: build/cl-systemd/% | $(CLOUDROOTLIBSYSTEMDSYSTEM)
	$(INSTALL) -m 0644 $< $@

$(CLOUDBINARIES): $(COMMON_SRCS) $(GODEPS_ENSURED)

$(CLOUDBIN)/%: | $(CLOUDBIN)
	$(GO) build -o $(@) bg/$*

$(CLOUDBIN)/cl-aggregate: \
	$(GOSRCBG)/cl-aggregate/aggregate.go \
	$(CLOUD_COMMON_SRCS)
$(CLOUDBIN)/cl-configctl: \
	$(GOSRCBG)/cl-configctl/configctl.go \
	$(GOSRCBG)/common/configctl/configctl.go \
	$(CLOUD_COMMON_SRCS)
$(CLOUDBIN)/cl-dtool: \
	$(GOSRCBG)/cl-dtool/dtool.go \
	$(GOSRCBG)/cl-dtool/export.go \
	$(GOSRCBG)/cl-dtool/merge.go \
	$(CLOUD_COMMON_SRCS)
$(CLOUDBIN)/cl-reg: \
	$(GOSRCBG)/cl-reg/main.go \
	$(CLOUD_COMMON_SRCS)
$(CLOUDBIN)/cl.configd: \
	$(GOSRCBG)/cl.configd/apstate.go \
	$(GOSRCBG)/cl.configd/backend.go \
	$(GOSRCBG)/cl.configd/cmdqueue.go \
	$(GOSRCBG)/cl.configd/cmdqueuedb.go \
	$(GOSRCBG)/cl.configd/configd.go \
	$(GOSRCBG)/cl.configd/db.go \
	$(GOSRCBG)/cl.configd/file.go \
	$(GOSRCBG)/cl.configd/frontend.go \
	$(GOSRCBG)/cl.configd/grpc.go \
	$(CLOUD_COMMON_SRCS)
$(CLOUDBIN)/cl.eventd: \
	$(GOSRCBG)/cl.eventd/eventd.go \
	$(GOSRCBG)/cl.eventd/inventory.go \
	$(CLOUD_COMMON_SRCS)
$(CLOUDBIN)/cl.httpd: \
	$(GOSRCBG)/cl.httpd/cl.httpd.go \
	$(GOSRCBG)/cl.httpd/auth.go \
	$(GOSRCBG)/cl.httpd/api.go \
	$(GOSRCBG)/cl.httpd/access.go \
	$(CLOUD_COMMON_SRCS)
$(CLOUDBIN)/cl.rpcd: \
	$(GOSRCBG)/cl.rpcd/cfg_relay.go \
	$(GOSRCBG)/cl.rpcd/event.go \
	$(GOSRCBG)/cl.rpcd/rpcd.go \
	$(GOSRCBG)/cl.rpcd/storage.go \
	$(CLOUD_COMMON_SRCS)

$(CLOUDROOTLIBSYSTEMDSYSTEM): | $(CLOUDROOTLIB)
	$(MKDIR) -p $@

$(CLOUDDIRS):
	$(MKDIR) -p $@

#
# Common definitions
#

$(GOSRCBG)/base_def/base_def.go: base/generate-base-def.py | $(GOSRCBG)/base_def
	$(PYTHON3) $< --go | $(GOFMT) > $@

base/base_def.py: base/generate-base-def.py
	$(PYTHON3) $< --python3 > $@

#
# Protocol buffers
#

$(GOSRCBG)/base_msg/base_msg.pb.go: base/base_msg.proto $(GOTOOLS)
	cd base && \
		protoc --plugin=$(GOTOOLS_BIN_PROTOCGENGO) \
		    --go_out ../$(GOSRCBG)/base_msg $(notdir $<)

base/base_msg_pb2.py: base/base_msg.proto
	protoc --python_out . $<

$(GOSRCBG)/common/cfgmsg/cfgmsg.pb.go: base/cfgmsg.proto $(GOTOOLS)
	cd base && \
		protoc --plugin=$(GOTOOLS_BIN_PROTOCGENGO) \
		    --go_out=../$(GOSRCBG)/common/cfgmsg \
		    $(notdir $<)

$(GOSRCBG)/cloud_rpc/cloud_rpc.pb.go: base/cloud_rpc.proto $(GOTOOLS)
	cd base && \
		protoc --plugin=$(GOTOOLS_BIN_PROTOCGENGO) \
			-I/usr/local/include \
			-I . \
			-I$(GOTOOLS_DIR)/src \
			-I$(GOTOOLS_DIR)/src/$(GOTOOLS_pgengo_repo)/descriptor \
			--go_out=plugins=grpc,Mbase_msg.proto=bg/base_msg,Mcfgmsg.proto=bg/common/cfgmsg:../$(GOSRCBG)/cloud_rpc \
			$(notdir $<)

base/cloud_rpc_pb2.py: base/cloud_rpc.proto
	$(PYTHON3) -m grpc_tools.protoc \
		-I. \
		-Ibase \
		--python_out=. --grpc_python_out=. $<

LOCAL_COMMANDS=$(COMMANDS:$(APPBIN)/%=$(GOBIN)/%)
LOCAL_DAEMONS=$(DAEMONS:$(APPBIN)/%=$(GOBIN)/%)

# Generate a hash of the contents of BUILDTOOLS, so that if the required
# packages change, we'll rerun the check.
# note: The hash is constant; := avoids repeated shell invocations
BUILDTOOLS_HASH := $(shell echo $(BUILDTOOLS) | $(SHA256SUM) | awk '{print $$1}')
BUILDTOOLS_FILE = .make-buildtools-$(BUILDTOOLS_HASH)

.PHONY: tools
tools: $(BUILDTOOLS_FILE) $(GOTOOLS)

install-tools: FRC
	build/check-tools.sh -i $(BUILDTOOLS)
	touch $@

$(BUILDTOOLS_FILE):
	build/check-tools.sh $(BUILDTOOLS)
	touch $@

NPM = npm
NPM_QUIET = --loglevel warn --no-progress
.make-npm-installed: client-web/package.json
	(cd client-web && $(NPM) install $(NPM_QUIET))
	touch $@

client-web: .make-npm-installed FRC | $(HTTPD_CLIENTWEB_DIR) $(CLOUDLIBCLHTTPDWEBCLIENTWEB)
	$(RM) -fr $(HTTPD_CLIENTWEB_DIR)/* $(CLOUDLIBCLHTTPDWEBCLIENTWEB)/*
	(cd client-web && $(NPM) run lint)
	(cd client-web && $(NPM) run build)
	tar -C client-web/dist -c -f - . | tar -C $(HTTPD_CLIENTWEB_DIR) -xvf -
	tar -C client-web/dist -c -f - . | tar -C $(CLOUDLIBCLHTTPDWEBCLIENTWEB) -xvf -

FRC:

.PHONY: clobber
clobber: clean packages-clobber godeps-clobber gotools-clobber
	$(RM) -fr $(ROOT) $(GOWS)/pkg $(GOWS)/bin $(SYSROOT)
	$(RM) -f .make-*

.PHONY: packages-clobber
packages-clobber:
	$(RM) -fr bg-appliance_*.*.*-*_* bg-cloud_*.*.*-*_*

clean:
	$(RM) -f \
		base/base_def.py \
		base/base_msg_pb2.py \
		base/cloud_rpc_pb2.py \
		$(GOSRCBG)/base_def/base_def.go \
		$(GOSRCBG)/base_msg/base_msg.pb.go \
		$(GOSRCBG)/cloud_rpc/cloud_rpc.pb.go \
		$(GOSRCBG)/common/version.go \
		$(APPBINARIES) \
		$(CLOUDBINARIES) \
		$(UTILBINARIES) \
		$(GO_MOCK_SRCS)
	$(RM) -fr $(COVERAGE_DIR)
	find $(GOSRCBG)/ap_common -name \*.pem | xargs --no-run-if-empty $(RM) -f

.PHONY: check-dirty
check-dirty:
	@c=$$(git status -s); \
	if [ -n "$$c" ]; then \
		echo "Workspace is dirty:"; \
		echo "$$c"; \
		echo "--"; \
		git diff; \
		exit 1; \
	fi
