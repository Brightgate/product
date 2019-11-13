<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style>
.ios .panel-left .page-content {
  background: #ffffff;
}
</style>

<style scoped>
.link-panel >>> .list-group-title {
  background: none;
  margin-top: 12px;
}

.link-panel >>> .item-content {
  line-height: 24px;
  min-height: 24px;
}

.link-panel >>> i.icon {
  margin-right: 4px;
}

.link-panel >>> .item-inner {
  padding: 4px 16px 8px 0px;
  line-height: 28px;
  min-height: 28px;
}

.org-switch-container >>> .item-inner {
  padding-right: 0;
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

    <f7-list class="link-panel" no-hairlines no-hairlines-between>
      <!-- org switcher -->
      <template v-if="appMode === appDefs.APPMODE_CLOUD && currentOrg && orgsCount > 1">
        <f7-list-item group-title>
          {{ $t('message.left_panel.group_organization') }}
        </f7-list-item>
        <f7-list-item class="org-switch-container">
          <bg-org-switch-button :title="currentOrg.name" />
        </f7-list-item>
      </template>
      <f7-list-item group-title>
        {{ $t('message.left_panel.group_tools') }}
      </f7-list-item>

      <f7-list-item
        v-if="appMode === appDefs.APPMODE_LOCAL">
        <f7-link icon-color="gray" icon-material="home" panel-close href="/">
          {{ $t('message.left_panel.home') }}
        </f7-link>
      </f7-list-item>

      <!-- my account -->
      <f7-list-item
        v-if="appMode === appDefs.APPMODE_CLOUD">
        <f7-link icon-color="gray" icon-material="person" panel-close href="/account_prefs/">
          {{ $t('message.left_panel.my_account') }}
        </f7-link>
      </f7-list-item>

      <!-- login/logout -->
      <f7-list-item v-if="loggedIn">
        <f7-link icon-color="gray" icon-material="exit_to_app" @click="onLogout">
          {{ $t('message.general.logout') }}
        </f7-link>
      </f7-list-item>
      <f7-list-item v-else>
        <f7-link icon-color="gray" icon-material="lock_open" panel-close @click="$f7.loginScreen.open('#bgLoginScreen')">
          {{ $t('message.general.login') }}
        </f7-link>
      </f7-list-item>

      <!-- test tools -->
      <f7-list-item v-if="showTestTools">
        <f7-link icon-color="gray" icon-material="bug_report" panel-close href="/test_tools/">
          Test Tools
        </f7-link>
      </f7-list-item>

      <f7-list-item group-title>
        {{ $t('message.left_panel.group_help') }}
      </f7-list-item>
      <f7-list-item>
        <f7-link icon-color="gray" icon-material="book" panel-close href="/help/end_customer_guide">
          {{ $t('message.left_panel.admin_guide') }}
        </f7-link>
      </f7-list-item>
      <f7-list-item>
        <f7-link icon-color="gray" icon-material="live_help" panel-close href="/support/">
          {{ $t('message.left_panel.support') }}
        </f7-link>
      </f7-list-item>
    </f7-list>

    <!-- popup to select org -->
    <bg-org-switch-popup v-if="appMode === appDefs.APPMODE_CLOUD" />

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
