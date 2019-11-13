<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>
div.item-title >>> div.vue-avatar--wrapper {
  vertical-align: middle;
}

li.short-media-item {
  padding-top: 0px;
  padding-bottom: 0px;
}

li.short-media-item >>> div.item-media {
  padding-top: 6px;
  padding-bottom: 6px;
}
</style>
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
      <f7-list-item v-for="acct in accounts" :key="acct.accountUUID"
                    :link="`${$f7route.url}${acct.accountUUID}/`"
                    :title="acct.name"
                    class="short-media-item"
                    media-item>
        <vue-avatar slot="media" :src="acct.hasAvatar ? `/api/account/${acct.accountUUID}/avatar` : undefined" :username="acct.name" :size="32" />
      </f7-list-item>
    </f7-list>

  </f7-page>
</template>
<script>
import Debug from 'debug';
import vuex from 'vuex';
import VueAvatar from 'vue-avatar';
const debug = Debug('page:users');

export default {
  components: {
    'vue-avatar': VueAvatar,
  },
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
