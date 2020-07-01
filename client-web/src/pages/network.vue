<!--
  COPYRIGHT 2020 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>
/*
 * When the accordion-item-opened class gets set on the enclosing li
 * we hide the summarized information in the closed accordion item.
 * Rather than the usual display:none, here we set the opacity to 0
 * (transparent) using a CSS transition to make it smooth.
 */
li.accordion-item >>> span.hide-when-accordion-open {
  opacity: 1;
  transition: 0.4s opacity ease-in;
}

li.accordion-item.accordion-item-opened >>> span.hide-when-accordion-open {
  opacity: 0;
  transition: 0.4s opacity ease-out;
}
</style>
<template>
  <f7-page ptr @ptr:refresh="pullRefresh" @page:beforein="onPageBeforeIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.network.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-block-title>{{ $t('message.network.networks') }} </f7-block-title>
    <f7-list media-list>
      <template v-for="vapName in orderedVAPs">
        <f7-list-item v-if="vaps[vapName] !== undefined"
                      :key="vapName"
                      :title="$t('message.network.names.' + vapName)"
                      :link="`${$f7route.url}vap/${vapName}`"
                      chevron-center>
          <div slot="subtitle">
            <f7-icon material="wifi" size="16" />
            {{ vaps[vapName].ssid }}
          </div>
          <div slot="text">
            {{ $t('message.network.descriptions.' + vapName) }}
          </div>
        </f7-list-item>
      </template>
      <template v-if="features.vpnConfig">
        <f7-list-item :key="'vpn'"
                      :title="$t('message.network.names.vpn')"
                      :link="`${$f7route.url}wg`"
                      chevron-center>
          <div slot="text">
            {{ $t('message.network.descriptions.vpn') }}
          </div>
        </f7-list-item>
      </template>
    </f7-list>

    <f7-block-title>{{ $t('message.network.config') }} </f7-block-title>
    <f7-list>
      <f7-list-item :title="$t('message.network.config_dns_server')">
        <!-- Stopgap; this works if we serve up multiple servers but
             we will want to do better -->
        {{ networkConfig.dns.servers ? networkConfig.dns.servers.join(',') : '' }}
      </f7-list-item>
      <f7-list-item>
        <bg-list-item-title
          slot="title"
          :title="$t('message.network.config_dns_domain')"
          :tip="$t('message.network.config_dns_domain_tip')" />
        {{ networkConfig.dns.domain }}
      </f7-list-item>
      <f7-list-item v-if="networkConfig.wan" accordion-item inset title="WAN Link">
        <span slot="after" class="hide-when-accordion-open">{{ networkConfig.wan.currentAddress }}</span>
        <f7-accordion-content>
          <f7-list inset>
            <f7-list-item title="Current Address">
              {{ networkConfig.wan.currentAddress }}
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
              v-if="networkConfig.wan.staticAddress"
              title="Static Address">
              {{ networkConfig.wan.staticAddress }}
            </f7-list-item>

            <f7-list-item
              title="Upstream Router Address">
              {{ staticWan ? networkConfig.wan.staticRoute : networkConfig.wan.dhcpRoute }}
            </f7-list-item>
            <f7-list-item
              v-if="!staticWan && networkConfig.wan.dhcpStart"
              title="DHCP Lease Start">
              {{ networkConfig.wan.dhcpStart }}
            </f7-list-item>
            <f7-list-item
              v-if="!staticWan && networkConfig.wan.dhcpDuration"
              title="DHCP Lease Duration">
              {{ networkConfig.wan.dhcpDuration }}
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
import appDefs from '../app_defs';

import BGListItemTitle from '../components/list_item_title.vue';
import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';

const debug = Debug('page:network');

export default {
  components: {
    'bg-site-breadcrumb': BGSiteBreadcrumb,
    'bg-list-item-title': BGListItemTitle,
  },
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
      'features',
      'networkConfig',
      'vaps',
    ]),

    staticWan: function() {
      if (!this.networkConfig.wan) {
        return false;
      }
      debug('staticWan: wan is', this.networkConfig.wan.staticAddress);
      return !!this.networkConfig.wan.staticAddress;
    },
  },

  methods: {
    pullRefresh: async function(done) {
      try {
        await this.$store.dispatch('fetchNetworkConfig');
      } finally {
        done();
      }
    },

    onPageBeforeIn: function() {
      this.$store.dispatch('fetchNetworkConfig').catch(() => {});
    },
  },
};
</script>
