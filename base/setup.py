#!/usr/bin/env python
#
# COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
#
# This copyright notice is Copyright Management Information under 17 USC 1202
# and is included to protect this work and deter copyright infringement.
# Removal or alteration of this Copyright Management Information without the
# express written permission of Brightgate Inc is prohibited, and any
# such unauthorized removal or alteration will be a violation of federal law.
#

from distutils.core import setup

setup(name='base',
      version='0.1.0',
      description='Brightgate Appliance Python Message and Resource Definitions',
      author='Brightgate Inc',
      author_email='stephen@brightate.com',
      py_modules=["base_def", "base_msg_pb2"],
      )
