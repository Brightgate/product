<!--
   Copyright 2019 Brightgate Inc.

   This Source Code Form is subject to the terms of the Mozilla Public
   License, v. 2.0. If a copy of the MPL was not distributed with this
   file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


<style scoped>
li.hover-item :hover {
  background: #eee !important;
}

span.check-slot {
  width: 24px;
  margin-left: 4px;
  margin-right: 4px;
  display: inline-block;
}

/*
 * Normally the footer is a lighter gray color; but the footer is only for
 * disabled items, and so it winds up doubly-lightened. The contrast is
 * then too low; so reset the footer font color to the theme black.
 */
.md span.role-footer {
  color: #212121;
}
.ios span.role-footer {
  color: #000;
}
</style>

<template>
  <f7-popup close-on-escape class="org-switch-popup">
    <f7-page>
      <f7-navbar no-shadow no-hairline class="org-switch-navbar">
        <f7-nav-left>
          <f7-link icon-material="close" popup-close />
        </f7-nav-left>
        <f7-nav-title>Select Organization</f7-nav-title>
      </f7-navbar>
      <f7-list media-list>
        <f7-list-item
          v-for="org in orderedOrgs"
          :key="org.id"
          :class="orgEnabled(org.id) ? '': 'disabled'"
          link="#"
          class="hover-item"
          no-chevron
          @click="selectOrg(org.id)">
          <div slot="media">
            <span class="check-slot">
              <f7-icon v-if="org.id === currentOrg.id" material="check" />
            </span>
            <f7-icon material="business" />
          </div>
          <span><f7-icon v-if="org.id === homeOrgID" material="home" /> {{ org.name }}</span>
          <span v-if="!orgEnabled(org.id)" slot="footer" class="role-footer">{{ $t('message.org_switch_popup.no_roles') }}</span>
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
      'accountOrgRoles',
    ]),

    homeOrgID: function() {
      const acct = this.accountByID(this.myAccountUUID);
      if (!acct) {
        return null;
      }
      return acct.organizationUUID;
    },

    orderedOrgs: function() {
      const ordered = Object.values(this.orgs);
      ordered.sort((a, b) => {
        if (a.id === this.homeOrgID) {
          return -1;
        }
        if (b.id === this.homeOrgID) {
          return 1;
        }
        return a.name.localeCompare(b.name);
      });
      return ordered;
    },
  },

  methods: {
    orgEnabled: function(orgID) {
      const aors = this.accountOrgRoles(this.myAccountUUID, orgID);
      debug('orgDisabled', orgID, aors);
      const result = aors.find((aor) => {
        return (aor.roles.length > 0);
      });
      debug('orgDisabled', result);
      return !!result;
    },

    // Commit the transaction, force the main view to go back to the top,
    // then close ourself.
    selectOrg: function(orgID) {
      debug('selectOrg', orgID);
      this.$f7.panel.close('left', false); // Close, no animation
      this.$store.commit('setCurrentOrgID', orgID);
      const mainView = this.$f7.views.get('#main-view');
      debug('mainView', mainView);
      mainView.router.navigate('/', {clearPreviousHistory: true});
      this.$f7.popup.close();
    },
  },
};
</script>

