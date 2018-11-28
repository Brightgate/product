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
      <f7-nav-left>&nbsp;</f7-nav-left>

      <f7-nav-title>
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

      <f7-nav-right>
        <span font-size="small">
          <f7-link v-if="loggedIn" @click="attemptLogout()">{{ $t('message.home.tools.logout') }}</f7-link>
          <f7-link v-else login-screen-open="#bgLoginScreen">{{ $t('message.home.tools.login') }}</f7-link>
        </span>
      </f7-nav-right>
    </f7-navbar>

    <template v-if="alertCount(alertActive(alerts))">
      <f7-block-title>{{ $t("message.alerts.serious_alerts") }}</f7-block-title>
      <f7-list>
        <f7-list-item
          v-for="alert in alertActive(alerts)"
          :key="alert.deviceID + '-' + alert.vulnid"
          :link="`/devices/${alert.deviceID}/`">
          <span>
            <f7-icon f7="bolt_round_fill" color="red" />
            {{ $t('message.alerts.problem_on_device',
                  {problem: vulnHeadline(alert.vulnid), device: deviceByUniqID(alert.deviceID).networkName})
            }}
          </span>
        </f7-list-item>
      </f7-list>
    </template>

    <f7-block-title>{{ $t("message.home.tools.tools") }}</f7-block-title>
    <f7-list>
      <f7-list-item
        :title="$t('message.home.tools.site_status')"
        :class="loggedIn ? '' : 'disabled'"
        link="/site_status/" />
      <f7-list-item
        :title="$t('message.home.tools.compliance_report')"
        :class="loggedIn ? '' : 'disabled'"
        link="/compliance_report/" />
      <f7-list-item
        :title="$t('message.home.tools.manage_devices', {'device_count': deviceCount(devices)})"
        :class="loggedIn ? '' : 'disabled'"
        link="/devices/" />
      <f7-list-item
        :title="$t('message.home.tools.users')"
        :class="loggedIn ? '' : 'disabled'"
        link="/users/" />
      <f7-list-item
        :title="$t('message.home.tools.enroll_guest')"
        :class="loggedIn ? '' : 'disabled'"
        link="/enroll_guest/" />
    </f7-list>

    <f7-block-title>{{ $t("message.notifications.notifications") }}</f7-block-title>
    <f7-list>
      <f7-list-item
        v-for="device in devices"
        v-if="device.notification"
        :key="device.uniqid"
        :title="$t('message.notifications.update_device', {'device': device.networkName})"
        :link="`/devices/${device.uniqid}/`" />
    </f7-list>

    <f7-block-title>{{ $t("message.home.testing.testing") }}</f7-block-title>
    <f7-list>
      <f7-list-item :title="$t('message.home.testing.enable_mock')">
        <f7-toggle slot="after" :checked="mock" @change="toggleMock" />
      </f7-list-item>
      <f7-list-item :title="$t('message.home.testing.enable_fakelogin')">
        <f7-toggle slot="after" :checked="fakeLogin" @change="toggleFakeLogin" />
      </f7-list-item>
      <f7-list-item
        :title="$t('message.home.testing.accept_devices')"
        :class="loggedIn ? '' : 'disabled'">
        <span slot="after">
          <f7-button fill @click="acceptSupreme">{{ $t("message.general.accept") }}
          </f7-button>
        </span>
      </f7-list-item>

      <!-- this looks weak, and the right answer would be to use
           f7's smart-select, but it's a beast to get right -->
      <f7-list-item :class="loggedIn ? '' : 'disabled'" item-input
                    inline-label>
        <f7-label>{{ $t('message.home.testing.switch_appliance') }}</f7-label>
        <f7-input
          :value="currentApplianceID"
          type="select"
          @change="onApplianceChange">
          <option
            v-for="appliance in applianceIDs"
            :key="appliance"
            :value="appliance">
            {{ appliance === "0" ? "Local Appliance" : appliance }}
          </option>
        </f7-input>
      </f7-list-item>
    </f7-list>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Debug from 'debug';

import vulnerability from '../vulnerability';
const debug = Debug('page:home');

export default {
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
      'applianceIDs',
      'currentApplianceID',
      'deviceByUniqID',
      'deviceCount',
      'devices',
      'fakeLogin',
      'loggedIn',
      'mock',
    ]),
  },

  methods: {
    toggleMock: function(evt) {
      debug('toggleMock', evt);
      this.$store.commit('setMock', evt.target.checked);
      this.$store.dispatch('fetchApplianceIDs').catch(() => {});
      this.$store.dispatch('fetchDevices').catch(() => {});
    },

    toggleFakeLogin: function(evt) {
      debug('toggleFakeLogin', evt);
      this.$store.commit('setFakeLogin', evt.target.checked);
    },

    acceptSupreme: async function() {
      let message = null;
      try {
        const result = await this.$store.dispatch('supreme');
        const c = result.changed ? result.changed : -1;
        message = this.$t('message.home.testing.accept_success', {'devicesChanged': c});
      } catch (err) {
        debug('supreme fail', err);
        message = this.$t('message.home.testing.accept_fail', {'reason': err.message});
      }
      this.acceptToast = this.$f7.toast.create({
        text: message,
        closeButton: true,
      });
      this.acceptToast.open();
    },

    attemptLogout: function() {
      this.$store.dispatch('logout', {});
    },

    vulnHeadline: function(vulnid) {
      return vulnerability.headline(vulnid);
    },

    onApplianceChange: function(evt) {
      debug('onApplianceChange', evt.target.value);
      this.$store.dispatch('setCurrentApplianceID', {id: evt.target.value});
    },

    onPageBeforeIn: async function() {
      // We do these optimistically, letting them fail if not logged in.
      this.$store.dispatch('fetchApplianceIDs').catch(() => {});
      this.$store.dispatch('fetchDevices').catch(() => {});
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
