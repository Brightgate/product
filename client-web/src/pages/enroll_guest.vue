<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page name="enroll">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.enroll_guest.title')" sliding />

    <center><h2>{{ $t('message.enroll_guest.header') }}</h2></center>
    <center><h4>{{ $t('message.enroll_guest.subheader') }}</h4></center>

    <div v-if="! $store.getters.Is_Logged_In">
      <p>{{ $t('message.general.need_login') }}</p>
    </div>
    <div v-else>
      <f7-list no-hairlines>
        <f7-list-item>
          <f7-label inline>Enroll to the: </f7-label>
          <f7-input
            :value="usertype"
            type="select"
            @change="usertype = $event.target.value">
            <option value="psk">{{ $t('message.enroll_guest.psk_network') }}</option>
            <option value="eap">{{ $t('message.enroll_guest.eap_network') }}</option>
          </f7-input>
        </f7-list-item>

        <f7-list-item>
          <f7-label>{{ $t('message.enroll_guest.phone') }}</f7-label>
          <f7-input
            :value="phone_input"
            :placeholder="$t('message.enroll_guest.phone_placeholder')"
            type="tel"
            required
            autofocus @input="onTelInput" />
        </f7-list-item>

        <f7-list-item v-if="usertype === 'eap'">
          <f7-label>{{ $t('message.enroll_guest.email') }}</f7-label>
          <f7-input
            :value="email_input"
            :placeholder="$t('message.enroll_guest.email_placeholder')"
            type="email"
            required
            @input="email_input = $event.target.value" />
        </f7-list-item>
      </f7-list>

      <f7-block inset>
        <f7-button :disabled="!valid_form" fill big @click="enrollGuest">
          <span v-if="!enrolling">{{ $t('message.enroll_guest.send_sms') }}</span>
          <span v-if="enrolling">
            {{ $t('message.enroll_guest.sending') }}
            <f7-preloader color="white" />
          </span>
        </f7-button>

      </f7-block>
    </div>
  </f7-page>

</template>
<script>

// n.b. our handling of phone number input only sort-of works.  Probably we
// should switch to cleave.js or some other framework.  If we roll our own,
// then phone number input should go into its own Vue component.

import {isValidNumber, AsYouType} from 'libphonenumber-js';
import emailvalidator from 'email-validator';
import Debug from 'debug';
const debug = Debug('page:enroll-guest');
let phone_ayt = null;

export default {
  data: function() {
    return {
      usertype: 'eap',
      phone_input: '',
      email_input: '',
      enrolling: false,
    };
  },

  computed: {
    valid_form: function() {
      if (this.usertype === 'eap') {
        if (!this.email_input || this.email_input === '') {
          return false;
        }
        if (emailvalidator.validate(this.email_input) === false) {
          return false;
        }
      }
      if (!this.phone_input || this.phone_input === '') {
        return false;
      }
      return isValidNumber(this.phone_input, 'US');
    },
  },

  methods: {
    onTelInput: function(event) {
      if (phone_ayt === null) {
        phone_ayt = new AsYouType('US');
      }
      phone_ayt.reset();
      this.phone_input = phone_ayt.input(event.target.value);
    },

    toastSuccess: function(text) {
      this.$f7.toast.show({
        text: text,
        closeButton: true,
        destroyOnClose: true,
        on: {
          close: () => {
            this.$f7router.back();
          },
        },
      });
    },

    enrollGuestEAP: function() {
      return this.$store.dispatch('enrollGuest',
        {type: 'eap', phone: this.phone_input, email: this.email_input}
      ).then((res) => {
        debug('enrollGuestEAP result', res);
        const user = res.user;
        this.toastSuccess(
          this.$t('message.enroll_guest.eap_success', {name: user.UID}));
      });
    },

    enrollGuestPSK: function() {
      return this.$store.dispatch('enrollGuest',
        {type: 'psk', phone: this.phone_input}
      ).then((res) => {
        debug('enrollGuestEAP result', res);
        this.toastSuccess(this.$t('message.enroll_guest.psk_success'));
      });
    },

    enrollGuest: function() {
      let p; // promise for guest enrollment
      debug(`enrollGuest: ${this.usertype} ${this.phone_input} ${this.email}`);
      this.enrolling = true;
      if (this.usertype === 'eap') {
        p = this.enrollGuestEAP();
      } else {
        p = this.enrollGuestPSK();
      }
      return p.finally(() => {
        this.enrolling = false;
      }).catch((err) => {
        debug('enrollGuest: failed', err);
        this.$f7.toast.show({
          text: this.$t('message.enroll_guest.sms_failure'),
          closeButton: true,
          destroyOnClose: true,
        });
      });
    },
  },
};
</script>
