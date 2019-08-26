<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>
h1 { margin-block-end: 0.1em; }
</style>
<template>
  <f7-page @page:beforein="onPageBeforeIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.account_prefs.title')" sliding />

    <f7-block>
      <h1>{{ myAccount.name }}</h1>
      <div>{{ orgNameByID(myAccount.organizationUUID) }}</div>
      <div>{{ myAccount.email }}</div>
      <div>{{ myAccount.phoneNumber }}</div>
    </f7-block>

    <f7-list>
      <f7-list-item
        :title="$t('message.account_prefs.self_provision')"
        link="/account_prefs/self_provision" />
    </f7-list>

    <f7-block-title>Roles</f7-block-title>
    <f7-list>
      <f7-list-item
        v-for="role in orderedRoles"
        :key="role.targetOrganization + role.relationship"
        :title="orgNameByID(role.targetOrganization)">
        {{ formatRoles(role.roles) }}
      </f7-list-item>
    </f7-list>
  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Debug from 'debug';

const debug = Debug('page:accountprefs');

export default {
  data: function() {
    return {
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'loggedIn',
      'myAccountUUID',
      'accountByID',
      'orgNameByID',
    ]),

    myAccount: function() {
      debug('myAccount', this.myAccountUUID);
      return this.accountByID(this.myAccountUUID);
    },

    orderedRoles: function() {
      const acct = this.accountByID(this.myAccountUUID);
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
    formatRoles: function(roleList) {
      if (roleList.length === 0) {
        return this.$t('message.account_prefs.roles_none');
      }
      return roleList.map((role) => {
        return this.$t(`message.api.roles.${role}`);
      }).join(', ');
    },

    onPageBeforeIn: function() {
      this.$store.dispatch('fetchOrgAccounts').catch(() => {});
      this.$store.dispatch('fetchAccountSelfProvision', this.myAccountUUID).catch(() => {});
      this.$store.dispatch('fetchAccountRoles', this.myAccountUUID).catch(() => {});
    },
  },
};
</script>
