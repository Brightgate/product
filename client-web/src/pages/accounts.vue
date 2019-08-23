<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page
    ptr
    @ptr:refresh="acctsPullRefresh"
    @page:beforein="onPageBeforeIn">

    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.accounts.title')" sliding />

    <f7-block>
      <h2>{{ currentOrg.name }}</h2>
    </f7-block>
    <f7-list>
      <f7-list-item v-if="accounts.length === 0" disabled>
        <div slot="title">
          <span>
            <f7-icon material="block" />
            <i>{{ $t('message.accounts.none_yet') }}</i>
          </span>
        </div>
      </f7-list-item>
      <f7-list-item v-for="acct in accounts"
                    :key="acct.accountUUID"
                    :title="acct.name"
                    :link="`${$f7route.url}${acct.accountUUID}/`" />
    </f7-list>

  </f7-page>
</template>
<script>
import Debug from 'debug';
import vuex from 'vuex';
const debug = Debug('page:users');

export default {
  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'accountList',
      'accountByID',
      'currentOrg',
    ]),

    accounts: function() {
      const accts = [];
      this.accountList.map((acctID) => {
        const acct = this.accountByID(acctID);
        if (acct) {
          accts.push(acct);
        }
      });
      accts.sort((a, b) => {return a.name.localeCompare(b.name);});
      return accts;
    },
  },

  methods: {
    acctsPullRefresh: async function(event, done) {
      try {
        await this.$store.dispatch('fetchOrgAccounts');
      } catch (err) {
        debug('acctsPullRefresh failed', err);
      }
      done();
    },

    onPageBeforeIn: function() {
      this.$store.dispatch('fetchOrgAccounts');
    },
  },
};
</script>
