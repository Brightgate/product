
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
        <f7-link panel-close href="/">Select Site</f7-link>
      </f7-list-item>
      <f7-list-item v-if="appMode === appDefs.APPMODE_LOCAL">
        <f7-link panel-close href="/">Home</f7-link>
      </f7-list-item>
      <f7-list-item v-if="appMode === appDefs.APPMODE_CLOUD">
        <f7-link panel-close href="/account_prefs/">My Account</f7-link>
      </f7-list-item>
      <f7-list-item v-if="appMode === appDefs.APPMODE_CLOUD && currentOrgAdmin">
        <f7-link class="bg-panel-link" panel-close href="/accounts/">Accounts</f7-link>
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
        <f7-link panel-close href="/help/end_customer_guide">Admin Guide</f7-link>
      </f7-list-item>
      <f7-list-item>
        <f7-link panel-close href="/support/">Brightgate Support</f7-link>
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
