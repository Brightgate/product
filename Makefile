#
# COPYRIGHT 2017 Brightgate Inc. All rights reserved.
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
#    (b) On Linux
#
#	 # apt-get install protobuf-compiler libzmq5-dev libpcap-dev vlan \
#		 bridge-utils
#	 [Retrieve Go tar archive from golang.org and unpack in $HOME.]
#
#    (c) on raspberry pi
#
#	 # apt-get install protobuf-compiler libzmq3-dev libpcap-dev vlan \
#		 bridge-utils
#
#	 [Retrieve Go tar archive from golang.org and unpack in $HOME.]
#
# 2. Each new shell,
#
#	 $ . ./env.sh
#
# 3. To clean out local binaries, use
#
#	 $ make plat-clobber

UNAME_S = $(shell uname -s)
UNAME_M = $(shell uname -m)
$(info kernel UNAME_S=$(UNAME_S))

ifeq ("$(GOROOT)","")
ifeq ("$(UNAME_S)","Darwin")
# On macOS, install the .pkg provided by golang.org.
export GOROOT=/usr/local/go
$(info operating-system macOS)
else
# On Linux
export GOROOT=$(HOME)/go
$(info operating-system Linux)
endif
endif

export GOPATH=$(shell pwd)/golang

GO=$(GOROOT)/bin/go
GO_CLEAN_FLAGS = -i -x
GO_GET_FLAGS = -v

$(info go-version $(shell $(GO) version))
$(info GOROOT $(GOROOT))
$(info GOPATH $(GOPATH))
$(info go env GOOS = $(shell $(GO) env GOOS))
$(info go env GOARCH = $(shell $(GO) env GOARCH))

PROTOC_PLUGINS = \
	$(GOPATH)/bin/protoc-gen-doc \
	$(GOPATH)/bin/protoc-gen-go

# XXX if not GOOS, GOARCH
ifeq ("$(GOARCH)","")
ROOT=proto.$(UNAME_M)
endif

ifeq ("$(GOARCH)","arm")
ROOT=proto.armv7l
endif

GOSRC=golang/src
APPBASE=$(ROOT)/opt/com.brightgate
APPBIN=$(APPBASE)/bin
APPDOC=$(APPBASE)/share/doc
APPETC=$(APPBASE)/etc
APPVAR=$(APPBASE)/var
APPSSL=$(APPETC)/ssl
APPSPOOL=$(APPVAR)/spool

HTTPD_TEMPLATE_DIR=$(APPETC)/templates/ap.httpd
NETWORK_TEMPLATE_DIR=$(APPETC)/templates/ap.networkd

DAEMONS = \
	ap.brokerd \
	ap.configd \
	ap.dhcp4d \
	ap.dns4d \
	ap.httpd \
	ap.identifierd \
	ap.logd \
	ap.mcp \
	ap.networkd \
	ap.sampled \
	ap.scand \
	ap.scand-ssdp

COMMANDS = \
	ap-arpspoof \
	ap-ctl \
	ap-msgping \
	ap-configctl

APPBINARIES  := $(COMMANDS:%=$(APPBIN)/%) $(DAEMONS:%=$(APPBIN)/%)

HTTPD_TEMPLATE_FILES = connect_apple.html.got \
		  connect_generic.html.got \
		  nophish.html.got \
		  stats.html.got
NETWORK_TEMPLATE_FILES = hostapd.conf.got

HTTPD_TEMPLATES := $(HTTPD_TEMPLATE_FILES:%=$(HTTPD_TEMPLATE_DIR)/%)
NETWORK_TEMPLATES := $(NETWORK_TEMPLATE_FILES:%=$(NETWORK_TEMPLATE_DIR)/%)
TEMPLATES = $(HTTPD_TEMPLATES) $(NETWORK_TEMPLATES)

CONFIGS = \
	$(APPETC)/ap_defaults.json \
	$(APPETC)/ap_identities.csv \
	$(APPETC)/ap_mfgid.json \
	$(APPETC)/mcp.json \
	$(APPETC)/oui.txt \
	$(APPETC)/prometheus.yml

DIRS = $(APPBIN) $(APPDOC) $(APPETC) $(APPVAR) $(APPSSL) $(APPSPOOL) \
       $(HTTPD_TEMPLATE_DIR) $(NETWORK_TEMPLATE_DIR)

install: $(APPBINARIES) $(CONFIGS) $(DIRS) $(TEMPLATES) docs

docs: | $(PROTOC_PLUGINS)

$(APPDOC)/: base/base_msg.proto | $(PROTOC_PLUGINS) $(APPDOC)
	cd base && \
		protoc --plugin $(GOPATH)/bin \
		    --doc_out $(APPDOC) $(notdir $<)

$(APPBINARIES) : | $(APPBIN)

$(APPBIN)/%: ./% | $(APPBIN)
	install -m 0755 $< $(APPBIN)

$(APPETC)/ap_defaults.json: ap_defaults.json | $(APPETC)
	install -m 0644 $< $(APPETC)

$(APPETC)/ap_identities.csv: ap_identities.csv | $(APPETC)
	install -m 0644 $< $(APPETC)

$(APPETC)/ap_mfgid.json: ap_mfgid.json | $(APPETC)
	install -m 0644 $< $(APPETC)

$(APPETC)/mcp.json: $(GOSRC)/ap.mcp/mcp.json | $(APPETC)
	install -m 0644 $< $(APPETC)

$(APPETC)/oui.txt: | $(APPETC)
	cd $(APPETC) && curl -s -S -O http://standards-oui.ieee.org/oui.txt

$(APPETC)/prometheus.yml: prometheus.yml | $(APPETC)
	install -m 0644 $< $(APPETC)

$(NETWORK_TEMPLATE_DIR)/hostapd.conf.got: $(GOSRC)/ap.networkd/hostapd.conf.got | $(APPETC)
	install -m 0644 $< $(NETWORK_TEMPLATE_DIR)

$(HTTPD_TEMPLATE_DIR)/%: $(GOSRC)/ap.httpd/% | $(APPETC)
	install -m 0644 $< $(HTTPD_TEMPLATE_DIR)

$(DIRS):
	mkdir -p $@

COMMON_SRCS = \
    $(GOSRC)/base_def/base_def.go \
    $(GOSRC)/base_msg/base_msg.pb.go \
    $(GOSRC)/ap_common/broker.go \
    $(GOSRC)/ap_common/config.go \
    $(GOSRC)/ap_common/mcp/mcp_client.go \
    $(GOSRC)/ap_common/network/network.go

$(APPBINARIES): $(COMMON_SRCS) .gotten

.gotten:
	$(GO) get $(GO_GET_FLAGS) $(DAEMONS) $(COMMANDS) 2>&1 | tee -a get.acc
	touch $@

$(APPBIN)/%:
	cd $(APPBIN) && $(GO) build $*

$(APPBIN)/ap.brokerd: $(GOSRC)/ap.brokerd/brokerd.go
$(APPBIN)/ap.configd: $(GOSRC)/ap.configd/configd.go
$(APPBIN)/ap.dhcp4d: $(GOSRC)/ap.dhcp4d/dhcp4d.go
$(APPBIN)/ap.dns4d: $(GOSRC)/ap.dns4d/dns4d.go golang/src/data/phishtank/phishtank.go
$(APPBIN)/ap.httpd: $(GOSRC)/ap.httpd/httpd.go
$(APPBIN)/ap.identifierd: $(GOSRC)/ap.identifierd/identifierd.go
$(APPBIN)/ap.logd: $(GOSRC)/ap.logd/logd.go
$(APPBIN)/ap.mcp: $(GOSRC)/ap.mcp/mcp.go
$(APPBIN)/ap.networkd: $(GOSRC)/ap.networkd/networkd.go
$(APPBIN)/ap.sampled: $(GOSRC)/ap.sampled/sampled.go
$(APPBIN)/ap.scand: $(GOSRC)/ap.scand/scand.go
$(APPBIN)/ap.scand-ssdp: $(GOSRC)/ap.scand-ssdp/scand-ssdp.go

$(APPBIN)/ap-arpspoof: $(GOSRC)/ap-arpspoof/arpspoof.go
$(APPBIN)/ap-configctl: $(GOSRC)/ap-configctl/configctl.go
$(APPBIN)/ap-ctl: $(GOSRC)/ap-ctl/ctl.go
$(APPBIN)/ap-msgping: $(GOSRC)/ap-msgping/msgping.go

$(APPBIN)/ap-run: ap-run.bash
	install -m 0755 $< $@

proto: $(GOSRC)/base_msg/base_msg.pb.go base/base_msg_pb2.py

$(GOSRC)/base_msg/base_msg.pb.go: base/base_msg.proto | \
	$(PROTOC_PLUGINS) $(GOSRC)/base_msg
	cd base && \
		protoc --plugin $(GOPATH)/bin \
		    --go_out $(GOPATH)/src/base_msg $(notdir $<)

base/base_msg_pb2.py: base/base_msg.proto
	protoc --python_out . $<

$(GOSRC)/base_msg:
	mkdir -p $(GOSRC)/base_msg

LOCAL_BINARIES=$(APPBINARIES:$(APPBIN)/%=$(GOPATH)/bin/%)

clobber: clean
	rm -f $(APPBINARIES) $(CONFIGS)
	rm -f $(LOCAL_BINARIES)

clean:
	rm -f base/base_msg_pb2.py $(GOSRC)/base_msg/base_msg.pb.go

plat-clobber: clobber
	-$(GO) clean $(GO_CLEAN_FLAGS) github.com/golang/protobuf/protoc-gen-go
	-$(GO) clean $(GO_CLEAN_FLAGS) github.com/golang/protobuf/proto
	-$(GO) clean $(GO_CLEAN_FLAGS) sourcegraph.com/sourcegraph/prototools/cmd/protoc-gen-doc
	-cat get.acc | sort -u | xargs $(GO) clean $(GO_CLEAN_FLAGS)
	rm -f get.acc .gotten

$(PROTOC_PLUGINS):
	$(GO) get -u github.com/golang/protobuf/proto
	$(GO) get -u github.com/golang/protobuf/protoc-gen-go
	$(GO) get -u sourcegraph.com/sourcegraph/prototools/cmd/protoc-gen-doc
