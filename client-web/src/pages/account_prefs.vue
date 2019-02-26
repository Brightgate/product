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
      <h1>{{ userInfo.name }}</h1>
      <div>{{ userInfo.organization }}</div>
      <div>{{ userInfo.email }}</div>
      <div>{{ userInfo.phoneNumber }}</div>
    </f7-block>

    <f7-list>
      <f7-list-item
        :title="$t('message.account_prefs.self_provision')"
        link="/account_prefs/self_provision" />
    </f7-list>

  </f7-page>
</template>

<script>
import vuex from 'vuex';

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
      'userInfo',
    ]),
  },

  methods: {
    onPageBeforeIn: function() {
      this.$store.dispatch('checkLogin').catch(() => {});
    },
  },
};
</script>
