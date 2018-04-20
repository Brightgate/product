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
    <f7-navbar
        :back-link="$t('message.general.back')"
        :title="$t('message.user_details.edit_title')"
        sliding>
    </f7-navbar>

    <f7-list>
      <!-- uid -- user name -->
      <f7-list-item v-if="new_user">
        <f7-label>{{$t('message.user_details.username')}}</f7-label>
        <f7-input type="email"
            :value="user_details.UID"
            @input="user_details.UID = $event.target.value"
            placeholder="User's short ID"
            required
            clear-button>
        </f7-input>
      </f7-list-item>
      <f7-list-item v-else
          :title="$t('message.user_details.username')">
        {{ user_details.UID }}
      </f7-list-item>

      <!-- display name -->
      <f7-list-item>
        <f7-label>Name</f7-label>
        <f7-input type="text"
            :value="user_details.DisplayName"
            @input="user_details.DisplayName = $event.target.value"
            placeholder="User's full name"
            clear-button>
        </f7-input>
      </f7-list-item>

      <!-- email -->
      <f7-list-item>
        <f7-label>Email</f7-label>
        <f7-input type="email"
            :value="user_details.Email"
            @input="user_details.Email = $event.target.value"
            placeholder="User Email"
            clear-button>
        </f7-input>
      </f7-list-item>

      <!-- phone & sms -->
      <f7-list-item>
        <f7-label>Phone</f7-label>
        <f7-input type="tel"
            :value="user_details.TelephoneNumber"
            @input="user_details.TelephoneNumber = $event.target.value"
            placeholder="User Mobile Phone"
            clear-button>
        </f7-input>
      </f7-list-item>

      <!-- Role -->
      <f7-list-item>
        <f7-label>{{$t('message.user_details.role')}}</f7-label>
        <f7-input type="select" value="admin">
          <!-- XXX <option value="user">User</option> -->
          <option value="admin">Administrator</option>
        </f7-input>
      </f7-list-item>

      <!-- Password -->
      <f7-list-item>
        <f7-label>{{$t('message.user_details.password')}}</f7-label>
        <f7-input type="password"
            @input="user_details.SetPassword = $event.target.value"
            placeholder="User Password"
            clear-button>
        </f7-input>
      </f7-list-item>

      <!-- 2-factor, disabled for now
      <f7-list-item :title="$t('message.user_details.twofactor')">
        <f7-link v-if="user_details.HasTOTP" :href="$f7route.url + 'twofactor/'">Enabled</f7-link>
        <f7-link v-else :href="$f7route.url + 'twofactor/'">Disabled</f7-link>
      </f7-list-item>
      -->
    </f7-list>

    <!-- Controls: Create/Save, Cancel, Delete -->
    <f7-block>
      <f7-row>
        <f7-col>
          <f7-button v-if="new_user" @click="saveUser" fill>Create</f7-button>
          <f7-button v-else @click="saveUser" fill>Save</f7-button>
        </f7-col>
        <f7-col>
          <f7-button outline back>Cancel</f7-button>
        </f7-col>
        <f7-col v-if="!new_user">
          <f7-button color="red" @click="deleteUser" outline>Delete</f7-button>
        </f7-col>
      </f7-row>
    </f7-block>

  </f7-page>
</template>
<script>
import _ from 'lodash';

export default {
  data: function() {
    const routeUUID = this.$f7route.params.UUID;
    const d = {
      new_user: (routeUUID === 'NEW'),
    };
    if (routeUUID === 'NEW') {
      d.user_details = {
        UID: '',
        DisplayName: '',
        Email: '',
        TelephoneNumber: '',
        HasTOTP: false,
      };
    } else {
      d.user_details = _.cloneDeep(this.$store.getters.User_By_UUID(routeUUID));
    }
    return d;
  },

  methods: {
    saveUser: function(event) { // eslint-disable-line no-unused-vars
      return this.$store.dispatch('saveUser', {
        user: this.user_details,
        newUser: this.new_user,
      }).then(() => {
        const txt = this.new_user
          ? this.$t('message.user_details.create_ok', {name: this.user_details.UID})
          : this.$t('message.user_details.save_ok', {name: this.user_details.UID});
        this.$f7.toast.show({
          text: txt,
          closeTimeout: 2000,
          destroyOnClose: true,
        });
        this.$f7router.back();
      }).catch((err) => {
        const txt = this.new_user
          ? this.$t('message.user_details.create_fail', {err: err})
          : this.$t('message.user_details.save_fail', {err: err});
        this.$f7.toast.show({
          text: txt,
          closeButton: true,
          destroyOnClose: true,
        });
      });
    },

    deleteUser: function(event) { // eslint-disable-line no-unused-vars
      return this.$store.dispatch('deleteUser', {
        user: this.user_details,
      }).then(() => {
        const txt = this.$t('message.user_details.delete_ok', {name: this.user_details.UID});
        this.$f7.toast.show({
          text: txt,
          closeTimeout: 5000,
          closeButton: true,
          destroyOnClose: true,
        });
        this.$f7router.navigate('/users/');
      }).catch((err) => {
        const txt = this.$t('message.user_details.delete_fail', {err: err});
        this.$f7.toast.show({
          text: txt,
          closeButton: true,
          destroyOnClose: true,
        });
      });
    },
  },

  on: {
    pageAfterIn: function() {
      console.log(`user_editor page for '${this.$f7route.params.UUID}'`);
    },
  },
};
</script>
