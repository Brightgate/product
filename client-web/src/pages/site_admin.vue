<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page>
    <f7-navbar :back-link="$t('message.general.back')" :title="site.name" sliding />

    <f7-block>
      <h2>Site {{ site.name }} administration</h2>
      You are administering Brightgate Wi-fi appliances for this site.
    </f7-block>

    <template v-if="alertCount(alertActive(alerts))">
      <f7-block-title>{{ $t("message.alerts.serious_alerts") }}</f7-block-title>
      <f7-list>
        <f7-list-item
          v-for="alert in alertActive(alerts)"
          :key="alert.deviceID + '-' + alert.vulnid"
          :link="`/sites/${currentApplianceID}/devices/${alert.deviceID}/`">
          <span>
            <f7-icon f7="bolt_round_fill" color="red" />
            {{ $t('message.alerts.problem_on_device',
                  {problem: vulnHeadline(alert.vulnid), device: deviceByUniqID(alert.deviceID).networkName})
            }}
          </span>
        </f7-list-item>
      </f7-list>
    </template>

    <f7-block-title>{{ $t("message.home.tools") }}</f7-block-title>
    <bg-site-controls :siteid="site.uniqid" :device-count="deviceCount(devices)" :disabled="!loggedIn" />
  </f7-page>
</template>
<script>
import vuex from 'vuex';
import Debug from 'debug';
import vulnerability from '../vulnerability';

import BGSiteControls from '../components/site_controls.vue';
const debug = Debug('page:site-admin');

export default {
  components: {
    'bg-site-controls': BGSiteControls,
  },

  data: function() {
    return {
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'alertActive',
      'alertCount',
      'alerts',
      'currentApplianceID',
      'deviceByUniqID',
      'deviceCount',
      'devices',
      'loggedIn',
    ]),

    site: function() {
      const siteid = this.$f7route.params.SiteID;
      const x = this.$store.getters.siteByUniqID(siteid);
      debug(`siteid ${siteid}`, x);
      return x;
    },
  },

  methods: {
    vulnHeadline: function(vulnid) {
      return vulnerability.headline(vulnid);
    },

    onPageBeforeIn: async function() {
      // We do these optimistically, letting them fail if not logged in.
      this.$store.dispatch('fetchDevices').catch(() => {});
      this.$store.dispatch('fetchUsers').catch(() => {});
      this.$store.dispatch('fetchNetworkConfig').catch(() => {});
    },
  },
};
</script>
