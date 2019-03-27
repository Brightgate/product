<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
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
      :title="$t('message.site_controls.compliance_report')"
      :class="disabled ? 'disabled' : undefined"
      :link="`/sites/${siteid}/compliance_report/`" />
    <f7-list-item
      v-if="admin"
      :title="$t('message.site_controls.manage_devices', {'device_count': deviceCount})"
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
    deviceCount: {
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
