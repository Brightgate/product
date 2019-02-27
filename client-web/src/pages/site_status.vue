<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page ptr @ptr:refresh="onPtrRefresh" @page:beforein="onPageBeforeIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.site_status.title')" sliding />

    <f7-block-title>{{ $t('message.site_status.devices') }} </f7-block-title>
    <f7-list>
      <f7-list-item :title="$t('message.site_status.devices_active')">
        {{ deviceCount(deviceActive(devices)) }}
      </f7-list-item>
      <f7-list-item :title="$t('message.site_status.devices_scanned')">
        {{ deviceCount(deviceVulnScanned(deviceActive(devices))) }}
      </f7-list-item>
      <f7-list-item :title="$t('message.site_status.devices_reg')">
        {{ deviceCount(devices) }}
      </f7-list-item>
    </f7-list>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Promise from 'bluebird';
import {f7AccordionContent} from 'framework7-vue';
import appDefs from '../app_defs';

export default {
  components: {f7AccordionContent},
  data: function() {
    return {
      appDefs: appDefs,
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'devices',
      'deviceCount',
      'deviceActive',
      'deviceVulnScanned',
    ]),
  },

  methods: {
    onPtrRefresh: function(el, done) {
      return Promise.all([
        this.$store.dispatch('fetchDevices').catch(() => {}),
      ]).asCallback(done);
    },

    onPageBeforeIn: function() {
      this.$store.dispatch('fetchDevices').catch(() => {});
    },
  },
};
</script>
