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
UNAME_S = $(shell uname -s)
$(info kernel UNAME_S=$(UNAME_S))

ifeq ("$(UNAME_S)","Darwin")
# On macOS, install the .pkg provided by golang.org.
export GOPATH=$(shell pwd)/golang
export GOROOT=$(HOME)/go
$(info operating-system macOS)
else
# On Linux
export GOPATH=$(shell pwd)/golang
export GOROOT=$(HOME)/go
$(info operating-system Linux)
endif

GO=$(GOROOT)/bin/go
$(info go-version $(shell $(GO) version))
$(info GOROOT $(GOROOT))
$(info GOPATH $(GOPATH))

PROTOC_PLUGIN=$(GOPATH)/bin/protoc-gen-go

DAEMONS = \
	proto/usr/local/bin/ap.brokerd \
	proto/usr/local/bin/ap.dhcp4d \
	proto/usr/local/bin/ap.dns4d \
	proto/usr/local/bin/ap.hostapd.m \
	proto/usr/local/bin/ap.httpd \
	proto/usr/local/bin/ap.logd

#	proto/usr/local/bin/ap.configd \

COMMANDS = \
	proto/usr/local/bin/ap-configctl \
	proto/usr/local/bin/ap-msgping

install: $(COMMANDS) $(DAEMONS)

proto/usr/local/bin/%: ./% | proto/usr/local/bin
	install -m 0755 $< proto/usr/local/bin

proto/usr/local/bin:
	mkdir -p proto/usr/local/bin

# XXX brokerd does not need the base messages.
./ap.brokerd: \
    golang/src/ap.brokerd/brokerd.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) build ap.brokerd

./ap-configctl: \
    golang/src/ap-configctl/configctl.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) build ap-configctl

./ap.configd: \
    golang/src/ap.configd/configd.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) build ap.configd

./ap.dhcp4d: \
    golang/src/ap.dhcp4d/dhcp4d.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) build ap.dhcp4d

./ap.dns4d: \
    golang/src/ap.dns4d/dns4d.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) build ap.dns4d

./ap.hostapd.m: \
    golang/src/ap.hostapd.m/hostapd.m.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) build ap.hostapd.m

./ap.httpd: \
    golang/src/ap.httpd/httpd.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) build ap.httpd

./ap.logd: \
    golang/src/ap.logd/logd.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) build ap.logd

./ap-msgping: \
    golang/src/ap-msgping/msgping.go \
    golang/src/base_def/base_def.go \
    golang/src/base_msg/base_msg.pb.go
	$(GO) build ap-msgping

proto: golang/src/base_msg/base_msg.pb.go base/base_msg_pb2.py | golang/src/base_msg

golang/src/base_msg/base_msg.pb.go: base/base_msg.proto | $(PROTOC_PLUGIN)
	cd base && \
		protoc --plugin $(PROTOC_PLUGIN) \
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
	-$(GO) clean -i -r -x github.com/golang/protobuf/protoc-gen-go
	-$(GO) clean -i -r -x github.com/golang/protobuf/proto

$(PROTOC_PLUGIN):
	$(GO) get -u github.com/golang/protobuf/proto
	$(GO) get -u github.com/golang/protobuf/protoc-gen-go
