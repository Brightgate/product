<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page ptr @ptr:refresh="onPtrRefresh" @page:beforein="onPageBeforeIn" @page:beforeout="onPageBeforeOut">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.site_status.title')" sliding />

    <f7-list>
      <f7-list-group>
        <f7-list-item :title="$t('message.site_status.ssids')" group-title />
        <f7-list-item :title="$t('message.site_status.ssid_psk')">
          {{ Network_Config.ssid }}
        </f7-list-item>
        <f7-list-item :title="$t('message.site_status.ssid_eap')">
          {{ Network_Config.ssid }}-eap
        </f7-list-item>
      </f7-list-group>

      <f7-list-group>
        <f7-list-item :title="$t('message.site_status.devices')" group-title />
        <f7-list-item :title="$t('message.site_status.devices_active')">
          {{ Device_Count(Device_Active(All_Devices)) }}
        </f7-list-item>
        <f7-list-item :title="$t('message.site_status.devices_scanned')">
          {{ Device_Count(Device_VulnScanned(Device_Active(All_Devices))) }}
        </f7-list-item>
        <f7-list-item :title="$t('message.site_status.devices_reg')">
          {{ Device_Count(All_Devices) }}
        </f7-list-item>
      </f7-list-group>

      <f7-list-group>
        <f7-list-item :title="$t('message.site_status.config')" group-title />
        <f7-list-item :title="$t('message.site_status.config_dns_server')">
          {{ Network_Config.dnsServer }}
        </f7-list-item>
        <f7-list-item :title="$t('message.site_status.config_default_ring')">
          {{ Network_Config.defaultRing }}
        </f7-list-item>
      </f7-list-group>
    </f7-list>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Promise from 'bluebird';

export default {
  data: function() {
    return {
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'Mock',
      'Is_Logged_In',
      'All_Devices',
      'Device_Count',
      'Device_Active',
      'Device_VulnScanned',
      'Network_Config',
    ]),
  },

  methods: {
    onPtrRefresh: function(el, done) {
      return Promise.all([
        this.$store.dispatch('fetchNetworkConfig').catch(() => {}),
        this.$store.dispatch('fetchDevices').catch(() => {}),
      ]).asCallback(done);
    },

    onPageBeforeIn: function() {
      console.log('site_status pageBeforeIn');
      if (this.$store.getters.Is_Logged_In) {
        return this.$store.dispatch('fetchNetworkConfig').catch(() => {});
      }
    },

    onPageBeforeOut: function() {
      console.log('site_status pageBeforeOut');
    },
  },
};
</script>
