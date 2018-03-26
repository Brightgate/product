<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <!-- App -->
  <div id="app">

    <!-- Statusbar -->
    <f7-statusbar></f7-statusbar>

    <!-- Main View -->
    <!-- Note that without stackPages, back navigation doesn't work as you'd
         expect.  XXX might be able to revisit this when we eject the page
         below into a separate component.
    -->
    <f7-view id="main-view" url="/" :stackPages="true" main>
      <f7-page
        v-on:page:init="pageInit"
        v-on:page:beforein="pageInit"
        v-on:page:beforeout="pageBeforeOut">

        <f7-navbar>
          <!-- f7-nav-title doesn't seem to center properly without also
               including left and right. -->
          <f7-nav-left>&nbsp;</f7-nav-left>
          <f7-nav-title><img style="padding-top:4px" src="img/bglogo.png"/></f7-nav-title>
          <f7-nav-right>
            <span font-size="small">
            <f7-link v-if="$store.getters.Is_Logged_In" @click="attemptLogout()">{{ $t('message.tools.logout') }}</f7-link>
            <f7-link v-else @click="openLogin">{{ $t('message.tools.login') }}</f7-link>
            </span>
          </f7-nav-right>
        </f7-navbar>
        <f7-block-title>{{ $t("message.status.brightgate_status") }}</f7-block-title>

        <f7-block-title>{{ $t("message.alerts.serious_alerts") }}</f7-block-title>
        <f7-list>
          <f7-list-item
                v-for="device in $store.getters.All_Devices"
                v-if="device.alert"
                :title="$t('message.alerts.wannacry', {'device': device.network_name})"
                :link="'/details/?uniqid=' + device.uniqid">
          </f7-list-item>
        </f7-list>

        <f7-block-title>{{ $t("message.tools.tools") }}</f7-block-title>
        <f7-list>
          <f7-list-item
		link="/devices/"
		:title="$t('message.tools.manage_devices', {'device_count': $store.getters.Device_Count})"
		:class="$store.getters.Is_Logged_In || $store.getters.Mock ? '' : 'disabled'">
          </f7-list-item>
          <f7-list-item
		link="/users/"
		:title="$t('message.tools.users')"
		:class="$store.getters.Is_Logged_In || $store.getters.Mock ? '' : 'disabled'">
          </f7-list-item>
          <f7-list-item
		link="/enroll/"
		:title="$t('message.tools.enroll_guest')"
		:class="$store.getters.Is_Logged_In ? '' : 'disabled'">
          </f7-list-item>
        </f7-list>

        <f7-block-title>{{ $t("message.notifications.notifications") }}</f7-block-title>
        <f7-list>
          <f7-list-item
                v-for="device in $store.getters.All_Devices"
                v-if="device.notification"
                :title="$t('message.notifications.update_device', {'device': device.network_name})"
                :link="'/details/?uniqid=' + device.uniqid">
          </f7-list-item>
        </f7-list>

        <f7-block-title>{{ $t("message.testing.testing") }}</f7-block-title>
        <f7-list>
          <f7-list-item :title="$t('message.testing.enable_mock')">
            <f7-toggle slot="after" :checked="$store.getters.Mock" @change="$store.commit('toggleMock'); $store.dispatch('fetchDevices').catch((err) => {})"></f7-toggle>
          </f7-list-item>
          <f7-list-item
		:title="$t('message.testing.accept_devices')"
		:class="$store.getters.Is_Logged_In ? '' : 'disabled'">
            <span slot="after">
              <f7-button @click="acceptSupreme" fill>{{ $t("message.general.accept") }}
              </f7-button>
            </span>
          </f7-list-item>
        </f7-list>

      </f7-page>
    </f7-view>

  <!-- Login Screen -->
  <f7-login-screen ref="bgLoginScreen">
    <f7-view>
      <f7-page login-screen>
        <f7-login-screen-title style="margin-top: 5px">
        <div style="margin-left: 5px; text-align: left" v-on:click="closeLogin()">
          <f7-link color="gray" icon-f7="close_round_fill"></f7-link>
        </div>
        <div><img src="img/bglogo.png"/></div>
	{{ $t('message.login.login') }}
        </f7-login-screen-title>

        <f7-list form>
          <f7-list-item>
            <f7-label>{{ $t('message.login.username') }}</f7-label>
            <f7-input
              name="username"
              type="text"
              @input="uid = $event.target.value"
	      @keyup.enter="attemptLogin"
              autofocus
              autocomplete="username"
              :placeholder="$t('message.login.username')"></f7-input>
          </f7-list-item>
          <f7-list-item>
            <f7-label>{{ $t('message.login.password') }}</f7-label>
            <f7-input
              name="password"
              type="password"
              @input="userPassword = $event.target.value"
	      @keyup.enter="attemptLogin"
              autocomplete="current-password"
              :placeholder="$t('message.login.password')"></f7-input>
          </f7-list-item>
        </f7-list>

        <f7-block>
          <f7-button fill v-on:click="attemptLogin">
            {{ $t('message.login.sign_in') }}
            <f7-preloader color="white" v-if="attemptingLogin"></f7-preloader>
          </f7-button>

          <f7-block v-if="loginError">
            Problems logging in: {{ loginError.message }}
          </f7-block>
        </f7-block>

      </f7-page>
    </f7-view>
  </f7-login-screen>

  </div>
</template>

<script>
import superagent from 'superagent';
import Promise from 'bluebird';

export default {

  data: function () {
    return {
      uid: "",
      userPassword: "",
      loginError: null,
      attemptingLogin: false,
      acceptToast: null,
    }
  },

  methods: {
    pageInit: function () {
      // if navigateTo is set then we're on an alternative landing
      // page such as malwareWarn, so don't fire the loggedIn or
      // other behaviors.
      console.log("app.vue pageInit")
      if (!window.navigateTo) {
        if (!this.$store.state.loggedIn) {
          this.$refs.bgLoginScreen.open(true)
        } else {
          return this.$store.dispatch('fetchDevices').catch((err) => {})
        }
      }
    },

    pageBeforeOut: function () {
      console.log("page before out")
      if (this.acceptToast) {
        this.acceptToast.close()
      }
    },

    acceptSupreme: function () {
      superagent.get('/apid/supreme').end((err, res) => {
        var message
        if (err) {
          message = this.$t('message.testing.accept_fail', {'reason': err.message})
        } else {
          var res_json = JSON.parse(res.text)
          var c = res_json.changed ? res_json.changed : -1;
          message = this.$t('message.testing.accept_success', {'devicesChanged': c})
        }
        this.acceptToast = this.$f7.toast.create({ 
          text: message,
          closeButton: true,
        })
        this.acceptToast.open()
      })
    },

    attemptLogin: function () {
      this.attemptingLogin = true
      return Promise.delay(250).then(() => {
        return this.$store.dispatch("login",
          {uid: this.uid, userPassword: this.userPassword})
      }).then(() => {
          this.$refs.bgLoginScreen.close(true)
          this.pageInit()
          this.attemptingLogin = false
      }).catch((err) => {
        this.pageInit()
	this.loginError = err
        this.attemptingLogin = false
      })
    },

    attemptLogout: function () {
      this.$store.dispatch("logout", {})
    },

    openLogin: function () {
      this.$refs.bgLoginScreen.open(true)
    },

    closeLogin: function () {
      this.$refs.bgLoginScreen.close(true)
    },

  },
}
</script>
