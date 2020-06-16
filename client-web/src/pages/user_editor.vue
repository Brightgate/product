<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page @page:afterin="onPageAfterIn">
    <f7-navbar
      :back-link="$t('message.general.back')"
      :title="newUser ? $t('message.user_details.add_title') : $t('message.user_details.edit_title')"
      sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-list>
      <!-- uid -- user name -->
      <f7-list-input
        v-if="newUser"
        :label="$t('message.user_details.user_name')"
        :value="user.UID"
        type="email"
        placeholder="User's short ID"
        required
        clear-button
        @input="user.UID = $event.target.value" />
      <f7-list-item
        v-else
        :title="$t('message.user_details.user_name')">
        {{ user.UID }}
      </f7-list-item>

      <!-- display name -->
      <f7-list-input
        :label="$t('message.user_details.name')"
        :value="user.DisplayName"
        :placeholder="$t('message.user_details.name_placeholder')"
        type="text"
        clear-button
        @input="user.DisplayName = $event.target.value" />

      <!-- email -->
      <f7-list-input
        :label="$t('message.user_details.email')"
        :value="user.Email"
        :placeholder="$t('message.user_details.email_placeholder')"
        type="email"
        clear-button
        @input="user.Email = $event.target.value" />

      <!-- phone & sms -->
      <f7-list-input
        :label="$t('message.user_details.phone')"
        :value="user.TelephoneNumber"
        :placeholder="$t('message.user_details.phone_placeholder')"
        type="tel"
        clear-button
        @input="user.TelephoneNumber = $event.target.value" />

      <!-- Role -->
      <f7-list-input
        :label="$t('message.user_details.role')"
        type="select">
        <option value="admin">Administrator</option>
      </f7-list-input>

      <!-- Password -->
      <f7-list-input
        :label="$t('message.user_details.password')"
        :value="user.SetPassword"
        type="password"
        placeholder="User Password"
        clear-button
        @input="user.SetPassword = $event.target.value" />

    </f7-list>

    <!-- Controls: Create/Save, Cancel, Delete -->
    <f7-block>
      <f7-row>
        <f7-col v-if="!newUser">
          <f7-button color="red" outline @click="deleteUser">Delete</f7-button>
        </f7-col>
        <f7-col>
          <f7-button outline back>Cancel</f7-button>
        </f7-col>
        <f7-col>
          <f7-button v-if="newUser" fill raised @click="saveUser">Create</f7-button>
          <f7-button v-else fill raised @click="saveUser">Save</f7-button>
        </f7-col>
      </f7-row>
    </f7-block>

  </f7-page>
</template>
<script>
import {cloneDeep} from 'lodash-es';
import Debug from 'debug';
import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';

const debug = Debug('page:user_editor');

export default {
  components: {
    'bg-site-breadcrumb': BGSiteBreadcrumb,
  },

  data: function() {
    const routeUUID = this.$f7route.params.UUID;
    const d = {
      newUser: (routeUUID === 'NEW'),
    };
    if (routeUUID === 'NEW') {
      d.user = {
        UID: '',
        DisplayName: '',
        Email: '',
        TelephoneNumber: '',
        SetPassword: null,
      };
    } else {
      debug('cloning', routeUUID);
      d.user = cloneDeep(this.$store.getters.userByUUID(routeUUID));
    }
    return d;
  },

  methods: {
    saveUser: function(event) { // eslint-disable-line no-unused-vars
      debug('saveUser');
      return this.$store.dispatch('saveUser', {
        user: this.user,
        newUser: this.newUser,
      }).then(() => {
        const txt = this.newUser ?
          this.$t('message.user_details.create_ok', {name: this.user.UID}) :
          this.$t('message.user_details.save_ok', {name: this.user.UID});
        this.$f7.toast.show({
          text: txt,
          closeTimeout: 2000,
          destroyOnClose: true,
        });
        this.$f7router.back();
      }).catch((err) => {
        debug('saveUser: Error', err);
        const txt = this.newUser ?
          this.$t('message.user_details.create_fail', {err: err}) :
          this.$t('message.user_details.save_fail', {err: err});
        this.$f7.toast.show({
          text: txt,
          closeButton: true,
          destroyOnClose: true,
        });
      });
    },

    deleteUser: function(event) { // eslint-disable-line no-unused-vars
      debug('deleteUser');
      return this.$store.dispatch('deleteUser', {
        user: this.user,
      }).then(() => {
        const txt = this.$t('message.user_details.delete_ok', {name: this.user.UID});
        this.$f7.toast.show({
          text: txt,
          closeTimeout: 5000,
          closeButton: true,
          destroyOnClose: true,
        });
        this.$f7router.back('/users/', {force: true});
      }).catch((err) => {
        const txt = this.$t('message.user_details.delete_fail', {err: err});
        this.$f7.toast.show({
          text: txt,
          closeButton: true,
          destroyOnClose: true,
        });
      });
    },

    onPageAfterIn: function() {
      debug(`user_editor page for '${this.$f7route.params.UUID}'`);
    },
  },
};
</script>
