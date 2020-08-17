<!--
   Copyright 2019 Brightgate Inc.

   This Source Code Form is subject to the terms of the Mozilla Public
   License, v. 2.0. If a copy of the MPL was not distributed with this
   file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


<!--
  This component renders markup representing the core controls
  for a site.  Status, Compliance, Device Management, Users, etc.

  Properties:
    - siteid: A string representing to which site the controls pertain.
    - device-count: How many devices to show in the "manage devices" link
    - disabled: Whether to render all components as disabled
-->

<template>
  <f7-list>
    <f7-list-item
      :title="$t('message.site_controls.network')"
      :class="disabled ? 'disabled' : undefined"
      :link="`/sites/${siteid}/network/`" />
    <f7-list-item
      v-if="admin"
      :title="$t('message.site_controls.hardware')"
      :class="disabled ? 'disabled' : undefined"
      :link="`/sites/${siteid}/nodes/`" />
    <f7-list-item
      v-if="admin"
      :title="$t('message.site_controls.compliance_report')"
      :class="disabled ? 'disabled' : undefined"
      :link="`/sites/${siteid}/compliance_report/`" />
    <f7-list-item
      v-if="admin"
      :title="$t('message.site_controls.manage_devices', {'active_device_count': activeDeviceCount, 'inactive_device_count': inactiveDeviceCount})"
      :class="disabled ? 'disabled' : undefined"
      :link="`/sites/${siteid}/devices/`" />
    <f7-list-item
      v-if="admin"
      :title="$t('message.site_controls.users')"
      :class="disabled ? 'disabled' : undefined"
      :link="`/sites/${siteid}/users/`" />
    <f7-list-item
      :title="$t('message.site_controls.enroll_guest')"
      :class="disabled ? 'disabled' : undefined"
      :link="`/sites/${siteid}/enroll_guest/`" />
  </f7-list>
</template>

<script>

export default {
  name: 'BgSiteControls',

  props: {
    activeDeviceCount: {
      type: Number,
      required: true,
    },
    inactiveDeviceCount: {
      type: Number,
      required: true,
    },
    disabled: {
      type: Boolean,
      required: false,
      default: false,
    },
    siteid: {
      type: String,
      required: true,
    },
    appMode: {
      type: String,
      required: true,
    },
    admin: {
      type: Boolean,
      required: true,
    },
  },
};
</script>

