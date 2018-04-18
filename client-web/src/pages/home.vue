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
                  img/bglogo_navbar_ios@2x.png 2x"/>
      <img v-else
          alt="Brightgate"
          style="padding-top:4px"
          src="img/bglogo_navbar_md.png"
          srcset="img/bglogo_navbar_md.png,
                  img/bglogo_navbar_md@2x.png 2x"/>
    </f7-nav-title>

    <f7-nav-right>
      <span font-size="small">
      <f7-link v-if="Is_Logged_In" @click="attemptLogout()">{{ $t('message.home.tools.logout') }}</f7-link>
      <f7-link v-else login-screen-open="#bgLoginScreen">{{ $t('message.home.tools.login') }}</f7-link>
      </span>
    </f7-nav-right>
  </f7-navbar>

  <f7-block-title v-if="Alerts_Count > 0">{{ $t("message.alerts.serious_alerts") }}</f7-block-title>
  <f7-list v-if="Alerts_Count > 0">
    <f7-list-item
          v-for="alert in Alerts"
          :key="alert.device.uniqid + '-' + alert.vulnid"
          :link="`/devices/${alert.device.uniqid}/`">
      <span>
        <f7-icon f7="bolt_round_fill" color="red"></f7-icon>
        {{ $t('message.alerts.problem_on_device',
             {problem: vulnHeadline(alert.vulnid), device: alert.device.network_name})
        }}
      </span>
    </f7-list-item>
  </f7-list>

  <f7-block-title>{{ $t("message.home.tools.tools") }}</f7-block-title>
  <f7-list>
    <f7-list-item
        link="/site_status/"
        :title="$t('message.home.tools.site_status')"
        :class="Is_Logged_In ? '' : 'disabled'">
    </f7-list-item>
    <f7-list-item
        link="/devices/"
        :title="$t('message.home.tools.manage_devices', {'device_count': Device_Count})"
        :class="Is_Logged_In ? '' : 'disabled'">
    </f7-list-item>
    <f7-list-item
        link="/users/"
        :title="$t('message.home.tools.users')"
        :class="Is_Logged_In ? '' : 'disabled'">
    </f7-list-item>
    <f7-list-item
        link="/enroll_guest/"
        :title="$t('message.home.tools.enroll_guest')"
        :class="Is_Logged_In ? '' : 'disabled'">
    </f7-list-item>
  </f7-list>

  <f7-block-title>{{ $t("message.notifications.notifications") }}</f7-block-title>
  <f7-list>
    <f7-list-item
          v-for="device in All_Devices"
          v-if="device.notification"
          v-bind:key="device.uniqid"
          :title="$t('message.notifications.update_device', {'device': device.network_name})"
          :link="`/devices/${device.uniqid}/`">
    </f7-list-item>
  </f7-list>

  <f7-block-title>{{ $t("message.home.testing.testing") }}</f7-block-title>
  <f7-list>
    <f7-list-item :title="$t('message.home.testing.enable_mock')">
      <f7-toggle slot="after" :checked="Mock" @change="toggleMock"></f7-toggle>
    </f7-list-item>
    <f7-list-item :title="$t('message.home.testing.enable_fakelogin')">
      <f7-toggle slot="after" :checked="Fake_Login" @change="toggleFakeLogin"></f7-toggle>
    </f7-list-item>
    <f7-list-item
        :title="$t('message.home.testing.accept_devices')"
        :class="Is_Logged_In ? '' : 'disabled'">
      <span slot="after">
        <f7-button @click="acceptSupreme" fill>{{ $t("message.general.accept") }}
        </f7-button>
      </span>
    </f7-list-item>
  </f7-list>

</f7-page>
</template>

<script>
import superagent from 'superagent';
import vuex from 'vuex';

import vulnerability from '../vulnerability';

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
      'Mock',
      'Is_Logged_In',
      'Fake_Login',
      'All_Devices',
      'Device_Count',
      'Alerts',
      'Alerts_Count',
    ]),
  },

  methods: {
    toggleMock: function() {
      console.log('toggleMock');
      this.$store.commit('toggleMock');
      this.$store.dispatch('fetchDevices').catch(() => {});
    },

    toggleFakeLogin: function() {
      this.$store.commit('toggleFakeLogin');
    },

    acceptSupreme: function() {
      superagent.get('/apid/supreme').end((err, res) => {
        let message;
        if (err) {
          message = this.$t('message.home.testing.accept_fail', {'reason': err.message});
        } else {
          const res_json = JSON.parse(res.text);
          const c = res_json.changed ? res_json.changed : -1;
          message = this.$t('message.home.testing.accept_success', {'devicesChanged': c});
        }
        this.acceptToast = this.$f7.toast.create({
          text: message,
          closeButton: true,
        });
        this.acceptToast.open();
      });
    },

    attemptLogout: function() {
      this.$store.dispatch('logout', {});
    },

    vulnHeadline: function(vulnid) {
      return vulnerability.headline(vulnid);
    },

  },

  on: {
    pageBeforeIn: function() {
      console.log('home.vue pageBeforeIn');
      if (this.$store.getters.Is_Logged_In) {
        return this.$store.dispatch('fetchDevices').catch(() => {});
      } else {
        this.$f7.loginScreen.open('#bgLoginScreen');
      }
    },

    pageBeforeOut: function() {
      console.log('home.vue pageBeforeOut');
      if (this.acceptToast) {
        this.acceptToast.close();
      }
    },
  },
};
</script>

