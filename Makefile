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
#	 # apt-get install protobuf-compiler libzmq5-dev
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

$(info GOOS = $(shell $(GO) env GOOS))
$(info GOARCH = $(shell $(GO) env GOARCH))

ifeq ("$(UNAME_S)","Darwin")
# On macOS, install the .pkg provided by golang.org.
export GOPATH=$(shell pwd)/golang
export GOROOT=/usr/local/go
$(info operating-system macOS)
else
# On Linux
export GOPATH=$(shell pwd)/golang
export GOROOT=$(HOME)/go
$(info operating-system Linux)
endif

GO=$(GOROOT)/bin/go
GO_CLEAN_FLAGS = -i -x
GO_GET_FLAGS = -v

$(info go-version $(shell $(GO) version))
$(info GOROOT $(GOROOT))
$(info GOPATH $(GOPATH))

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

DAEMONS = \
	$(APPBIN)/ap.brokerd \
	$(APPBIN)/ap.dhcp4d \
	$(APPBIN)/ap.dns4d \
	$(APPBIN)/ap.hostapd.m \
	$(APPBIN)/ap.httpd \
	$(APPBIN)/ap.logd

#	$(APPBIN)/ap.configd \

COMMANDS = \
	$(APPBIN)/ap-msgping \
	$(APPBIN)/bg-run \
	$(APPBIN)/pi-netstrap

#	$(APPBIN)/ap-configctl \

install: $(COMMANDS) $(DAEMONS) docs

docs: | $(PROTOC_PLUGINS)

$(APPDOC)/: base/base_msg.proto | $(PROTOC_PLUGINS) $(APPDOC)
	cd base && \
		protoc --plugin $(GOPATH)/bin \
		    --doc_out $(APPDOC) $(notdir $<)

$(COMMANDS) $(DAEMONS) : | $(APPBIN)

$(APPBIN)/%: ./% | $(APPBIN)
	install -m 0755 $< $(APPBIN)

$(APPBIN):
	mkdir -p $(APPBIN)

# XXX brokerd does not need the base messages.
$(APPBIN)/ap.brokerd: \
    golang/src/ap.brokerd/brokerd.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) get $(GO_GET_FLAGS) ap.brokerd
	cd $(APPBIN) && $(GO) build ap.brokerd

$(APPBIN)/ap-configctl: \
    golang/src/ap-configctl/configctl.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) get $(GO_GET_FLAGS) ap-configctl
	cd $(APPBIN) && $(GO) build ap-configctl

$(APPBIN)/ap.configd: \
    golang/src/ap.configd/configd.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) get $(GO_GET_FLAGS) ap.configd
	cd $(APPBIN) && $(GO) build ap.configd

$(APPBIN)/ap.dhcp4d: \
    golang/src/ap.dhcp4d/dhcp4d.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) get $(GO_GET_FLAGS) ap.dhcp4d
	cd $(APPBIN) && $(GO) build ap.dhcp4d

$(APPBIN)/ap.dns4d: \
    golang/src/ap.dns4d/dns4d.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) get $(GO_GET_FLAGS) ap.dns4d
	cd $(APPBIN) && $(GO) build ap.dns4d

$(APPBIN)/ap.hostapd.m: \
    golang/src/ap.hostapd.m/hostapd.m.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) get $(GO_GET_FLAGS) ap.hostapd.m
	cd $(APPBIN) && $(GO) build ap.hostapd.m

$(APPBIN)/ap.httpd: \
    golang/src/ap.httpd/httpd.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) get $(GO_GET_FLAGS) ap.httpd
	cd $(APPBIN) && $(GO) build ap.httpd

$(APPBIN)/ap.logd: \
    golang/src/ap.logd/logd.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) get $(GO_GET_FLAGS) ap.logd
	cd $(APPBIN) && $(GO) build ap.logd

$(APPBIN)/ap-msgping: \
    golang/src/ap-msgping/msgping.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) get $(GO_GET_FLAGS) ap-msgping
	cd $(APPBIN) && $(GO) build ap-msgping

$(APPBIN)/bg-run: bg-run.bash
	install -m 0755 $< $@

$(APPBIN)/pi-netstrap: pi-netstrap.bash
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

clobber: clean
	rm -f $(COMMANDS) $(DAEMONS)

clean:
	rm -f base/base_msg_pb2.py golang/src/base_msg/base_msg.pb.go

plat-clobber: clobber
	-$(GO) clean $(GO_CLEAN_FLAGS) github.com/golang/protobuf/protoc-gen-go
	-$(GO) clean $(GO_CLEAN_FLAGS) github.com/golang/protobuf/proto
	-$(GO) clean $(GO_CLEAN_FLAGS) sourcegraph.com/sourcegraph/prototools/cmd/protoc-gen-doc

$(PROTOC_PLUGINS):
	$(GO) get -u github.com/golang/protobuf/proto
	$(GO) get -u github.com/golang/protobuf/protoc-gen-go
	$(GO) get -u sourcegraph.com/sourcegraph/prototools/cmd/protoc-gen-doc
