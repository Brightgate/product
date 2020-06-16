<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>
div.checkbox-list {
  padding-top: 4px;
  padding-left: 24px;
}
</style>
<template>
  <f7-page>
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.account_roles.title')" sliding />

    <f7-block>
      <h1>
        {{ acct && acct.name ? acct.name : '' }}
      </h1>
      {{ orgNameByID(acct.organizationUUID) }}
    </f7-block>

    <f7-list>
      <f7-list-item group-title>
        {{ $t('message.account_roles.roles_group') }}
      </f7-list-item>
      <f7-list-item v-for="aorole in orderedRoles" :key="aorole.targetOrganization + aorole.relationship">
        <div slot="title">{{ orgNameByID(aorole.targetOrganization) }}
          <div class="checkbox-list">
            <span v-for="role in aorole.limitRoles" :key="role">
              <!-- XXX We can't seem to get f7-checkbox to be reactive to the
                underlying role value when the value changes.  So we attach
                a generation number to the key, which will force vue to
                build out a new component -->
              <f7-checkbox
                :key="`${aorole.targetOrganization}-${role}-${generation}`"
                :checked="accountHasOrgRole(acct.accountUUID, aorole.targetOrganization, role)"
                :class="canEditRole(aorole.targetOrganization, role) ? '' : 'disabled'"
                @change="setRole(aorole.targetOrganization, role, $event.target.checked)"
              />&nbsp;{{ $t(`message.api.roles.${role}`) }}&nbsp;&nbsp;
            </span>
          </div>
        </div>
      </f7-list-item>
    </f7-list>

  </f7-page>
</template>
<script>
import Debug from 'debug';
import vuex from 'vuex';
import appDefs from '../app_defs';
const debug = Debug('page:account_roles');

export default {
  data: function() {
    return {
      generation: 0,
      appDefs: appDefs,
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'accountByID',
      'orgNameByID',
      'orgs',
      'myAccountUUID',
      'accountHasOrgRole',
    ]),

    acct: function() {
      const accountID = this.$f7route.params.accountID;
      return this.$store.getters.accountByID(accountID);
    },

    orderedRoles: function() {
      const acct = this.acct;
      debug('orderedRoles, acct is', acct);
      if (!acct.roles) {
        return [];
      }
      const ordered = [...acct.roles];
      ordered.sort((a, b) => {
        if (a.relationship === 'self' && b.relationship !== 'self') {
          return -1;
        }
        const aOrgName = this.orgNameByID(a.targetOrganization);
        const bOrgName = this.orgNameByID(b.targetOrganization);
        return aOrgName.localeCompare(bOrgName);
      });
      return ordered;
    },
  },

  methods: {
    onPageBeforeIn: function() {
      debug('onPageBeforeIn');
      const accountID = this.$f7route.params.accountID;
      this.$store.dispatch('fetchAccountRoles', accountID);
    },

    canEditRole: function(tgtOrgUUID, role) {
      const accountID = this.$f7route.params.accountID;
      const myAccount = this.accountByID(this.myAccountUUID);
      if (accountID === this.myAccountUUID &&
        tgtOrgUUID === myAccount.organizationUUID &&
        role === appDefs.ROLE_ADMIN) {
        return false;
      }
      return true;
    },

    setRole: async function(tgtOrgUUID, role, value) {
      const accountID = this.$f7route.params.accountID;
      if (!this.canEditRole(tgtOrgUUID, role)) {
        debug('cannot edit admin\'s own admin role');
        return;
      }
      debug(`setting role ${accountID} tgt=${tgtOrgUUID} role=${role} value=${value}`);
      await this.$store.dispatch('updateAccountRoles',
        {accountID, tgtOrgUUID, role, value});
      await this.$store.dispatch('fetchAccountRoles', accountID);
      // See note above about reactivity.
      this.generation++;
    },
  },
};
</script>
