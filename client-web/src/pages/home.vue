<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page @page:init="onPageInit" @page:reinit="onPageInit">

    <f7-navbar>
      <!-- f7-nav-title doesn't seem to center properly without also
         including left and right. -->
      <f7-nav-left v-if="!leftPanelVisible">
        <f7-link panel-open="left" icon-ios="f7:menu" icon-md="material:menu" />
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

    <template v-if="appMode === appDefs.APPMODE_LOCAL">
      <f7-block>
        <h2>{{ $t("message.home.local_site") }}</h2>
        {{ $t("message.home.local_site_explanation") }}
      </f7-block>
    </template>

    <template v-if="(appMode === appDefs.APPMODE_CLOUD) && currentOrg">
      <f7-block>
        <h2>{{ currentOrg.name }}</h2>
      </f7-block>
    </template>

    <template v-if="accountNeedsProvisioning">
      <f7-block-title>{{ $t("message.notifications.notifications") }}</f7-block-title>
      <f7-list>
        <f7-list-item
          v-if="accountNeedsProvisioning"
          key="selfProvision"
          :title="$t('message.notifications.self_provision_title')"
          :subtitle="$t('message.notifications.self_provision_text')"
          media-item
          chevron-center
          link="/account_prefs/wifi_provision/">
          <f7-icon slot="media" size="48" ios="f7:alert_fill" md="material:warning" color="yellow" />
        </f7-list-item>
        <!-- XXX the below notifications can never trigger in the current app
          <f7-list-item
          v-for="device in devices"
          v-if="device.notification"
          :key="device.uniqid"
          :title="$t('message.notifications.update_device', {'device': device.displayName})"
          :link="`/sites/${currentSiteID}/devices/${device.uniqid}/`" />
        -->
      </f7-list>
    </template>

    <template v-if="appMode === appDefs.APPMODE_CLOUD">
      <f7-list>
        <f7-list-item
          v-if="currentOrgAdmin"
          link="/accounts/">
          Accounts
        </f7-list-item>
        <bg-site-list
          :sites="sites"
          :class="loggedIn ? '' : 'disabled'"
          :current-site="currentSiteID"
          @site-change="onSiteChange"
        />
      </f7-list>
    </template>

    <template v-if="appMode === appDefs.APPMODE_LOCAL">
      <template v-if="alertCount(alertActive(alerts))">
        <f7-block-title>{{ $t("message.alerts.serious_alerts") }}</f7-block-title>
        <f7-list>
          <f7-list-item
            v-for="alert in alertActive(alerts)"
            :key="alert.deviceID + '-' + alert.vulnid"
            :link="`/sites/${currentSiteID}/devices/${alert.deviceID}/`">
            <div slot="media">
              <f7-icon f7="bolt_circle_fill" color="red" />
            </div>
            <span>
              {{ $t('message.alerts.problem_on_device',
                    {problem: vulnHeadline(alert.vulnid), device: deviceByUniqID(alert.deviceID).displayName})
              }}
            </span>
          </f7-list-item>
        </f7-list>
      </template>

      <f7-block-title>{{ $t("message.home.tools") }}</f7-block-title>
      <bg-site-controls
        :siteid="'0'"
        :active-device-count="activeDeviceCount"
        :inactive-device-count="inactiveDeviceCount"
        :disabled="!loggedIn"
        :app-mode="appMode"
        :admin="siteAdmin" />
    </template>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Debug from 'debug';

import vulnerability from '../vulnerability';
import BGSiteControls from '../components/site_controls.vue';
import BGSiteList from '../components/site_list.vue';
import BGOrgSwitchButton from '../components/org_switch_button.vue';
import appDefs from '../app_defs';
const debug = Debug('page:home');

export default {
  components: {
    'bg-org-switch-button': BGOrgSwitchButton,
    'bg-site-controls': BGSiteControls,
    'bg-site-list': BGSiteList,
  },
  data: function() {
    return {
      appDefs: appDefs,
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'myAccount',
      'alertActive',
      'alertCount',
      'alerts',
      'appMode',
      'currentOrgAdmin',
      'currentSiteID',
      'deviceActive',
      'deviceByUniqID',
      'deviceCount',
      'deviceInactive',
      'devices',
      'fakeLogin',
      'leftPanelVisible',
      'loggedIn',
      'mock',
      'currentOrg',
      'siteAdmin',
      'sites',
    ]),

    activeDeviceCount: function() {
      return this.deviceCount(this.deviceActive(this.devices));
    },
    inactiveDeviceCount: function() {
      return this.deviceCount(this.deviceInactive(this.devices));
    },
    accountNeedsProvisioning: function() {
      if (!this.myAccount) {
        return false;
      }
      /* For now, only show for the user's home Org */
      if (!this.currentOrg || (this.currentOrg.id !== this.myAccount.organizationUUID)) {
        return false;
      }
      const sp = this.myAccount.selfProvision;
      debug('accountNeedsProvisioning:', sp);
      return sp && sp.status && sp.status === 'unprovisioned';
    },
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
      this.$store.dispatch('setCurrentSiteID', {id: siteID});
    },

    onPageInit: async function() {
      // We do these optimistically, letting them fail if not logged in.
      await this.$store.dispatch('checkLogin');
      if (this.$store.getters.loggedIn) {
        this.$store.dispatch('fetchPostLogin');
      } else {
        this.$f7.loginScreen.open('#bgLoginScreen', false);
      }
    },
  },
};
</script>
