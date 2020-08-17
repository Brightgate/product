#!/usr/bin/env python
#
# Copyright 2017 Brightgate Inc.
#
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#


from distutils.core import setup

setup(name='base',
      version='0.1.0',
      description='Brightgate Appliance Python Message and Resource Definitions',
      author='Brightgate Inc',
      author_email='stephen@brightate.com',
      py_modules=["base_def", "base_msg_pb2"],
      )

