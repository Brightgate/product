<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->

<!--
  This component renders markup representing a compact summary of the the
  status of a security ring:

   -------------------------------------------------------
   | ✔ #ok | ? #unscanned | ! #vulnerable  | 0 #inactive |
   -------------------------------------------------------

  Properties:
    - devices: an array of devices
    - show-zero: show values even if zeroed [default: false]
-->

<template>
  <span v-if="empty && !showZero">
    <span style="color: rgba(0,0,0,0.3); text-align: center;">
      empty
    </span>
  </span>
  <span v-else>
    <span style="width: 3em; display: inline-block;">
      <span v-if="okCount > 0 || showZero">
        <f7-icon f7="check_round_fill" size="1em" color="green" />
        {{ okCount }}
      </span>
    </span>
    <span style="width: 3em; display: inline-block;">
      <span v-if="unscannedCount > 0 || showZero">
        <f7-icon f7="help_fill" size="1em" color="orange" />
        {{ unscannedCount }}
      </span>
    </span>
    <span style="width: 3em; display: inline-block;">
      <span v-if="vulnCount > 0 || showZero">
        <f7-icon f7="bolt_round_fill" size="1em" color="red" />
        {{ vulnCount }}
      </span>
    </span>
    <span style="width: 3em; display: inline-block;">
      <span v-if="inactiveCount > 0 || showZero">
        <f7-icon f7="circle" size="1em" color="gray" />
        {{ inactiveCount }}
      </span>
    </span>
  </span>
</template>

<script>
import Vuex from 'vuex';

export default {
  name: 'BgRingSummary',

  props: {
    devices: {
      type: Array,
      required: true,
    },
    showZero: {
      type: Boolean,
      required: false,
      default: false,
    },
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...Vuex.mapGetters([
      'Device_Count',
      'Device_VulnScanned',
      'Device_Vulnerable',
      'Device_NotVulnerable',
      'Device_Active',
    ]),

    empty: function() {
      return this.Device_Count(this.devices) === 0;
    },
    okCount: function() {
      return this.Device_Count(
        this.Device_NotVulnerable(
          this.Device_VulnScanned(
            this.Device_Active(this.devices))));
    },
    unscannedCount: function() {
      const active = this.Device_Count(
        this.Device_Active(this.devices));
      const scanned = this.Device_Count(
        this.Device_VulnScanned(
          this.Device_Active(this.devices)));
      return active - scanned;
    },
    vulnCount: function() {
      return this.Device_Count(
        this.Device_Vulnerable(
          this.Device_VulnScanned(
            this.Device_Active(this.devices))));
    },
    inactiveCount: function() {
      return this.Device_Count(this.devices) -
        this.Device_Count(
          this.Device_Active(this.devices));
    },
  },
};
</script>
