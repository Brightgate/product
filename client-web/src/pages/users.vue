<!--
  COPYRIGHT 2020 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>
span.wifi {
  display: inline-block;
  background: #eeeeee;
  font-family: monospace;
  padding: 0.2em;
}
</style>
<template>
  <f7-page
    ptr
    @ptr:refresh="pullRefresh"
    @page:beforein="onPageBeforeIn">

    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.users.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-block-title>
      {{ $t('message.users.cloud_self_provisioned') }}
    </f7-block-title>
    <f7-block>
      <i18n path="message.users.cloud_self_provisioned_explain" tag="span">
        <span place="wifi" class="wifi"><f7-icon material="wifi" size="16" />{{ eapName }}</span>
      </i18n>&nbsp;
      <!-- {{ $t('message.users.cloud_self_provisioned_explain') }} -->
      <template v-if="appMode === appDefs.APPMODE_CLOUD">
        <f7-link href="/accounts/">{{ $t('message.users.manage_accounts') }}</f7-link>.
      </template>
    </f7-block>
    <f7-list v-if="appMode === appDefs.APPMODE_CLOUD">
      <f7-list-item v-if="spUsers.length === 0" disabled>
        <div slot="title">
          <span>
            <f7-icon material="block" />
            <i>{{ $t('message.users.none_provisioned_yet') }}</i>
          </span>
        </div>
      </f7-list-item>
      <f7-list-item v-for="user in spUsers"
                    :key="user.UUID"
                    :link="`/accounts/${user.UUID}/`">
        <div slot="title">
          <f7-icon ios="f7:cloud_fill" md="material:cloud" color="gray" />
          {{ accountByID(user.UUID).name }}
        </div>
      </f7-list-item>
    </f7-list>
    <!-- non-cloud view -->
    <f7-list v-if="appMode === appDefs.APPMODE_LOCAL">
      <f7-list-item v-for="user in spUsers"
                    :key ="user.UUID"
                    :link="`${$f7route.url}${user.UUID}/`">
        <div slot="title">
          <f7-icon ios="f7:cloud_fill" md="material:cloud" color="gray" />
          {{ user.DisplayName ? user.DisplayName : user.UID }}
        </div>
      </f7-list-item>
    </f7-list>

    <f7-block-title>
      {{ $t('message.users.site_specific') }}
    </f7-block-title>
    <f7-block>
      <i18n path="message.users.site_specific_explain" tag="span">
        <span place="wifi" class="wifi"><f7-icon material="wifi" size="16" />{{ eapName }}</span>
      </i18n>
    </f7-block>
    <f7-list>
      <f7-list-item v-for="user in localUsers"
                    :key ="user.UUID"
                    :link="`${$f7route.url}${user.UUID}/`">
        <div slot="title">
          <f7-icon material="router" color="gray" /> {{ user.DisplayName ? user.DisplayName : user.UID }}
        </div>
      </f7-list-item>
      <f7-list-item
        :title="$t('message.users.site_specific_add')"
        :link="`${this.$f7route.url}NEW/editor/`"
        no-chevron>
        <div slot="inner">
          <f7-icon ios="f7:plus_circle_fill" md="material:add_circle" />
        </div>
      </f7-list-item>
    </f7-list>

  </f7-page>
</template>
<script>
import vuex from 'vuex';
import Debug from 'debug';
import {pickBy, orderBy} from 'lodash-es';
import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';
import appDefs from '../app_defs';

const debug = Debug('page:users');

export default {
  components: {
    'bg-site-breadcrumb': BGSiteBreadcrumb,
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
      'accountByID',
      'accountList',
      'vaps',
    ]),

    spUsers: function() {
      const users = this.$store.getters.users;
      debug('spUsers: users', users);
      const spu = pickBy(users, (user) => {
        if (this.appMode !== appDefs.APPMODE_CLOUD) {
          return user.SelfProvisioning;
        }
        // cloud mode; logic is a bit different-- we consult the account
        // as authoritative since the provisioning might be pending.
        // XXX We may need to revisit this if it winds up being too confusing
        // in the window between requesting provisioning and actual
        // provisioning.  Ideally this would go away more completely.
        if (user.SelfProvisioning !== true) {
          return false;
        }
        const acct = this.accountByID(user.UUID);
        return acct && acct.name && acct.selfProvision && acct.selfProvision.status === 'provisioned';
      });
      const o = orderBy(spu, 'DisplayName');
      debug('spUsers', o);
      return o;
    },

    localUsers: function() {
      const lu = pickBy(this.$store.getters.users, {SelfProvisioning: false});
      const o = orderBy(lu, 'DisplayName');
      debug('localUsers', o);
      return o;
    },

    eapName: function() {
      if (this.vaps[appDefs.VAP_EAP]) {
        return this.vaps[appDefs.VAP_EAP].ssid;
      }
      return '';
    },
  },
  methods: {
    pullRefresh: async function(done) {
      try {
        await this.$store.dispatch('fetchUsers');
        await this.$store.dispatch('fetchOrgAccounts');
        this.accountList.forEach((uu) => {
          this.$store.dispatch('fetchAccountSelfProvision', uu);
        });
      } finally {
        done();
      }
    },

    onPageBeforeIn: async function() {
      this.$store.dispatch('fetchUsers');
      this.$store.dispatch('fetchOrgAccounts');
      this.accountList.forEach((uu) => {
        this.$store.dispatch('fetchAccountSelfProvision', uu);
      });
    },
  },
};
</script>
