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
      ptr @ptr:refresh="usersPullRefresh"
      @page:afterin="onPageAfterIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.users.title')" sliding>
    </f7-navbar>

    <!-- n.b. FAB must be direct child of a page -->
    <f7-fab color="pink" @click="openEditorNew" href="false">
      <f7-icon f7="add"></f7-icon>
    </f7-fab>

    <f7-list>
      <f7-list-item v-for="user in All_Users"
            v-bind:key ="user.UUID"
            v-bind:title="user.DisplayName"
            v-bind:link="`/users/${user.UUID}/`">
      </f7-list-item>
    </f7-list>

  </f7-page>
</template>
<script>
import Promise from 'bluebird';
import _ from 'lodash';

export default {
  computed: {
    All_Users: function() {
      return _.orderBy(this.$store.getters.All_Users, 'DisplayName');
    },
  },
  methods: {
    openEditorNew: function() {
      const editor = `${this.$f7route.url}NEW/editor/`;
      console.log('opening editor ', editor);
      this.$f7router.navigate(editor);
    },
    usersPullRefresh: function(event, done) {
      return Promise.resolve(this.$store.dispatch('fetchUsers')).asCallback(done);
    },

    onPageAfterIn: function() {
      console.log('users.vue on pageAfterIn');
      return this.$store.dispatch('fetchUsers');
    },
  },
};
</script>
