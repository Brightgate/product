<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->

<style scoped>
.org-switch-navbar {
  background: #f8f8f8 !important;
  color: #222 !important;
}

li.hover-item :hover {
  background: #eee !important;
}

span.check-slot {
  width: 24px;
  margin-left: 4px;
  margin-right: 4px;
  display: inline-block;
}

/* when we go to f7 v4 this can go away */
a.org-switch-close {
  border: none;
}
</style>

<template>
  <f7-popup class="org-switch-popup" @popup:opened="onPopupOpened">
    <f7-page>
      <f7-navbar no-shadow no-hairline class="org-switch-navbar">
        <f7-nav-left>
          <f7-button class="org-switch-close" icon-color="black" icon-material="close" popup-close />
        </f7-nav-left>
        <f7-nav-title>Select Organization</f7-nav-title>
      </f7-navbar>
      <f7-list media-list>
        <f7-list-item v-for="(org, orgID) in orgs" :key="orgID" link="#" class="hover-item" @click="selectOrg(orgID)">
          <div slot="media">
            <span class="check-slot">
              <f7-icon v-if="orgID === currentOrg.id" material="check" />
            </span>
            <f7-icon material="business" />
          </div>
          <span><f7-icon v-if="orgID === homeOrgID" material="home" /> {{ org.name }}</span>
        </f7-list-item>
      </f7-list>
    </f7-page>
  </f7-popup>
</template>

<script>
import Vuex from 'vuex';
import Debug from 'debug';

const debug = Debug('component:org_switch_popup');

export default {
  name: 'BgOrgSwitchPopup',

  props: {
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...Vuex.mapGetters([
      'orgs',
      'currentOrg',
      'myAccountUUID',
      'accountByID',
    ]),

    homeOrgID: function() {
      const acct = this.accountByID(this.myAccountUUID);
      if (!acct) {
        return null;
      }
      return acct.organizationUUID;
    },
  },

  methods: {
    // Close the left panel, to avoid the user needing to do so; doing this
    // early (rather than at selectOrg) reduces visual clutter on popup-close.
    onPopupOpened: function() {
      debug('popup opened');
      this.$f7.panel.close();
    },

    // Commit the transaction, force the main view to go back to the top,
    // then close ourself.
    selectOrg: function(orgID) {
      debug('selectOrg', orgID);
      this.$store.commit('setCurrentOrgID', orgID);
      const mainView = this.$f7.views.get('#main-view');
      debug('mainView', mainView);
      mainView.router.navigate('/', {clearPreviousHistory: true});
      this.$f7.popup.close();
    },
  },
};
</script>
