<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page ptr @ptr:refresh="onPtrRefresh" @page:beforein="onPageBeforeIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.network.title')" sliding />

    <f7-block-title>{{ $t('message.network.networks') }} </f7-block-title>
    <f7-list media-list chevron-center>
      <template v-for="vapName in orderedVAPs">
        <f7-list-item v-if="vaps[vapName] !== undefined"
                      :key="vapName"
                      :title="$t('message.network.names.' + vapName)"
                      :link="`${$f7route.url}vap/${vapName}`">
          <div slot="subtitle">
            <f7-icon material="wifi" size="16" />
            {{ vaps[vapName].ssid }}
          </div>
          <div slot="text">
            {{ $t('message.network.descriptions.' + vapName) }}
          </div>
        </f7-list-item>
      </template>
    </f7-list>

    <f7-block-title>{{ $t('message.network.config') }} </f7-block-title>
    <f7-list>
      <f7-list-item :title="$t('message.network.config_dns_server')">
        {{ networkConfig.dnsServer }}
      </f7-list-item>
      <f7-list-item :title="$t('message.network.config_wan_current')">
        {{ networkConfig.wanCurrent }}
      </f7-list-item>
    </f7-list>
  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Promise from 'bluebird';
import appDefs from '../app_defs';

export default {
  data: function() {
    return {
      appDefs: appDefs,
      orderedVAPs: [appDefs.VAP_EAP, appDefs.VAP_PSK, appDefs.VAP_GUEST],
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'networkConfig',
      'vaps',
    ]),
  },

  methods: {
    onPtrRefresh: function(el, done) {
      return Promise.all([
        this.$store.dispatch('fetchVAPs').catch(() => {}),
        this.$store.dispatch('fetchNetworkConfig').catch(() => {}),
      ]).asCallback(done);
    },

    onPageBeforeIn: function() {
      this.$store.dispatch('fetchVAPs').catch(() => {});
      this.$store.dispatch('fetchNetworkConfig').catch(() => {});
    },
  },
};
</script>
