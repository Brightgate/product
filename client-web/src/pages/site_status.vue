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

    <f7-block-title>{{ $t('message.site_status.ssids') }} </f7-block-title>
    <f7-list>
      <f7-list-item :title="$t('message.site_status.ssid_psk')">
        {{ networkConfig.ssid }}
      </f7-list-item>
      <f7-list-item :title="$t('message.site_status.ssid_eap')">
        {{ networkConfig.ssid }}-eap
      </f7-list-item>
    </f7-list>

    <f7-block-title>{{ $t('message.site_status.devices') }} </f7-block-title>
    <f7-list>
      <f7-list-item :title="$t('message.site_status.devices_active')">
        {{ deviceCount(deviceActive(allDevices)) }}
      </f7-list-item>
      <f7-list-item :title="$t('message.site_status.devices_scanned')">
        {{ deviceCount(deviceVulnScanned(deviceActive(allDevices))) }}
      </f7-list-item>
      <f7-list-item :title="$t('message.site_status.devices_reg')">
        {{ deviceCount(allDevices) }}
      </f7-list-item>
    </f7-list>

    <f7-block-title>{{ $t('message.site_status.config') }} </f7-block-title>
    <f7-list>
      <f7-list-item :title="$t('message.site_status.config_dns_server')">
        {{ networkConfig.dnsServer }}
      </f7-list-item>
      <f7-list-item :title="$t('message.site_status.config_default_ring_wpa_psk')">
        {{ networkConfig.defaultRingWPAPSK }}
      </f7-list-item>
      <f7-list-item :title="$t('message.site_status.config_default_ring_wpa_eap')">
        {{ networkConfig.defaultRingWPAEAP }}
      </f7-list-item>
    </f7-list>

    <f7-block-title>Ring Configuration</f7-block-title>
    <f7-list>
      <f7-list-item v-for="(ring, ringName) in rings" :key="ringName" :title="ringName" accordion-item>
        <f7-accordion-content>
          <f7-list inset>
            <f7-list-item title="Authentication">{{ ring.auth }}</f7-list-item>
            <f7-list-item title="Subnet">{{ ring.subnet }}</f7-list-item>
            <f7-list-item title="Lease Duration">
              {{ leaseDurationMinutes(ring.leaseDuration) }}
              {{ ring.leaseDuration >= 120 ? '(' + leaseDuration(ring.leaseDuration) + ')' : "" }}
            </f7-list-item>
          </f7-list>
        </f7-accordion-content>
      </f7-list-item>
    </f7-list>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Promise from 'bluebird';
import {f7AccordionContent} from 'framework7-vue';
import {formatDistanceStrict} from '../date-fns-wrapper';

export default {
  components: {f7AccordionContent},
  data: function() {
    return {
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'mock',
      'loggedIn',
      'allDevices',
      'deviceCount',
      'deviceActive',
      'deviceVulnScanned',
      'networkConfig',
      'rings',
    ]),
  },

  methods: {
    leaseDurationMinutes: function(minutes) {
      return formatDistanceStrict(minutes * 60 * 1000, 0, {'unit': 'minute'});
    },
    leaseDuration: function(minutes) {
      return formatDistanceStrict(minutes * 60 * 1000, 0);
    },

    onPtrRefresh: function(el, done) {
      return Promise.all([
        this.$store.dispatch('fetchRings').catch(() => {}),
        this.$store.dispatch('fetchNetworkConfig').catch(() => {}),
        this.$store.dispatch('fetchDevices').catch(() => {}),
      ]).asCallback(done);
    },

    onPageBeforeIn: function() {
      this.$store.dispatch('fetchRings').catch(() => {});
      this.$store.dispatch('fetchDevices').catch(() => {});
      this.$store.dispatch('fetchNetworkConfig').catch(() => {});
    },
  },
};
</script>
