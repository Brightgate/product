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
      <f7-list-item :after="wan.currentAddress" accordion-item inset title="WAN Link">
        <f7-accordion-content>
          <f7-list inset>
            <f7-list-item title="Current Address">
              {{ wan.currentAddress }}
            </f7-list-item>
            <f7-list-item title="Mode">
              <span v-if="staticWan">
                Statically configured address
              </span>
              <span v-else>
                Assigned by upstream (DHCP)
              </span>
            </f7-list-item>
            <f7-list-item
              v-if="wan.staticAddress"
              title="Static Address">
              {{ wan.staticAddress }}
            </f7-list-item>

            <f7-list-item
              title="Upstream Router Address">
              {{ staticWan ? wan.staticRoute : wan.dhcpRoute }}
            </f7-list-item>
            <f7-list-item
              v-if="!staticWan && wan.dhcpStart"
              title="DHCP Lease Start">
              {{ wan.dhcpStart }}
            </f7-list-item>
            <f7-list-item
              v-if="!staticWan && wan.dhcpDuration"
              title="DHCP Lease Duration">
              {{ wan.dhcpDuration }}
            </f7-list-item>
          </f7-list>
        </f7-accordion-content>
      </f7-list-item>
    </f7-list>
  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Debug from 'debug';
import Promise from 'bluebird';
import appDefs from '../app_defs';
const debug = Debug('page:network');

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
      'wan',
    ]),

    staticWan: function() {
      debug('staticWan: wan is', this.$store.getters.wan.staticAddress);
      return !!this.$store.getters.wan.staticAddress;
    },
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
