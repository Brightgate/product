<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>
span.orgname {
  font-size: 14pt;
}
</style>
<template>
  <f7-page>
    <f7-navbar :back-link="$t('message.general.back')" title="Site Administration" sliding />
    <f7-block>
      <h2>{{ site.regInfo.name }}<br>
        <span class="orgname">{{ site.regInfo.organization }}</span>
      </h2>
      <span v-if="siteAdmin">
        {{ $t('message.site_admin.admin_title') }}
      </span>
      <span v-else>
        {{ $t('message.site_admin.user_title') }}
      </span>
    </f7-block>

    <template v-if="alertCount(alertActive(alerts)) || health.heartbeatProblem || health.configProblem">
      <f7-block-title>{{ $t("message.alerts.serious_alerts") }}</f7-block-title>
      <f7-list media-list>
        <!-- site level alerts -->
        <f7-list-item v-for="siteAlert in siteAlerts"
                      :key="siteAlert"
                      :title="$t(`message.site_alert.${siteAlert}.short`)"
                      :link="`/sites/${currentSiteID}/alerts/${siteAlert}/`">
          <f7-icon slot="media" f7="bolt_round_fill" color="red" />
          <div class="item-text">{{ $t(`message.site_alert.${siteAlert}.title`) }}</div>
        </f7-list-item>

        <!-- alerts for specific client devices -->
        <f7-list-item
          v-for="alert in alertActive(alerts)"
          :key="alert.deviceID + '-' + alert.vulnid"
          :title="$t('message.alerts.vulnerability')"
          :link="`/sites/${currentSiteID}/devices/${alert.deviceID}/`">
          <f7-icon slot="media" f7="bolt_round_fill" color="red" />
          <span slot="text">
            {{
              $t('message.alerts.problem_on_device', {
                problem: vulnHeadline(alert.vulnid),
                device: deviceByUniqID(alert.deviceID).displayName
              })
            }}
          </span>
        </f7-list-item>
      </f7-list>
    </template>

    <f7-block-title>{{ $t("message.home.tools") }}</f7-block-title>
    <bg-site-controls
      :siteid="site.id"
      :device-count="deviceCount(devices)"
      :disabled="!loggedIn"
      :app-mode="appMode"
      :admin="siteAdmin" />
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
      'appMode',
      'currentSiteID',
      'deviceByUniqID',
      'deviceCount',
      'devices',
      'health',
      'loggedIn',
      'siteAdmin',
    ]),

    site: function() {
      const siteid = this.$f7route.params.siteID;
      const x = this.$store.getters.siteByID(siteid);
      debug(`siteid ${siteid}`, x);
      return x;
    },

    siteAlerts: function() {
      const siteAlerts = [];
      if (this.health.heartbeatProblem) {
        siteAlerts.push('heartbeat');
      }
      if (this.health.configProblem) {
        siteAlerts.push('configQueue');
      }
      return siteAlerts;
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
