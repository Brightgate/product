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
    <f7-nav-title><img style="padding-top:4px" src="img/bglogo.png"/></f7-nav-title>
    <f7-nav-right>
      <span font-size="small">
      <f7-link v-if="$store.getters.Is_Logged_In" @click="attemptLogout()">{{ $t('message.tools.logout') }}</f7-link>
      <f7-link v-else login-screen-open="#bgLoginScreen">{{ $t('message.tools.login') }}</f7-link>
      </span>
    </f7-nav-right>
  </f7-navbar>
  <f7-block-title>{{ $t("message.status.brightgate_status") }}</f7-block-title>

  <f7-block-title>{{ $t("message.alerts.serious_alerts") }}</f7-block-title>
  <f7-list>
    <f7-list-item
          v-for="device in $store.getters.All_Devices"
          v-if="device.alert"
          v-bind:key="device.uniqid"
          :title="$t('message.alerts.wannacry', {'device': device.network_name})"
          :link="'/details/?uniqid=' + device.uniqid">
    </f7-list-item>
  </f7-list>

  <f7-block-title>{{ $t("message.tools.tools") }}</f7-block-title>
  <f7-list>
    <f7-list-item
        link="/devices/"
        :title="$t('message.tools.manage_devices', {'device_count': $store.getters.Device_Count})"
        :class="loggedIn ? '' : 'disabled'">
    </f7-list-item>
    <f7-list-item
        link="/users/"
        :title="$t('message.tools.users')"
        :class="loggedIn ? '' : 'disabled'">
    </f7-list-item>
    <f7-list-item
        link="/enroll/"
        :title="$t('message.tools.enroll_guest')"
        :class="loggedIn ? '' : 'disabled'">
    </f7-list-item>
  </f7-list>

  <f7-block-title>{{ $t("message.notifications.notifications") }}</f7-block-title>
  <f7-list>
    <f7-list-item
          v-for="device in $store.getters.All_Devices"
          v-if="device.notification"
          v-bind:key="device.uniqid"
          :title="$t('message.notifications.update_device', {'device': device.network_name})"
          :link="'/details/?uniqid=' + device.uniqid">
    </f7-list-item>
  </f7-list>

  <f7-block-title>{{ $t("message.testing.testing") }}</f7-block-title>
  <f7-list>
    <f7-list-item :title="$t('message.testing.enable_mock')">
      <f7-toggle slot="after" :checked="mocked" @change="toggleMock"></f7-toggle>
    </f7-list-item>
    <f7-list-item :title="$t('message.testing.enable_fakelogin')">
      <f7-toggle slot="after" :checked="fakeLogin" @change="toggleFakeLogin"></f7-toggle>
    </f7-list-item>
    <f7-list-item
        :title="$t('message.testing.accept_devices')"
        :class="loggedIn ? '' : 'disabled'">
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

export default {
  data: function() {
    return {
      acceptToast: null,
    };
  },

  computed: {
    mocked() {
      return this.$store.getters.Mock;
    },
    loggedIn() {
      return this.$store.getters.Is_Logged_In;
    },
    fakeLogin() {
      return this.$store.getters.Fake_Login;
    },
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
          message = this.$t('message.testing.accept_fail', {'reason': err.message});
        } else {
          const res_json = JSON.parse(res.text);
          const c = res_json.changed ? res_json.changed : -1;
          message = this.$t('message.testing.accept_success', {'devicesChanged': c});
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

