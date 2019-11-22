<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->

<style scoped>
/* Backdrop */
div.bg-login-screen >>> div.login-screen-content {
  background: #384b6a;
  background-repeat: no-repeat;
  background-size: cover;
  background-image:
      url("../assets/login_backdrop.jpg"),
      radial-gradient(ellipse at bottom right, #f5f5f5, #384b6a 60%);
  height: 100%;
}

a.close-button {
  display: block;
  float: right;
  margin: 10px;
}

/*
 * Creates a div which starts 20% of the way down the page, stretching to the
 * bottom of the page (viewport).  The div is 80% of the width of the page,
 * up to 480px (a common max width in F7), and is centered on the page.
 *
 * Children are subject to flex layout in a single column.
 */
div.page-contents {
  position: fixed;
  /* vertical extent */
  top: 20%;
  height: 80%;
  /* horizontal extent; 'translate' and 'left: 50%' work together to center */
  width: 80%;
  max-width: 480px;
  left: 50%;
  transform: translate(-50%);
  /* flex rules for children */
  display: flex;
  flex-direction: column;
}

/* Generic content container -- similar to a card */
div.container {
  background: rgba(255, 255, 255, 0.80);
  border-radius: 4px;
  box-sizing: border-box;
  padding: 10px;
  margin: 20px 0px;
}

/* Login error related rules */
div.login-error-sizer {
  min-height: 60px;
  padding: 0;
}

div.container.login-error {
  width: 100%;
  background: var(--bg-color-red-5);
  border-left: 4px solid var(--bg-color-red-50);
  /* flex rules for children */
  display: flex;
  align-items: center;
}

div.login-error i.error-icon {
  margin: 15px;
  margin-left: 5px;
}

/* Cloud login related rules */
div.container.cloud-login-container {
  /* Keep this container snug around its contents */
  width: fit-content;
  margin-left: auto;
  margin-right: auto;
}

/*
 * Cloud login buttons.  This is distilled from
 * - https://developers.google.com/identity/branding-guidelines
 * - https://docs.microsoft.com/en-us/azure/active-directory/develop/howto-add-branding-in-azure-ad-apps
 */
a.sign-in-button-flex {
  background: white;
  /* Suppress SHOUTING BUTTONS as per guidelines */
  text-transform: none;
  color: #5e5e5e;
  padding: 6px 12px;
  margin: 20px;
  /* flex rules for children */
  display: flex;
  align-items: center;
}

a.sign-in-button-flex .logo {
  display: block;
  padding-right: 12px;
  width: 21px;
  height: 21px;
}

/* Appliance login related rules */
div.container.appliance-login-container {
  width: 100%;
}
</style>

<template>
  <f7-login-screen :id="id">
    <f7-page class="bg-login-screen" login-screen>
      <f7-link class="close-button" color="gray" icon-f7="xmark_circle_fill" @click="closeLogin()" />
      <img :alt="$t('message.login.login')"
           class="login-bg-logo"
           width="300"
           src="../assets/bglogo_white@2x.png"
           srcset="../assets/bglogo_white@2x.png 1x
          ../assets/bglogo_white@4x.png 2x
          ../assets/bglogo_white@6x.png 3x">

      <div class="page-contents">

        <div class="login-error-sizer">
          <div v-if="error" class="container login-error">
            <f7-icon class="error-icon" material="error" color="red" size="28" />
            <div class="error-text">
              <p>
                {{ error }}
              </p>
              <p v-if="authProvidersError">
                The error encountered was <i>{{ authProvidersError }}</i>
              </p>
            </div>
          </div>
        </div>

        <template v-if="appMode === appDefs.APPMODE_LOCAL">
          <div class="container appliance-login-container">
            <f7-list form>
              <f7-list-input
                :label="$t('message.login.username')"
                :placeholder="$t('message.login.username')"
                :value="uid"
                :validate="true"
                :validate-on-blur="true"
                name="username"
                type="text"
                autofocus
                autocomplete="username"
                pattern=".{1,}"
                required
                @focus="loginError = null"
                @input="uid = $event.target.value"
                @keyup.native.enter="attemptLogin" />
              <f7-list-input
                :label="$t('message.login.password')"
                :placeholder="$t('message.login.password')"
                :value="userPassword"
                :validate="true"
                :validate-on-blur="true"
                name="password"
                type="password"
                autocomplete="current-password"
                pattern=".{1,}"
                required
                @focus="loginError = null"
                @input="userPassword = $event.target.value"
                @keyup.native.enter="attemptLogin" />
            </f7-list>
            <f7-block>
              <f7-button fill @click="attemptLogin">
                {{ $t('message.login.sign_in') }}
              </f7-button>
            </f7-block>
          </div>
        </template>

        <template v-else-if="appMode === appDefs.APPMODE_CLOUD">
          <div class="container cloud-login-container">
            <div v-for="ap in authProviders" :key="ap">
              <template v-if="ap === 'google'">
                <f7-button class="sign-in-button-flex" external href="/auth/google" raised>
                  <google-logo-icon class="logo" />
                  <div>
                    {{ $t('message.login.oauth2_google') }}
                  </div>
                </f7-button>
              </template>
              <template v-else-if="ap === 'azureadv2'">
                <f7-button class="sign-in-button-flex" external href="/auth/azureadv2" raised>
                  <ms-logo-icon class="logo" />
                  <div>
                    {{ $t('message.login.oauth2_microsoft') }}
                  </div>
                </f7-button>
              </template>
              <template v-else>
                <f7-button :href="'/auth/'+ap" class="sign-in-button-flex other" external raised>
                  {{ $t('message.login.oauth2_other', {provider: ap}) }}
                </f7-button>
              </template>
            </div>
          </div>
        </template>

      </div>

    </f7-page>
  </f7-login-screen>
</template>

<script>
import assert from 'assert';
import {f7LoginScreen, f7LoginScreenTitle} from 'framework7-vue';
import vuex from 'vuex';
import Promise from 'bluebird';
import Debug from 'debug';
import appDefs from '../app_defs';
import msLogoIcon from '../assets/ms-logo.svg';
import googleLogoIcon from '../assets/google-logo.svg';
const debug = Debug('component:login_screen');

export default {
  components: {f7LoginScreen, f7LoginScreenTitle, msLogoIcon, googleLogoIcon},

  props: {
    id: {
      type: String,
      required: true,
    },
  },

  data: function() {
    return {
      appDefs: appDefs,
      loginError: null,
      uid: '',
      userPassword: '',
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in template.
    ...vuex.mapGetters([
      'appMode',
      'authProviders',
      'authProvidersError',
      'userIDError',
    ]),

    error() {
      if (this.appMode === appDefs.APPMODE_CLOUD) {
        const ue = this.userIDError;
        if (!ue) {
          return undefined;
        }
        switch (ue.reason) {
        case appDefs.LOGIN_REASON.SERVER_ERROR:
          return this.$t('message.login.server_error', ue);
        case appDefs.LOGIN_REASON.NO_OAUTH_RULE_MATCH:
          return this.$t('message.login.no_oauth_rule', ue);
        case appDefs.LOGIN_REASON.NO_ROLES:
          return this.$t('message.login.no_roles', ue);
        case appDefs.LOGIN_REASON.UNKNOWN_ERROR:
          return this.$t('message.login.unknown_error', ue);
        case appDefs.LOGIN_REASON.NO_SESSION:
          return undefined;
        }
        debug('got unknown userIDError', ue);
        assert(false, 'unknown userIDError');
        return undefined;
      } else if (this.appMode === appDefs.APPMODE_LOCAL) {
        const le = this.loginError;
        if (!le) {
          return undefined;
        }
        if (le.response && le.response.status === 401) {
          return this.$t('message.login.fail_unauthorized');
        }
        return this.$t('message.login.fail_other', {err: le.message});
      } else {
        return this.$t('message.login.down');
      }
    },
  },

  methods: {
    attemptLogin: async function() {
      debug('attempting login');
      this.loginError = null;
      this.$f7.preloader.show();
      try {
        await Promise.delay(350);
        await this.$store.dispatch('login',
          {uid: this.uid, userPassword: this.userPassword});
        this.$f7.loginScreen.close();
      } catch (err) {
        debug('login err is', err);
        debug('login err.response is', err.response);
        this.loginError = err;
      } finally {
        this.$f7.preloader.hide();
      }
    },

    closeLogin: function() {
      this.loginError = null;
      this.$f7.loginScreen.close();
    },
  },
};
</script>
