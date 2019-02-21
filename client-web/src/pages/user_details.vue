<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page>
    <f7-navbar :back-link="$t('message.general.back')" :title="user.DisplayName" sliding />

    <f7-fab v-if="user.SelfProvisioning === false" color="pink" @click="openEditor">
      <f7-icon f7="compose_fill" />
    </f7-fab>

    <f7-card v-if="user.SelfProvisioning">
      <f7-card-header>
        <span><f7-icon ios="f7:cloud" md="material:cloud" color="gray" /> Cloud Self-Provisioned User</span>
      </f7-card-header>
      <f7-card-content>This user was created using Cloud Self Provisioning via the Brightgate
      cloud portal.  The user cannot be administered from this screen.
        <!-- XXX there's really no-where that the user can be administered at present. -->
      </f7-card-content>
    </f7-card>
    <f7-card v-else>
      <f7-card-header>
        <span><f7-icon material="router" color="gray" /> Site-Local User</span>
      </f7-card-header>
      <f7-card-content>This user was created locally to this Site, and has
      privileges to login to the Site's local web interface; this user is
      backed-up to to the Brightgate cloud, and can be edited there.  Because
      the user is site-local, it is not synchronized to other sites in your
      organization.
        <!-- XXX in the future we could say more about roles -->
      </f7-card-content>
    </f7-card>


    <f7-list>
      <!-- Username -->
      <f7-list-item :title="$t('message.user_details.username')">
        {{ user.UID }}
      </f7-list-item>

      <!-- Email -->
      <f7-list-item v-if="user.Email">
        <div slot="media"><f7-icon f7="email_fill" color="blue" /></div>
        <span>
          <f7-link :href="`mailto: ${user.Email}`" external>{{ user.Email }}</f7-link>
        </span>
      </f7-list-item>
      <f7-list-item v-else>
        <div slot="media"><f7-icon f7="email_fill" color="grey" /></div>
        None
      </f7-list-item>

      <!-- Phone & SMS -->
      <f7-list-item v-if="user.TelephoneNumber">
        <div slot="media"><f7-icon f7="phone_fill" color="blue" /></div>
        <div slot="title">
          <f7-link :href="`tel: ${user.TelephoneNumber}`" external>{{ user.TelephoneNumber }}</f7-link>
        </div>
        <div slot="after">
          <f7-link :href="`sms: ${user.TelephoneNumber}`" external>
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

      <!-- Role -- Disabled for now
      <f7-list-item :title="$t('message.user_details.role')">
        {{ $t('message.user_details.roles.admin') }}
      </f7-list-item>
      -->

      <!-- 2-factor -- Disabled for now
      <f7-list-item :title="$t('message.user_details.twofactor')">
        <f7-link v-if="user.HasTOTP" :href="`${$f7route.url}twofactor/`">Enabled</f7-link>
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
    user: function() {
      return this.$store.getters.userByUUID(this.$f7route.params.UUID);
    },
  },

  methods: {
    openEditor: function() {
      debug('openEditor; current route', this.$f7route);
      const editor = `${this.$f7route.url}editor/`;
      debug('openEditor; navigate to', editor);
      this.$f7router.navigate(editor);
    },
  },
};
</script>
