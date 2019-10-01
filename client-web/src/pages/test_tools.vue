<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

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

      <f7-nav-right v-if="!leftPanelVisible">
        &nbsp;
      </f7-nav-right>
    </f7-navbar>

    <div>
      <f7-block-title>{{ $t("message.test_tools.testing") }}</f7-block-title>
      <f7-list>
        <f7-list-group>
          <f7-list-item :title="$t('message.test_tools.mocks_group')" group-title />
          <f7-list-item :title="$t('message.test_tools.enable_mock')">
            <f7-toggle slot="after" :checked="mock" @change="toggleMock" />
          </f7-list-item>
          <f7-list-item :title="$t('message.test_tools.enable_fakelogin')">
            <f7-toggle slot="after" :checked="fakeLogin" @change="toggleFakeLogin" />
          </f7-list-item>
        </f7-list-group>
        <f7-list-group>
          <f7-list-item :title="$t('message.test_tools.appmode_group')" group-title />
          <f7-list-item
            :checked="testAppMode === appDefs.APPMODE_NONE"
            :title="$t('message.test_tools.auto_mode')"
            radio
            name="mode-radio"
            @change="setTestAppMode(appDefs.APPMODE_NONE)"
          />
          <f7-list-item
            :checked="testAppMode === appDefs.APPMODE_CLOUD"
            :title="$t('message.test_tools.cloud_mode')"
            radio
            name="mode-radio"
            @change="setTestAppMode(appDefs.APPMODE_CLOUD)"
          />
          <f7-list-item
            :checked="testAppMode === appDefs.APPMODE_LOCAL"
            :title="$t('message.test_tools.local_mode')"
            radio
            name="mode-radio"
            @change="setTestAppMode(appDefs.APPMODE_LOCAL)"
          />
        </f7-list-group>
      </f7-list>
    </div>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Debug from 'debug';
import appDefs from '../app_defs';

const debug = Debug('page:test_tools');

export default {
  components: {
  },
  data: function() {
    return {
      appDefs: appDefs,
      generatedUsername: 'none',
      generatedPassword: 'none',
      verifier: '',
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'appMode',
      'currentSiteID',
      'fakeLogin',
      'leftPanelVisible',
      'loggedIn',
      'mock',
      'testAppMode',
    ]),
  },

  methods: {
    ...vuex.mapActions([
      'logout',
    ]),

    toggleMock: function(evt) {
      debug('toggleMock', evt);
      this.$store.commit('setMock', evt.target.checked);
      this.$store.dispatch('fetchProviders').catch(() => {});
      this.$store.dispatch('checkLogin').catch(() => {});
      this.$store.dispatch('fetchPostLogin').catch(() => {});
    },

    toggleFakeLogin: function(evt) {
      debug('toggleFakeLogin', evt);
      this.$store.commit('setFakeLogin', evt.target.checked);
      this.$store.dispatch('checkLogin').catch(() => {});
    },

    setTestAppMode: function(mode) {
      debug('setTestAppMode', mode);
      this.$store.commit('setTestAppMode', mode);
      // Force the mock to update too
      this.$store.commit('setMock', this.$store.getters.mock);
      this.$store.dispatch('fetchProviders').catch(() => {});
      this.$store.dispatch('checkLogin').catch(() => {});
      this.$store.dispatch('fetchPostLogin').catch(() => {});
    },
  },
};
</script>
