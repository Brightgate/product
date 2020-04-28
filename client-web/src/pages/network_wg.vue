<!--
  COPYRIGHT 2020 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>

span.public-key {
  word-wrap: break-word;
  word-break: break-all;
  white-space: normal;
  font-family: monospace;
  font-size: smaller;
  width: 60%;
  text-align: right;
}

</style>
<template>
  <f7-page ptr @ptr:refresh="pullRefresh" @page:beforein="onPageBeforeIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.network_wg.title')" sliding />

    <f7-fab v-if="siteAdmin" color="pink" @click="openEditor">
      <f7-icon size="32" ios="f7:gear_alt_fill" md="material:settings" />
    </f7-fab>

    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-block-title>{{ $t('message.network_wg.properties') }} </f7-block-title>

    <f7-list>
      <f7-list-item :title="$t('message.network_wg.status')">
        {{ wg.enabled ? $t('message.network_wg.enabled') : $t('message.network_wg.disabled') }}
      </f7-list-item>
      <f7-list-item :title="$t('message.network_wg.address')">
        {{ wg.address || $t('message.network_wg.address_none') }}
      </f7-list-item>
      <f7-list-item :title="$t('message.network_wg.port')">
        {{ wg.port || $t('message.network_wg.port_none') }}
      </f7-list-item>
      <f7-list-item :title="$t('message.network_wg.public_key')">
        <span class="public-key">{{ wg.publicKey || '' }}</span>
      </f7-list-item>
    </f7-list>
  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Debug from 'debug';
import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';

const debug = Debug('page:network_wg');

export default {
  components: {
    'bg-site-breadcrumb': BGSiteBreadcrumb,
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'networkConfig',
      'siteAdmin',
      'vaps',
    ]),

    wg: function() {
      return this.networkConfig.wg || {};
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

    openEditor: function() {
      debug('openEditor; current route', this.$f7route);
      const editor = `${this.$f7route.url}/editor`;
      debug('openEditor; navigate to', editor);
      this.$f7router.navigate(editor);
    },

  },
};
</script>
