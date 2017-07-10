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

APPBASE=$(ROOT)/opt/com.brightgate
APPBIN=$(APPBASE)/bin
APPDOC=$(APPBASE)/share/doc
APPETC=$(APPBASE)/etc
APPVAR=$(APPBASE)/var
APPSPOOL=$(APPVAR)/spool

DAEMONS = \
	$(APPBIN)/ap.brokerd \
	$(APPBIN)/ap.configd \
	$(APPBIN)/ap.dhcp4d \
	$(APPBIN)/ap.dns4d \
	$(APPBIN)/ap.httpd \
	$(APPBIN)/ap.identifierd \
	$(APPBIN)/ap.logd \
	$(APPBIN)/ap.mcp \
	$(APPBIN)/ap.networkd \
	$(APPBIN)/ap.sampled \
	$(APPBIN)/ap.scand

COMMANDS = \
	$(APPBIN)/ap-arpspoof \
	$(APPBIN)/ap-ctl \
	$(APPBIN)/ap-msgping \
	$(APPBIN)/ap-configctl \
	$(APPBIN)/ap-run

CONFIGS = \
	$(APPETC)/ap_defaults.json \
	$(APPETC)/ap_identities.csv \
	$(APPETC)/ap_mfgid.json \
	$(APPETC)/mcp.json \
	$(APPETC)/oui.txt \
	$(APPETC)/prometheus.yml

DIRS = $(APPBIN) $(APPDOC) $(APPETC) $(APPVAR) $(APPSPOOL)

install: $(COMMANDS) $(DAEMONS) $(CONFIGS) $(DIRS) docs

docs: | $(PROTOC_PLUGINS)

$(APPDOC)/: base/base_msg.proto | $(PROTOC_PLUGINS) $(APPDOC)
	cd base && \
		protoc --plugin $(GOPATH)/bin \
		    --doc_out $(APPDOC) $(notdir $<)

$(COMMANDS) $(DAEMONS) : | $(APPBIN)

$(APPBIN)/%: ./% | $(APPBIN)
	install -m 0755 $< $(APPBIN)

$(APPETC)/ap_defaults.json: ap_defaults.json | $(APPETC)
	install -m 0644 $< $(APPETC)

$(APPETC)/ap_identities.csv: ap_identities.csv | $(APPETC)
	install -m 0644 $< $(APPETC)

$(APPETC)/ap_mfgid.json: ap_mfgid.json | $(APPETC)
	install -m 0644 $< $(APPETC)

$(APPETC)/mcp.json: golang/src/ap.mcp/mcp.json | $(APPETC)
	install -m 0644 $< $(APPETC)

$(APPETC)/oui.txt: | $(APPETC)
	cd $(APPETC) && curl -s -S -O http://standards-oui.ieee.org/oui.txt

$(APPETC)/prometheus.yml: prometheus.yml | $(APPETC)
	install -m 0644 $< $(APPETC)

$(APPBIN):
	mkdir -p $(APPBIN)

$(APPDOC):
	mkdir -p $(APPDOC)

$(APPETC):
	mkdir -p $(APPETC)

$(APPSPOOL):
	mkdir -p $(APPSPOOL)

$(APPVAR):
	mkdir -p $(APPVAR)

COMMON_SRCS = \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go \
    golang/src/ap_common/broker.go \
    golang/src/ap_common/config.go \
    golang/src/ap_common/mcp/mcp_client.go \
    golang/src/ap_common/network/network.go

$(APPBIN)/ap-arpspoof: \
    golang/src/ap-arpspoof/arpspoof.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap-arpspoof 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap-arpspoof

# XXX brokerd does not need the base messages.
$(APPBIN)/ap.brokerd: \
    golang/src/ap.brokerd/brokerd.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap.brokerd 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap.brokerd

$(APPBIN)/ap.configd: \
    golang/src/ap.configd/configd.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap.configd 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap.configd

$(APPBIN)/ap.dhcp4d: \
    golang/src/ap.dhcp4d/dhcp4d.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap.dhcp4d 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap.dhcp4d

$(APPBIN)/ap.dns4d: \
    golang/src/ap.dns4d/dns4d.go \
    golang/src/data/phishtank/phishtank.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap.dns4d 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap.dns4d

$(APPBIN)/ap.httpd: \
    golang/src/ap.httpd/httpd.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap.httpd 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap.httpd

$(APPBIN)/ap.identifierd: \
    golang/src/ap.identifierd/identifierd.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap.identifierd 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap.identifierd

$(APPBIN)/ap.logd: \
    golang/src/ap.logd/logd.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap.logd 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap.logd

$(APPBIN)/ap.mcp: \
    golang/src/ap.mcp/mcp.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap.mcp 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap.mcp

$(APPBIN)/ap.networkd: \
    golang/src/ap.networkd/networkd.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap.networkd 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap.networkd

$(APPBIN)/ap.sampled: \
    golang/src/ap.sampled/sampled.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap.sampled 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap.sampled

$(APPBIN)/ap.scand: \
    golang/src/ap.scand/scand.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap.scand 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap.scand

$(APPBIN)/ap-configctl: \
    golang/src/ap-configctl/configctl.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap-configctl 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap-configctl

$(APPBIN)/ap-ctl: \
    golang/src/ap-ctl/ctl.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap-ctl 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap-ctl

$(APPBIN)/ap-msgping: \
    golang/src/ap-msgping/msgping.go \
    $(COMMON_SRCS)
	$(GO) get $(GO_GET_FLAGS) ap-msgping 2>&1 | tee -a get.acc
	cd $(APPBIN) && $(GO) build ap-msgping

$(APPBIN)/ap-run: ap-run.bash
	install -m 0755 $< $@

proto: golang/src/base_msg/base_msg.pb.go base/base_msg_pb2.py

golang/src/base_msg/base_msg.pb.go: base/base_msg.proto | \
	$(PROTOC_PLUGINS) golang/src/base_msg
	cd base && \
		protoc --plugin $(GOPATH)/bin \
		    --go_out $(GOPATH)/src/base_msg $(notdir $<)

base/base_msg_pb2.py: base/base_msg.proto
	protoc --python_out . $<

golang/src/base_msg:
	mkdir -p golang/src/base_msg

LOCAL_COMMANDS=$(COMMANDS:$(APPBIN)/%=$(GOPATH)/bin/%)
LOCAL_DAEMONS=$(DAEMONS:$(APPBIN)/%=$(GOPATH)/bin/%)

clobber: clean
	rm -f $(COMMANDS) $(DAEMONS) $(CONFIGS)
	rm -f $(LOCAL_COMMANDS) $(LOCAL_DAEMONS)

clean:
	rm -f base/base_msg_pb2.py golang/src/base_msg/base_msg.pb.go

plat-clobber: clobber
	-$(GO) clean $(GO_CLEAN_FLAGS) github.com/golang/protobuf/protoc-gen-go
	-$(GO) clean $(GO_CLEAN_FLAGS) github.com/golang/protobuf/proto
	-$(GO) clean $(GO_CLEAN_FLAGS) sourcegraph.com/sourcegraph/prototools/cmd/protoc-gen-doc
	-cat get.acc | sort -u | xargs $(GO) clean $(GO_CLEAN_FLAGS)
	rm -f get.acc

$(PROTOC_PLUGINS):
	$(GO) get -u github.com/golang/protobuf/proto
	$(GO) get -u github.com/golang/protobuf/protoc-gen-go
	$(GO) get -u sourcegraph.com/sourcegraph/prototools/cmd/protoc-gen-doc
