<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page @page:beforein="onPageBeforeIn" @page:beforeout="onPageBeforeOut">

    <f7-navbar>
      <!-- f7-nav-title doesn't seem to center properly without also
         including left and right. -->
      <f7-nav-left v-if="!leftPanelVisible">
        <f7-link panel-open="left" icon-ios="f7:menu" icon-md="f7:menu" />
      </f7-nav-left>

      <f7-nav-title v-if="!leftPanelVisible">
        <img v-if="this.$f7router.app.theme === 'ios'"
             alt="Brightgate"
             style="padding-top:4px"
             src="img/bglogo_navbar_ios.png"
             srcset="img/bglogo_navbar_ios.png,
                  img/bglogo_navbar_ios@2x.png 2x">
        <img v-else
             alt="Brightgate"
             style="padding-top:4px"
             src="img/bglogo_navbar_md.png"
             srcset="img/bglogo_navbar_md.png,
                  img/bglogo_navbar_md@2x.png 2x">
      </f7-nav-title>
    </f7-navbar>

    <f7-block-title>{{ $t("message.notifications.notifications") }}</f7-block-title>
    <f7-list>
      <f7-list-item
        v-for="device in devices"
        v-if="device.notification"
        :key="device.uniqid"
        :title="$t('message.notifications.update_device', {'device': device.networkName})"
        :link="`/sites/${currentApplianceID}/devices/${device.uniqid}/`" />
    </f7-list>

    <template v-if="appMode === 'appliance'">
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

      <f7-block>
        <h2>{{ $t("message.home.local_appliance") }}</h2>
        {{ $t("message.home.local_appliance_explanation") }}
      </f7-block>
      <f7-block-title>{{ $t("message.home.tools") }}</f7-block-title>
      <bg-site-controls :siteid="'0'" :device-count="deviceCount(devices)" :disabled="!loggedIn" />
    </template>

    <template v-if="appMode === 'cloud'">
      <f7-block>
        <h2>{{ $t("message.home.select_site") }}</h2>
      </f7-block>
      <bg-site-list
        :sites="sites"
        :class="loggedIn ? '' : 'disabled'"
        :current-site="currentApplianceID"
        @site-change="onSiteChange"
      />
    </template>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Debug from 'debug';

import vulnerability from '../vulnerability';
import BGSiteControls from '../components/site_controls.vue';
import BGSiteList from '../components/site_list.vue';
const debug = Debug('page:home');

export default {
  components: {
    'bg-site-controls': BGSiteControls,
    'bg-site-list': BGSiteList,
  },
  data: function() {
    return {
      acceptToast: null,
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
      'currentApplianceID',
      'deviceByUniqID',
      'deviceCount',
      'devices',
      'fakeLogin',
      'leftPanelVisible',
      'loggedIn',
      'mock',
      'sites',
    ]),
  },

  methods: {
    attemptLogout: function() {
      this.$store.dispatch('logout', {});
    },

    vulnHeadline: function(vulnid) {
      return vulnerability.headline(vulnid);
    },

    onSiteChange: function(siteID) {
      debug('onSiteChange', siteID);
      this.$store.dispatch('setCurrentApplianceID', {id: siteID});
    },

    onPageBeforeIn: async function() {
      // We do these optimistically, letting them fail if not logged in.
      this.$store.dispatch('fetchDevices').catch(() => {});
      this.$store.dispatch('fetchAppliances').catch(() => {});
      await this.$store.dispatch('checkLogin');
      if (!this.$store.getters.loggedIn) {
        this.$f7.loginScreen.open('#bgLoginScreen');
      }
    },

    onPageBeforeOut: function() {
      if (this.acceptToast) {
        this.acceptToast.close();
      }
    },
  },
};
</script>
