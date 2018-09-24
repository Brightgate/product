<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page @page:afterin="onPageAfterIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="user_details.DisplayName" sliding />

    <f7-fab color="pink" @click="openEditor">
      <f7-icon f7="compose_fill" />
    </f7-fab>

    <f7-list>
      <!-- Username -->
      <f7-list-item :title="$t('message.user_details.username')">
        {{ user_details.UID }}
      </f7-list-item>

      <!-- Email -->
      <f7-list-item v-if="user_details.Email">
        <div slot="media"><f7-icon f7="email_fill" color="blue" /></div>
        <span>
          <f7-link :href="`mailto: ${user_details.Email}`" external>{{ user_details.Email }}</f7-link>
        </span>
      </f7-list-item>
      <f7-list-item v-else>
        <div slot="media"><f7-icon f7="email_fill" color="grey" /></div>
        None
      </f7-list-item>

      <!-- Phone & SMS -->
      <f7-list-item v-if="user_details.TelephoneNumber">
        <div slot="media"><f7-icon f7="phone_fill" color="blue" /></div>
        <div slot="title">
          <f7-link :href="`tel: ${user_details.TelephoneNumber}`" external>{{ user_details.TelephoneNumber }}</f7-link>
        </div>
        <div slot="after">
          <f7-link :href="`sms: ${user_details.TelephoneNumber}`" external>
            <f7-icon f7="chat_fill" color="blue" />
          </f7-link>
        </div>
      </f7-list-item>
      <f7-list-item v-else>
        <div slot="media"><f7-icon f7="phone_fill" color="grey" /></div>
        <div slot="title">
          None
        </div>
      </f7-list-item>

      <!-- Role -->
      <f7-list-item :title="$t('message.user_details.role')">
        {{ $t('message.user_details.roles.admin') }}
      </f7-list-item>

      <!-- 2-factor -- Disabled for now
      <f7-list-item :title="$t('message.user_details.twofactor')">
        <f7-link v-if="user_details.HasTOTP" :href="`${$f7route.url}twofactor/`">Enabled</f7-link>
        <f7-link v-else :href="`${$f7route.url}twofactor/`">Disabled</f7-link>
      </f7-list-item>
      -->

    </f7-list>

  </f7-page>
</template>
<script>
import Debug from 'debug';
const debug = Debug('page:user-details');

export default {
  computed: {
    user_details: function() {
      return this.$store.getters.User_By_UUID(this.$f7route.params.UUID);
    },
  },

  methods: {
    openEditor: function() {
      const editor = `${this.$f7route.url}editor/`;
      debug('opening editor ', editor);
      this.$f7router.navigate(editor);
    },

    onPageAfterIn: function() {
      debug(`user_details page for ${this.$f7route.params.UUID}`);
    },
  },
};
</script>
