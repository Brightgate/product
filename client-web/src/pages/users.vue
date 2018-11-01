<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

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

    <!-- n.b. FAB must be direct child of a page -->
    <f7-fab color="pink" href="false" @click="openEditorNew">
      <f7-icon f7="add" />
    </f7-fab>

    <f7-list>
      <f7-list-item
        v-for="user in users"
        :key ="user.UUID"
        :title="user.DisplayName ? user.DisplayName : user.UID"
        :link="`${$f7route.url}${user.UUID}/`" />
    </f7-list>

  </f7-page>
</template>
<script>
import {orderBy} from 'lodash-es';
import Debug from 'debug';
const debug = Debug('page:users');

export default {
  computed: {
    users: function() {
      return orderBy(this.$store.getters.users, 'DisplayName');
    },
  },
  methods: {
    openEditorNew: function() {
      const editor = `${this.$f7route.url}NEW/editor/`;
      debug('opening editor ', editor);
      this.$f7router.navigate(editor);
    },

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
