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
#	 [If in the Brightgate cloud, Go is already installed.  Else, retrieve
#	 Go tar archive from golang.org and unpack in $HOME.]
#
#    (c) On raspberry pi
#
#	 $ make tools
#	 [Follow the directions]
#	 $ sudo apt-get install hostapd
#	 $ sudo pip3 install sh
#	 [Retrieve Go tar archive from golang.org and unpack in $HOME.]
#	 [Retrieve the TensorFlow C library from
#	  https://ph0.b10e.net/w/testing-raspberry-pi/ or
#	  https://ph0.b10e.net/w/testing-banana-pi/]
#
# 2. To clean out local binaries, use
#
#	 $ make plat-clobber
#
# 3. On x86_64, the build constructs all components, whether for appliance or
#    for cloud.  On ARM, only appliance components are built.

#
# OS definitions
#
UNAME_S = $(shell uname -s)
UNAME_M = $(shell uname -m)

#
# Git related definitions
#
export GITROOT = $(shell git rev-parse --show-toplevel)
export GOPATH=$(GITROOT)/golang
GITHASH=$(shell git describe --always --long --dirty)

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
export GOROOT=$(wildcard /opt/net.b10e/go)
GOROOT_SEARCH += /opt/net.b10e/go
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

GOOS = $(shell GOROOT=$(GOROOT) $(GO) env GOOS)
GOARCH = $(shell GOROOT=$(GOROOT) $(GO) env GOARCH)
GOHOSTARCH = $(shell GOROOT=$(GOROOT) $(GO) env GOHOSTARCH)
GOVERSION = $(shell GOROOT=$(GOROOT) $(GO) version)

GOWS = golang
GOSRCBG = $(GOWS)/src/bg
# Vendoring directory, where external deps are placed
GOSRCBGVENDOR = $(GOSRCBG)/vendor
# Where we stick build tools
GOBIN = golang/bin

GOVERFLAGS=-ldflags="-X main.ApVersion=$(GITHASH)"

#
# Miscellaneous environment setup
#
INSTALL = install
MKDIR = mkdir
RM = rm

PYTHON3 = python3
PYTHON3VERSION = $(shell $(PYTHON3) -V)

NODE = node
NODEVERSION = $(shell $(NODE) --version)

PROTOC_PLUGINS = \
	$(GOPATH)/bin/protoc-gen-doc \
	$(GOPATH)/bin/protoc-gen-go

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
# UNAME_M will read armv7l on Raspbian and on Ubuntu for  Banana Pi.
# Both use armhf as the architecture for .deb files.
ROOT=proto.armv7l
PKG_DEB_ARCH=armhf
TARGETS=appliance
endif

#
# Cross compilation setup
#
ifneq ($(GOHOSTARCH),$(GOARCH))
ifeq ($(SYSROOT),)
$(error SYSROOT must be set for cross builds)
endif
ifeq ($(GOARCH),arm)
BUILDTOOLS_CROSS = crossbuild-essential-armhf
else
$(error 'arm' is the only supported cross target)
endif

# SYSROOT doesn't work right if isn't an absolute path
CROSS_SYSROOT=$(realpath $(SYSROOT))
CROSS_CC=/usr/bin/arm-linux-gnueabihf-gcc
CROSS_CGO_LDFLAGS=--sysroot $(CROSS_SYSROOT) -Lusr/local/lib
CROSS_CGO_CFLAGS=--sysroot $(CROSS_SYSROOT) -Iusr/local/include
endif

BUILDTOOLS = \
	$(BUILDTOOLS_CROSS) \
	protobuf-compiler \
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
$(ifneq "$(GOHOSTARCH)","$(GOARCH)")
define report
#     CROSSBUILD: $(GOHOSTARCH) -> $(GOARCH)
#        SYSROOT: $(SYSROOT)
$(info $(report))
endef

#
# Appliance components and supporting definitions
#

APPROOT=$(ROOT)/appliance
APPBASE=$(APPROOT)/opt/com.brightgate
APPBIN=$(APPBASE)/bin
APPDOC=$(APPBASE)/share/doc
APPWEB=$(APPBASE)/share/web
APPSNMAP=$(APPBASE)/share/nmap/scripts
# APPCSS
# APPJS
# APPHTML
APPETC=$(APPBASE)/etc
APPROOTLIB=$(APPROOT)/lib
APPVAR=$(APPBASE)/var
APPSECRET=$(APPETC)/secret
APPSECRETSSL=$(APPSECRET)/ssl
APPSECRETIOTCORE=$(APPSECRET)/iotcore
APPSPOOL=$(APPVAR)/spool
APPSPOOLANTIPHISH=$(APPVAR)/spool/antiphishing
APPSPOOLWATCHD=$(APPVAR)/spool/watchd
APPRULES=$(APPETC)/filter.rules.d
APPMODEL=$(APPETC)/device_model

ROOTETC=$(APPROOT)/etc
ROOTETCCROND=$(ROOTETC)/cron.d
ROOTETCIPTABLES=$(ROOTETC)/iptables
ROOTETCLOGROTATED=$(ROOTETC)/logrotate.d
ROOTETCRSYSLOGD=$(ROOTETC)/rsyslog.d

HTTPD_CLIENTWEB_DIR=$(APPVAR)/www/client-web
HTTPD_TEMPLATE_DIR=$(APPETC)/templates/ap.httpd
NETWORKD_TEMPLATE_DIR=$(APPETC)/templates/ap.networkd
USERAUTHD_TEMPLATE_DIR=$(APPETC)/templates/ap.userauthd

COMMON_GOPKGS = \
	bg/ap_common/apcfg \
	bg/ap_common/aputil \
	bg/ap_common/broker \
	bg/ap_common/device \
	bg/ap_common/iotcore \
	bg/ap_common/mcp \
	bg/ap_common/model \
	bg/ap_common/network \
	bg/ap_common/watchd \
	bg/common

APPCOMMAND_GOPKGS = \
	bg/ap-arpspoof \
	bg/ap-complete \
	bg/ap-configctl \
	bg/ap-ctl \
	bg/ap-inspect \
	bg/ap-iot \
	bg/ap-msgping \
	bg/ap-ouisearch \
	bg/ap-rpc \
	bg/ap-stats \
	bg/ap-userctl \
	bg/ap-vuln-aggregate

APPDAEMON_GOPKGS = \
	bg/ap.brokerd \
	bg/ap.configd \
	bg/ap.dhcp4d \
	bg/ap.dns4d \
	bg/ap.httpd \
	bg/ap.identifierd \
	bg/ap.iotd \
	bg/ap.logd \
	bg/ap.mcp \
	bg/ap.networkd \
	bg/ap.relayd \
	bg/ap.userauthd \
	bg/ap.watchd

ALL_GOPKGS = $(APP_GOPKGS) $(CLOUD_GOPKGS)

APP_GOPKGS = $(COMMON_GOPKGS) $(APPCOMMAND_GOPKGS) $(APPDAEMON_GOPKGS)

MISCCOMMANDS = \
	ap-start

APPBINARIES = \
	$(APPCOMMAND_GOPKGS:bg/%=$(APPBIN)/%) \
	$(APPDAEMON_GOPKGS:bg/%=$(APPBIN)/%) \
	$(MISCCOMMANDS:%=$(APPBIN)/%)

# XXX Common configurations?

GO_TESTABLES = \
	bg/ap_common/apcfg \
	bg/ap_common/aputil \
	bg/ap_common/iotcore \
	bg/ap_common/network \
	bg/ap.iotd \
	bg/ap.networkd \
	bg/ap.userauthd \
	bg/cl_common/daemonutils \
	bg/cloud_models/cloudiotsvc

HTTPD_TEMPLATE_FILES = \
	stats.html.got

NETWORKD_TEMPLATE_FILES = \
	hostapd.conf.got

USERAUTHD_TEMPLATE_FILES = \
	hostapd.radius.got \
	hostapd.radius_clients.got \
	hostapd.users.got

# hostapd.radius_clients.got \

HTTPD_TEMPLATES = $(HTTPD_TEMPLATE_FILES:%=$(HTTPD_TEMPLATE_DIR)/%)
NETWORKD_TEMPLATES = $(NETWORKD_TEMPLATE_FILES:%=$(NETWORKD_TEMPLATE_DIR)/%)
USERAUTHD_TEMPLATES = $(USERAUTHD_TEMPLATE_FILES:%=$(USERAUTHD_TEMPLATE_DIR)/%)
APPTEMPLATES = $(HTTPD_TEMPLATES) $(NETWORKD_TEMPLATES) $(USERAUTHD_TEMPLATES)

FILTER_RULES = \
	$(APPRULES)/base.rules \
	$(APPRULES)/local.rules \
	$(APPRULES)/relayd.rules

APPCONFIGS = \
	$(APPETC)/ap_defaults.json \
	$(APPETC)/ap_identities.csv \
	$(APPETC)/ap_mfgid.json \
	$(APPETC)/devices.json \
	$(APPETC)/mcp.json \
	$(APPETC)/oui.txt \
	$(APPETC)/prometheus.yml \
	$(APPROOTLIB)/systemd/system/ap.mcp.service \
	$(APPROOTLIB)/systemd/system/brightgate-appliance.service \
	$(APPSNMAP)/smb-vuln-ms17-010.nse \
	$(APPSPOOLANTIPHISH)/example_blacklist.csv \
	$(APPSPOOLANTIPHISH)/whitelist.csv \
	$(APPSPOOLWATCHD)/vuln-db.json \
	$(ROOTETCCROND)/com-brightgate-appliance-cron \
	$(ROOTETCIPTABLES)/rules.v4 \
	$(ROOTETCRSYSLOGD)/com-brightgate-rsyslog.conf \
	$(ROOTETCLOGROTATED)/com-brightgate-logrotate-logd \
	$(ROOTETCLOGROTATED)/com-brightgate-logrotate-mcp

APPDIRS = \
	$(APPBIN) \
	$(APPDOC) \
	$(APPETC) \
	$(APPROOTLIB) \
	$(APPRULES) \
	$(APPSECRET) \
	$(APPSECRETSSL) \
	$(APPSECRETIOTCORE) \
	$(APPSNMAP) \
	$(APPSPOOL) \
	$(APPVAR) \
	$(APPSPOOLANTIPHISH) \
	$(APPSPOOLWATCHD) \
	$(HTTPD_CLIENTWEB_DIR) \
	$(HTTPD_TEMPLATE_DIR) \
	$(NETWORKD_TEMPLATE_DIR) \
	$(ROOTETC) \
	$(ROOTETCCROND) \
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
	$(GOSRCBG)/ap_common/apcfg/apcfg.go \
	$(GOSRCBG)/ap_common/apcfg/events.go \
	$(GOSRCBG)/ap_common/apcfg/users.go \
	$(GOSRCBG)/ap_common/aputil/aputil.go \
	$(GOSRCBG)/ap_common/broker/broker.go \
	$(GOSRCBG)/ap_common/device/device.go \
	$(GOSRCBG)/ap_common/iotcore/iotcore.go \
	$(GOSRCBG)/ap_common/iotcore/iotcred.go \
	$(GOSRCBG)/ap_common/mcp/mcp_client.go \
	$(GOSRCBG)/ap_common/model/model.go \
	$(GOSRCBG)/ap_common/network/network.go \
	$(GOSRCBG)/ap_common/watchd/watchd_client.go \
	$(GOSRCBG)/base_def/base_def.go \
	$(GOSRCBG)/base_msg/base_msg.pb.go \
	$(GOSRCBG)/common/urlfetch.go

# Miscellaneous utilities

UTILROOT=$(ROOT)/util
UTILBIN=$(UTILROOT)/bin

UTILCOMMON_SRCS = \
	$(GOSRCBG)/ap_common/device/device.go \
	$(GOSRCBG)/ap_common/model/model.go \
	$(GOSRCBG)/ap_common/network/network.go \
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
CLOUDROOTLIB=$(CLOUDROOT)/lib
CLOUDVAR=$(CLOUDBASE)/var
CLOUDSPOOL=$(CLOUDVAR)/spool

CLOUDDAEMON_GOPKGS = \
	bg/cl.eventd \
	bg/cl.httpd \
	bg/cl.rpcd

CLOUDCOMMON_GOPKGS = \
	bg/cl_common/daemonutils \
	bg/cloud_models/appliancedb \
	bg/cloud_models/cloudiotsvc \
	bg/common

CLOUDCOMMAND_GOPKGS = bg/cl-aggregate

CLOUD_GOPKGS = $(CLOUDCOMMON_GOPKGS) $(CLOUDDAEMON_GOPKGS) $(CLOUDCOMMAND_GOPKGS)

CLOUDDAEMONS = $(CLOUDDAEMON_GOPKGS:bg/%=%)

CLOUDCOMMANDS = $(CLOUDCOMMAND_GOPKGS:bg/%=%)

CLOUDCONFIGS = \
	$(CLOUDROOTLIB)/systemd/system/cl.httpd.service \
	$(CLOUDROOTLIB)/systemd/system/cl.rpcd.service \
	$(CLOUDROOTLIB)/systemd/system/cl.eventd.service

CLOUDBINARIES = $(CLOUDCOMMANDS:%=$(CLOUDBIN)/%) $(CLOUDDAEMONS:%=$(CLOUDBIN)/%)

CLOUDDIRS = \
	$(CLOUDBIN) \
	$(CLOUDETC) \
	$(CLOUDROOTLIB) \
	$(CLOUDSPOOL) \
	$(CLOUDVAR)

CLOUDCOMPONENTS = $(CLOUDBINARIES) $(CLOUDCONFIGS) $(CLOUDDIRS)

CLOUD_COMMON_SRCS = \
    $(GOSRCBG)/cloud_rpc/cloud_rpc.pb.go \
    $(GOSRCBG)/cloud_models/appliancedb/appliancedb.go \
    $(GOSRCBG)/cl_common/daemonutils/utils.go \
    $(GOSRCBG)/common/urlfetch.go

install: tools mocks $(TARGETS)

appliance: $(APPCOMPONENTS)

cloud: $(CLOUDCOMPONENTS)

util: $(UTILCOMPONENTS)

packages: install client-web
	$(PYTHON3) build/deb-pkg/deb-pkg.py --arch $(PKG_DEB_ARCH)

packages-lint: install client-web
	$(PYTHON3) build/deb-pkg/deb-pkg.py --lint --arch $(PKG_DEB_ARCH)

test: test-go

MOCKERY=$(GOPATH)/bin/mockery

$(MOCKERY):
	$(GO) get -u github.com/vektra/mockery/.../

GO_MOCK_IOTMQTTCLIENT = $(GOSRCBG)/ap_common/iotcore/mocks/IoTMQTTClient.go
GO_MOCK_SRCS = \
	$(GO_MOCK_IOTMQTTCLIENT)

mocks: $(GO_MOCK_SRCS)

# Mock rules-- not sure how to make this work with pattern substitution
# After generation, make compliant with
# https://github.com/golang/go/issues/13560 so that golint will skip this file.
# Can be pulled after https://github.com/vektra/mockery/issues/183 is fixed
# The use of 'realpath' avoids an issue in mockery for workspaces with
# symlinks (https://github.com/vektra/mockery/issues/157).
$(GO_MOCK_IOTMQTTCLIENT): $(GOSRCBG)/ap_common/iotcore/iotcore.go | $(MOCKERY) deps-ensured
	cd $(realpath $(dir $<)) && GOPATH=$(realpath $(GOPATH)) $(MOCKERY) -name IoTMQTTClient
	sed -i 's/\(\/\/ Code generated.*\)/\1.  DO NOT EDIT./' $@

test-go: install
	$(GO) test $(GO_TESTFLAGS) $(GO_TESTABLES)

coverage: coverage-go

coverage-go: install
	$(GO) test -cover $(GO_TESTABLES)

vet-go:
	$(GO) vet $(APP_GOPKGS)
	$(GO) vet $(CLOUD_GOPKGS)

lint-go:
	$(GOLINT) -set_exit_status $(ALL_GOPKGS)

docs: | $(PROTOC_PLUGINS)


$(APPDOC)/: base/base_msg.proto | $(PROTOC_PLUGINS) $(APPDOC) $(BASE_MSG)
	cd base && \
		protoc --plugin $(GOPATH)/bin \
		    --doc_out $(APPDOC) $(notdir $<)

# Installation of appliance configuration files

$(APPETC)/ap_defaults.json: $(GOSRCBG)/ap.configd/ap_defaults.json | $(APPETC)
	$(INSTALL) -m 0644 $< $@

$(APPETC)/ap_identities.csv: ap_identities.csv | $(APPETC)
	$(INSTALL) -m 0644 $< $@

$(APPETC)/ap_mfgid.json: ap_mfgid.json | $(APPETC)
	$(INSTALL) -m 0644 $< $@

$(APPROOTLIB)/systemd/system/ap.mcp.service: ap.mcp.service | $(APPROOTLIB)/systemd/system
	$(INSTALL) -m 0644 $< $@

$(APPROOTLIB)/systemd/system/brightgate-appliance.service: brightgate-appliance.service | $(APPROOTLIB)/systemd/system
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

$(ROOTETCCROND)/com-brightgate-appliance-cron: com-brightgate-appliance-cron | $(ROOTETCCROND)
	$(INSTALL) -m 0644 $< $(ROOTETCCROND)

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

$(APPSPOOLANTIPHISH)/example_blacklist.csv: $(GOSRCBG)/data/phishtank/example_blacklist.csv | $(APPSPOOLANTIPHISH)
	$(INSTALL) -m 0644 $< $@

$(APPSPOOLANTIPHISH)/whitelist.csv: $(GOSRCBG)/data/phishtank/whitelist.csv | $(APPSPOOLANTIPHISH)
	$(INSTALL) -m 0644 $< $@

$(APPSPOOLWATCHD)/vuln-db.json: $(GOSRCBG)/ap-vuln-aggregate/sample-db.json | $(APPSPOOLWATCHD)
	$(INSTALL) -m 0644 $< $@

$(HTTPD_TEMPLATE_DIR)/%: $(GOSRCBG)/ap.httpd/% | $(APPETC)
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

COMMON_SRCS = \
	$(GOSRCBG)/base_def/base_def.go \
	$(GOSRCBG)/base_msg/base_msg.pb.go \
	$(GOSRCBG)/ap_common/broker/broker.go \
	$(GOSRCBG)/ap_common/apcfg/apcfg.go \
	$(GOSRCBG)/ap_common/aputil/aputil.go \
	$(GOSRCBG)/ap_common/mcp/mcp_client.go \
	$(GOSRCBG)/ap_common/network/network.go

PHISH_SRCS = \
	$(GOSRCBG)/data/phishtank/datasource.go \
	$(GOSRCBG)/data/phishtank/csv.go \
	$(GOSRCBG)/data/phishtank/remote.go \
	$(GOSRCBG)/data/phishtank/safebrowsing.go

$(APPBINARIES): $(APP_COMMON_SRCS) | $(APPBIN) deps-ensured

$(APPBIN)/ap-start: ap-start.sh
	$(INSTALL) -m 0755 $< $@

# Build rules for go binaries.
ifeq ($(GOHOSTARCH),$(GOARCH))
# Non-cross compilation ("normal" build)

# Here we use 'go install' because it is faster than 'go build'
#
# The 'touch' is present because of the blanket dependency of all APPBINARIES
# on APP_COMMON_SRCS.  If a developer updates one of the common sources, then
# make will conclude that all binaries must be rebuilt, but 'go install' will
# skip writing the binary in cases where it detects that there is nothing to
# do.  This should be revisited for golang 1.10.
$(APPBIN)/%:
	GOBIN=$(realpath $(@D)) \
	    $(GO) install $(GOVERFLAGS) bg/$*
	touch $(@)

else
# Cross compiling

# We cannot use 'go install' because part of go's cross-compilation scheme
# involves rebuilding parts of its standard library at compile time, and
# 'go install' will fail doing that because it wants to write things to
# the global GOROOT.  We fall back to 'go build'.
#
# See above for rationale about touch.  Possibly not strictly needed here
# but included for parity.
$(APPBIN)/%:
	SYSROOT=$(CROSS_SYSROOT) CC=$(CROSS_CC) \
	    CGO_LDFLAGS="$(CROSS_CGO_LDFLAGS)" \
	    CGO_CFLAGS="$(CROSS_CGO_CFLAGS)" \
	    CGO_ENABLED=1 $(GO) build -o $(@) $(GOVERFLAGS) bg/$*
	touch $(@)
endif

$(APPBIN)/ap.brokerd: $(GOSRCBG)/ap.brokerd/brokerd.go
$(APPBIN)/ap.configd: \
	$(GOSRCBG)/ap.configd/configd.go \
	$(GOSRCBG)/ap.configd/devices.go \
	$(GOSRCBG)/ap.configd/upgrade_v1.go \
	$(GOSRCBG)/ap.configd/upgrade_v2.go \
	$(GOSRCBG)/ap.configd/upgrade_v4.go \
	$(GOSRCBG)/ap.configd/upgrade_v5.go \
	$(GOSRCBG)/ap.configd/upgrade_v6.go \
	$(GOSRCBG)/ap.configd/upgrade_v7.go \
	$(GOSRCBG)/ap.configd/upgrade_v8.go \
	$(GOSRCBG)/ap.configd/upgrade_v9.go
$(APPBIN)/ap.dhcp4d: $(GOSRCBG)/ap.dhcp4d/dhcp4d.go
$(APPBIN)/ap.dns4d: \
	$(GOSRCBG)/ap.dns4d/dns4d.go \
	$(PHISH_SRCS)
$(APPBIN)/ap.httpd: \
	$(GOSRCBG)/ap.httpd/ap.httpd.go \
	$(GOSRCBG)/ap.httpd/api-demo.go \
	$(PHISH_SRCS)
$(APPBIN)/ap.identifierd: $(GOSRCBG)/ap.identifierd/identifierd.go
$(APPBIN)/ap.iotd: $(GOSRCBG)/ap.iotd/iotd.go
$(APPBIN)/ap.logd: $(GOSRCBG)/ap.logd/logd.go
$(APPBIN)/ap.mcp: $(GOSRCBG)/ap.mcp/mcp.go
$(APPBIN)/ap.networkd: \
	$(GOSRCBG)/ap.networkd/filterd.go \
	$(GOSRCBG)/ap.networkd/hostapd.go \
	$(GOSRCBG)/ap.networkd/networkd.go \
	$(GOSRCBG)/ap.networkd/parse.go
$(APPBIN)/ap.relayd: $(GOSRCBG)/ap.relayd/relayd.go
$(APPBIN)/ap.userauthd: $(GOSRCBG)/ap.userauthd/userauthd.go
$(APPBIN)/ap.watchd: \
	$(GOSRCBG)/ap.watchd/api.go \
	$(GOSRCBG)/ap.watchd/block.go \
	$(GOSRCBG)/ap.watchd/droplog.go \
	$(GOSRCBG)/ap.watchd/metrics.go \
	$(GOSRCBG)/ap.watchd/sampler.go \
	$(GOSRCBG)/ap.watchd/scanner.go \
	$(GOSRCBG)/ap.watchd/watchd.go

$(APPBIN)/ap-arpspoof: $(GOSRCBG)/ap-arpspoof/arpspoof.go
$(APPBIN)/ap-complete: $(GOSRCBG)/ap-complete/complete.go
$(APPBIN)/ap-configctl: $(GOSRCBG)/ap-configctl/configctl.go
$(APPBIN)/ap-ctl: $(GOSRCBG)/ap-ctl/ctl.go
$(APPBIN)/ap-inspect: $(GOSRCBG)/ap-inspect/inspect.go
$(APPBIN)/ap-msgping: $(GOSRCBG)/ap-msgping/msgping.go
$(APPBIN)/ap-ouisearch: $(GOSRCBG)/ap-ouisearch/ouisearch.go
$(APPBIN)/ap-rpc: \
	$(GOSRCBG)/ap-rpc/rpc.go \
	$(CLOUD_COMMON_SRCS)
$(APPBIN)/ap-iot: \
	$(GOSRCBG)/ap-iot/iot.go \
	$(CLOUD_COMMON_SRCS)
$(APPBIN)/ap-stats: $(GOSRCBG)/ap-stats/stats.go
$(APPBIN)/ap-userctl: $(GOSRCBG)/ap-userctl/userctl.go
$(APPBIN)/ap-vuln-aggregate: \
	$(GOSRCBG)/ap-vuln-aggregate/ap-inspect.go \
	$(GOSRCBG)/ap-vuln-aggregate/nmap.go \
	$(GOSRCBG)/ap-vuln-aggregate/aggregate.go

LOCAL_BINARIES=$(APPBINARIES:$(APPBIN)/%=$(GOPATH)/bin/%)

# Miscellaneous utility components

$(UTILBINARIES): $(UTILCOMMON_SRCS) | deps-ensured

$(UTILDIRS):
	$(MKDIR) -p $@

$(UTILBIN)/%: $(GOSRCBG)/util/%.go | $(UTILBIN)
	GOBIN=$(realpath $(@D)) \
	    $(GO) install $(GOVERFLAGS) $(GOSRCBG)/util/$*.go

# Cloud components

# Installation of cloud configuration files

$(CLOUDETC)/datasources.json: datasources.json | $(CLOUDETC)
	$(INSTALL) -m 0644 $< $(CLOUDETC)

# Install service descriptions
$(CLOUDROOTLIB)/systemd/system/%: % | $(CLOUDROOTLIB)/systemd/system
	$(INSTALL) -m 0644 $< $@

$(CLOUDBINARIES): $(COMMON_SRCS) | deps-ensured

# Here we use 'go install' because it is faster than 'go build'
#
# The 'touch' is present because of the blanket dependency of all CLOUDBINARIES
# on COMMON_SRCS.  If a developer updates one of the common sources, then make
# will conclude that all binaries must be rebuilt, but 'go install' will skip
# writing the binary in cases where it detects that there is nothing to do.
# This should be revisited for golang 1.10.
$(CLOUDBIN)/%: | $(CLOUDBIN)
	GOBIN=$(realpath $(@D)) \
	    $(GO) install $(GOVERFLAGS) bg/$*
	touch $(@)

$(CLOUDBIN)/cl-aggregate: \
	$(GOSRCBG)/cl-aggregate/aggregate.go \
	$(CLOUD_COMMON_SRCS)
$(CLOUDBIN)/cl.eventd: \
	$(GOSRCBG)/cl.eventd/eventd.go \
	$(CLOUD_COMMON_SRCS)
$(CLOUDBIN)/cl.httpd: \
	$(GOSRCBG)/cl.httpd/cl.httpd.go
$(CLOUDBIN)/cl.rpcd: \
	$(GOSRCBG)/cl.rpcd/rpcd.go \
	$(CLOUD_COMMON_SRCS)

$(CLOUDROOTLIB)/systemd/system: | $(CLOUDROOTLIB)
	$(MKDIR) -p $(CLOUDROOTLIB)/systemd/system

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

$(GOSRCBG)/base_msg/base_msg.pb.go: base/base_msg.proto | \
	$(PROTOC_PLUGINS) $(GOSRCBG)/base_msg
	cd base && \
		protoc --plugin $(GOPATH)/bin \
		    --go_out ../$(GOSRCBG)/base_msg $(notdir $<)

base/base_msg_pb2.py: base/base_msg.proto
	protoc --python_out . $<

$(GOSRCBG)/cloud_rpc/cloud_rpc.pb.go: base/cloud_rpc.proto | \
	$(PROTOC_PLUGINS) $(GOSRCBG)/cloud_rpc
	cd base && \
		protoc --plugin $(GOPATH)/bin \
			-I/usr/local/include \
			-I . \
			-I$(GOPATH)/src \
			-I$(GOPATH)/src/github.com/golang/protobuf/protoc-gen-go/descriptor \
			--go_out=plugins=grpc,Mbase_msg.proto=bg/base_msg:../$(GOSRCBG)/cloud_rpc \
			$(notdir $<)

base/cloud_rpc_pb2.py: base/cloud_rpc.proto
	$(PYTHON3) -m grpc_tools.protoc \
		-I. \
		-Ibase \
		--python_out=. --grpc_python_out=. $<

$(PROTOC_PLUGINS):
	$(GO) get -u github.com/golang/protobuf/proto
	$(GO) get -u github.com/golang/protobuf/protoc-gen-go
	$(GO) get -u sourcegraph.com/sourcegraph/prototools/cmd/protoc-gen-doc

LOCAL_COMMANDS=$(COMMANDS:$(APPBIN)/%=$(GOPATH)/bin/%)
LOCAL_DAEMONS=$(DAEMONS:$(APPBIN)/%=$(GOPATH)/bin/%)

# Generate a hash of the contents of BUILDTOOLS, so that if the required
# packages change, we'll rerun the check.
BUILDTOOLS_HASH=$(shell echo $(BUILDTOOLS) | md5sum | awk '{print $$1}')
BUILDTOOLS_FILE=.tools-$(BUILDTOOLS_HASH)

tools: $(BUILDTOOLS_FILE)

$(BUILDTOOLS_FILE):
	build/check-tools.sh $(BUILDTOOLS)
	touch $@

#
# Go Dependencies: Pull in definitions for 'dep'
#
include Makefile.godeps

NPM = npm
client-web/.npm-installed: client-web/package.json
	(cd client-web && $(NPM) install)
	touch $@

client-web: client-web/.npm-installed FRC | $(HTTPD_CLIENTWEB_DIR)
	$(RM) -fr $(HTTPD_CLIENTWEB_DIR)/*
	(cd client-web && $(NPM) run build)
	tar -C client-web/dist -c -f - . | tar -C $(HTTPD_CLIENTWEB_DIR) -xvf -

FRC:

clobber: clean clobber-packages clobber-godeps
	$(RM) -fr $(ROOT)
	$(RM) -fr $(GOWS)/pkg
	$(RM) -fr $(GOWS)/bin

clobber-packages:
	-$(RM) -fr bg-appliance_*.*.*-*_* bg-cloud_*.*.*-*_*

clean:
	$(RM) -f \
		.tools-* \
		base/base_def.py \
		base/base_msg_pb2.py \
		base/cloud_rpc_pb2.py \
		$(GOSRCBG)/base_def/base_def.go \
		$(GOSRCBG)/base_msg/base_msg.pb.go \
		$(GOSRCBG)/cloud_rpc/cloud_rpc.pb.go \
		$(APPBINARIES) \
		$(CLOUDBINARIES) \
		$(UTILBINARIES) \
		$(GO_MOCK_SRCS)

plat-clobber: clobber
	-$(GO) clean $(GO_CLEAN_FLAGS) github.com/golang/protobuf/protoc-gen-go
	-$(GO) clean $(GO_CLEAN_FLAGS) github.com/golang/protobuf/proto
	-$(GO) clean $(GO_CLEAN_FLAGS) sourcegraph.com/sourcegraph/prototools/cmd/protoc-gen-doc
	-$(RM) -fr golang/src/github.com golang/src/golang.org golang/src/google.golang.org golang/src/sourcegraph.com
