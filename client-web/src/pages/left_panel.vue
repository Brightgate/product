<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style>
.ios div.panel-left div.page-content {
  background: #f7f7f8;
}
</style>
<template>
  <f7-page>
    <f7-navbar>
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
    </f7-navbar>

    <bg-org-switch-button v-if="currentOrg && orgsCount > 1" :title="currentOrg.name" />

    <f7-list no-hairlines no-hairlines-between>
      <f7-list-item v-if="appMode === appDefs.APPMODE_CLOUD">
        <f7-link panel-close href="/">
          {{ $t('message.left_panel.select_site') }}
        </f7-link>
      </f7-list-item>
      <f7-list-item v-if="appMode === appDefs.APPMODE_LOCAL">
        <f7-link panel-close href="/">
          {{ $t('message.left_panel.home') }}
        </f7-link>
      </f7-list-item>
      <f7-list-item v-if="appMode === appDefs.APPMODE_CLOUD">
        <f7-link panel-close href="/account_prefs/">
          {{ $t('message.left_panel.my_account') }}
        </f7-link>
      </f7-list-item>
      <f7-list-item v-if="appMode === appDefs.APPMODE_CLOUD && currentOrgAdmin">
        <f7-link panel-close href="/accounts/">
          {{ $t('message.left_panel.accounts') }}
        </f7-link>
      </f7-list-item>
      <f7-list-item>
        <f7-link v-if="loggedIn" @click="onLogout">
          {{ $t('message.general.logout') }}
        </f7-link>
        <f7-link v-else @click="$f7.panel.close('left'); $f7.loginScreen.open('#bgLoginScreen')">
          {{ $t('message.general.login') }}
        </f7-link>
      </f7-list-item>
      <f7-list-item v-if="showTestTools">
        <f7-link panel-close href="/test_tools/">Test Tools</f7-link>
      </f7-list-item>
      <f7-list-item>
        <f7-link panel-close href="/help/end_customer_guide">
          {{ $t('message.left_panel.admin_guide') }}
        </f7-link>
      </f7-list-item>
      <f7-list-item>
        <f7-link panel-close href="/support/">
          {{ $t('message.left_panel.support') }}
        </f7-link>
      </f7-list-item>
    </f7-list>

    <!-- popup to select org -->
    <bg-org-switch-popup />

  </f7-page>
</template>
<script>
import vuex from 'vuex';
import bgOrgSwitchPopup from '../components/org_switch_popup.vue';
import bgOrgSwitchButton from '../components/org_switch_button.vue';
import appDefs from '../app_defs';

export default {
  components: {
    'bg-org-switch-button': bgOrgSwitchButton,
    'bg-org-switch-popup': bgOrgSwitchPopup,
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
      'appMode',
      'currentOrgAdmin',
      'currentOrg',
      'loggedIn',
      'orgsCount',
    ]),
    showTestTools: function() {
      const tt = localStorage.getItem('testTools');
      return !!tt;
    },
  },

  methods: {
    onLogout: async function() {
      await this.$store.dispatch('logout');
      await this.$f7.panel.close('left');
      window.location.reload();
    },
  },
};
</script>
