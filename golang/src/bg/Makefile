#
# COPYRIGHT 2020 Brightgate Inc. All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

#
# This Makefile allows the developer to "sit" in golang/src/bg and invoke make
# as usual.  This facilitates some editors (at least those in the vim family)
# to better understand where source files are, since go compiler error messages
# are typically relative to this directory.
#

# Where to find the Makefile to invoke
_INVOKE_ROOT=../../..

# List of targets
_TGTS= \
      install \
      appliance \
      cloud \
      coverage \
      coverage-go \
      test \
      test-go \
      vet-go \
      lint-go \
      fmt-go \
      cilint-go \
      check-go \
      packages \
      clobber \
      clean

# Generate a rule for each target listed in _TGTS
define INVOKE_template =
.PHONY: $(1)
$(1):
	$$(MAKE) -C $$(_INVOKE_ROOT) $(1)

endef

$(foreach tgt,$(_TGTS),$(eval $(call INVOKE_template,$(tgt))))
