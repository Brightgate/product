<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>
/*
 * See the Framework7 kitchen sink vue and CSS for more on card styling.
 * See the Vue docs for advice on deep >>> selectors.
 */
/* light grey header with bold font; similar to alerts in device details */
.alert-card >>> .card-header {
        display: block;
        padding: 10px;
        font-size: 16px;
        font-weight: bold;
        background: #f8f8f8;
}
</style>
<template>
  <f7-page>
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.site_alert.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-card class="alert-card">
      <f7-card-header>
        <f7-icon f7="bolt_round_fill" color="red" />
        {{ $t(msgPath('short')) }}
      </f7-card-header>
      <f7-card-content>
        <p>
          <b>{{ $t(msgPath('title')) }}</b>
        </p>
        <p>
          {{ $t(msgPath('text')) }}
        </p>
        <template v-if="checks.length > 0">
          {{ $t(msgPath('check_intro')) }}
          <ul>
            <li v-for="(check, idx) in checks" :key="idx">
              {{ check }}
            </li>
          </ul>
          {{ $t(msgPath('check_final')) }}
        </template>
      </f7-card-content>
      <f7-card-footer>
        <!-- spacer -->
        <span />
        <f7-link :href="mailtoLink" external target="_blank">
          {{ $t('message.site_alert.contact_support') }}</f7-link>
      </f7-card-footer>
    </f7-card>

  </f7-page>
</template>
<script>
import vuex from 'vuex';
import Debug from 'debug';

import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';
const debug = Debug('page:site-alert');

export default {
  components: {
    'bg-site-breadcrumb': BGSiteBreadcrumb,
  },

  data: function() {
    return {
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'orgNameBySiteID',
    ]),

    site: function() {
      const siteid = this.$f7route.params.siteID;
      const x = this.$store.getters.siteByID(siteid);
      debug(`siteid ${siteid}`, x);
      return x;
    },

    checks: function() {
      let i = 1;
      const checks = [];
      while (this.$te(this.msgPath(`checks[${i}]`))) {
        checks.push(this.$t(this.msgPath(`checks[${i}]`)));
        i += 1;
      }
      debug('checks is', checks);
      return checks;
    },

    mailtoLink: function() {
      const siteName = `${this.orgNameBySiteID(this.site.id)} > ${this.site.regInfo.name}`;
      const mailto = 'mailto:support@brightgate.com';
      const title = this.$t(this.msgPath('title'));
      const subject = `Support Request; ${title}: ${siteName}`;
      return `${mailto }?subject=${ encodeURIComponent(subject)}`;
    },
  },

  methods: {
    msgPath: function(msg) {
      return `message.site_alert.${ this.$f7route.params.alertID }.${ msg}`;
    },
  },
};
</script>
