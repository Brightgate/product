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
    <!-- Some notes on this view declaration:
         1. Without stackPages, back navigation doesn't work as you'd expect.
            XXX might be able to revisit this when we eject the page below into
            a separate component.
         2. startRoute dynamically computes the URL to load for this view; this
            enables the alternate landing pages such as the malware warning.
            There's probably a better way to pass this information in besides
            a window level property, but I couldn't figure it out.
    -->
    <f7-view id="main-view" :url="startRoute" :stackPages="true" main>
    </f7-view>

  <!-- Login Screen -->
  <f7-login-screen id="bgLoginScreen">
    <f7-view>
      <f7-page login-screen>
        <f7-login-screen-title style="margin-top: 5px">
        <div style="margin-left: 5px; text-align: left" v-on:click="closeLogin()">
          <f7-link color="gray" icon-f7="close_round_fill"></f7-link>
        </div>
        <div>
          <img
            src="img/bglogo_navbar_ios.png"
            srcset="img/bglogo_navbar_ios.png,
                    img/bglogo_navbar_ios@2x.png 2x"/>
        </div>
        {{ $t('message.login.login') }}
        </f7-login-screen-title>

        <f7-list form>
          <f7-list-item>
            <f7-label>{{ $t('message.login.username') }}</f7-label>
            <f7-input
              name="username"
              type="email"
              @input="uid = $event.target.value"
              @keyup.native.enter="attemptLogin"
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
              @keyup.native.enter="attemptLogin"
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
            <span v-if="loginError.res && loginError.res.unauthorized" style="color: red">
              {{ $t('message.login.fail_unauthorized') }}
            </span>
            <span v-else style="color: red">
              {{ $t('message.login.fail_other', {err: loginError.message}) }}
            </span>
          </f7-block>
        </f7-block>

      </f7-page>
    </f7-view>
  </f7-login-screen>

  </div>
</template>

<script>
import Promise from 'bluebird';

export default {
  computed: {
    startRoute() {
      return window.navigateTo ? window.navigateTo : '/';
    },
  },

  data: function() {
    return {
      uid: '',
      userPassword: '',
      loginError: null,
      attemptingLogin: false,
    };
  },

  methods: {
    attemptLogin: function() {
      this.attemptingLogin = true;
      return Promise.delay(250).then(() => {
        return this.$store.dispatch('login',
          {uid: this.uid, userPassword: this.userPassword});
      }).then(() => {
        this.$f7.loginScreen.close();
        this.attemptingLogin = false;
      }).catch((err) => {
        console.log('login err is', err);
        this.loginError = err;
        this.attemptingLogin = false;
      });
    },

    closeLogin: function() {
      this.$f7.loginScreen.close();
    },

  },
};
</script>
