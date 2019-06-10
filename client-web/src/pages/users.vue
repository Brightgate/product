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
    @ptr:refresh="usersPullRefresh"
    @page:beforein="onPageBeforeIn">

    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.users.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-list>
      <f7-list-item :group-title="true" :title="$t('message.users.cloud_self_provisioned')" />
      <f7-list-item v-for="user in spUsers"
                    :key ="user.UUID"
                    :link="`${$f7route.url}${user.UUID}/`">
        <div slot="title">
          <f7-icon ios="f7:cloud_fill" md="material:cloud" color="gray" /> {{ user.DisplayName ? user.DisplayName : user.UID }}
        </div>
      </f7-list-item>

      <f7-list-item :group-title="true" :title="$t('message.users.site_specific')" />
      <f7-list-item v-for="user in localUsers"
                    :key ="user.UUID"
                    :link="`${$f7route.url}${user.UUID}/`">
        <div slot="title">
          <f7-icon material="router" color="gray" /> {{ user.DisplayName ? user.DisplayName : user.UID }}
        </div>
      </f7-list-item>
      <f7-list-item
        :title="$t('message.users.add_site_specific')"
        :link="`${this.$f7route.url}NEW/editor/`"
        no-chevron>
        <div slot="inner">
          <f7-icon ios="f7:add_round_fill" md="material:add_circle" />
        </div>
      </f7-list-item>
    </f7-list>

  </f7-page>
</template>
<script>
import {pickBy, orderBy} from 'lodash-es';
import Debug from 'debug';
import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';

const debug = Debug('page:users');

export default {
  components: {
    'bg-site-breadcrumb': BGSiteBreadcrumb,
  },

  computed: {
    spUsers: function() {
      const spu = pickBy(this.$store.getters.users, {SelfProvisioning: true});
      return orderBy(spu, 'DisplayName');
    },
    localUsers: function() {
      const lu = pickBy(this.$store.getters.users, {SelfProvisioning: false});
      return orderBy(lu, 'DisplayName');
    },
  },
  methods: {
    usersPullRefresh: async function(event, done) {
      try {
        await this.$store.dispatch('fetchUsers');
      } catch (err) {
        debug('usersPullRefresh failed', err);
      }
      done();
    },

    onPageBeforeIn: function() {
      this.$store.dispatch('fetchUsers');
    },
  },
};
</script>
