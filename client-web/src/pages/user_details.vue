<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page>
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.user_details.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-fab v-if="user.SelfProvisioning === false" color="pink" @click="openEditor">
      <f7-icon f7="compose_fill" />
    </f7-fab>

    <f7-block>
      <h1>
        {{ user.DisplayName }}
      </h1>

    </f7-block>

    <f7-list>
      <!-- User Name -->
      <f7-list-item :title="$t('message.user_details.user_name')">
        {{ user.UID }}
      </f7-list-item>
      <f7-list-item :title="$t('message.user_details.user_type')">
        <f7-link v-if="user.SelfProvisioning"
                 popover-open=".popover-user-type"
                 icon-ios="f7:cloud" icon-md="material:cloud">
          &nbsp;Cloud User
        </f7-link>
        <f7-link v-else
                 popover-open=".popover-user-type"
                 icon-ios="material:router" icon-md="material:router">
          &nbsp;Site-Specific Administrator
        </f7-link>
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

    </f7-list>
    <f7-popover class="popover-user-type">
      <template v-if="user.SelfProvisioning">
        <f7-block-title>
          <f7-icon ios="f7:cloud" md="material:cloud" /> Cloud User
        </f7-block-title>
        <f7-block v-if="user.SelfProvisioning">
          This user was created using the Brightgate portal's Wi-Fi
          self-provisioning wizard and cannot be modified from this page.
        </f7-block>
      </template>
      <template v-else>
        <f7-block-title>
          <f7-icon material="router" color="gray" /> Site-Specific Administrator
        </f7-block-title>
        <f7-block>
          This user is a site administrator. This user has site-specific credentials and may log in only to this site's local web interface and Wi-Fi network.
        </f7-block>
      </template>
    </f7-popover>

  </f7-page>
</template>
<script>
import Debug from 'debug';

import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';

const debug = Debug('page:user-details');

export default {
  components: {
    'bg-site-breadcrumb': BGSiteBreadcrumb,
  },

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
