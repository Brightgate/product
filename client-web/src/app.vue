<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style>
/*
 * When the left panel is visible due to the breakpoint, we'd like it to have
 * a reasonably well defined border.  Due to the layout framework7 produces,
 * we're pretty limited in the appearance of the border.
 */
div .panel-visible-by-breakpoint {
  border-right: 1px solid rgb(33, 150, 243);
}
</style>
<template>
  <!-- App -->
  <f7-app :params="f7params">

    <!-- Statusbar -->
    <f7-statusbar />

    <!-- Left panel -->
    <f7-panel
      left
      cover
      @panel:breakpoint="onPanelBreakpoint"
    >
      <f7-view url="/left-panel/" links-view=".view-main" />
    </f7-panel>

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
    <f7-view id="main-view" :url="startRoute" :stack-pages="true" :main="true" />

    <!-- Login Screen -->
    <f7-login-screen id="bgLoginScreen">
      <f7-view>
        <f7-page login-screen>
          <f7-login-screen-title style="margin-top: 5px">
            <div style="margin-left: 5px; text-align: left" @click="closeLogin()">
              <f7-link color="gray" icon-f7="close_round_fill" />
            </div>
            <div>
              <img
                src="img/bglogo_navbar_ios.png"
                srcset="img/bglogo_navbar_ios.png,
                    img/bglogo_navbar_ios@2x.png 2x">
            </div>
            {{ $t('message.login.login') }}
          </f7-login-screen-title>

          <f7-list form>
            <f7-list-item>
              <f7-label>{{ $t('message.login.username') }}</f7-label>
              <f7-input
                :placeholder="$t('message.login.username')"
                :value="uid"
                name="username"
                type="email"
                autofocus
                autocomplete="username"
                @input="uid = $event.target.value"
                @keyup.native.enter="attemptLogin" />
            </f7-list-item>
            <f7-list-item>
              <f7-label>{{ $t('message.login.password') }}</f7-label>
              <f7-input
                :placeholder="$t('message.login.password')"
                :value="userPassword"
                name="password"
                type="password"
                autocomplete="current-password"
                @input="userPassword = $event.target.value"
                @keyup.native.enter="attemptLogin" />
            </f7-list-item>
          </f7-list>

          <f7-block>
            <f7-button fill @click="attemptLogin">
              {{ $t('message.login.sign_in') }}
              <f7-preloader v-if="attemptingLogin" color="white" />
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

  </f7-app>
</template>

<script>
import assert from 'assert';
import {f7App, f7Statusbar, f7LoginScreen, f7LoginScreenTitle} from 'framework7-vue';
import Promise from 'bluebird';
import Debug from 'debug';
import routes from './routes';
const debug = Debug('page:app.vue');

export default {
  components: {f7App, f7Statusbar, f7LoginScreen, f7LoginScreenTitle},

  data: function() {
    return {
      f7params: {
        id: 'net.b10e.appliance-admin',
        name: 'Brightgate Appliance Admin Tool',
        theme: 'auto',
        routes: routes,
        debugger: true,
        panel: {
          leftBreakpoint: 960,
        },
      },
      uid: '',
      userPassword: '',
      loginError: null,
      attemptingLogin: false,
    };
  },
  computed: {
    startRoute() {
      return window.navigateTo ? window.navigateTo : '/';
    },
  },

  beforeMount: function() {
    // Allow to run async
    this.$store.dispatch('checkLogin'
    ).then(() => {
      this.$store.dispatch('fetchPeriodic');
    });
  },

  methods: {
    attemptLogin: async function() {
      debug('attempting login');
      this.attemptingLogin = true;
      try {
        await Promise.delay(350);
        await this.$store.dispatch('login',
          {uid: this.uid, userPassword: this.userPassword});
        this.$f7.loginScreen.close();
        this.attemptingLogin = false;
      } catch (err) {
        debug('login err is', err);
        this.loginError = err;
        this.attemptingLogin = false;
      }
    },

    closeLogin: function() {
      this.$f7.loginScreen.close();
    },

    // Fired when the visibility due to the "breakpoint" changes-- either because
    // the panel became invisible (size reduces) or the panel becomes visible
    // (size increases).
    onPanelBreakpoint: function(evt) {
      debug('panel breakpoint', evt);
      assert(evt.target.classList instanceof DOMTokenList);
      const visible = evt.target.classList.contains('panel-visible-by-breakpoint');
      this.$store.commit('setLeftPanelVisible', visible);
    },
  },
};
</script>
