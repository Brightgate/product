<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>
span.wifi {
  display: inline-block;
  background: #eeeeee;
  font-family: monospace;
  padding-left: 0.2em;
  padding-right: 0.2em;
}

</style>
<template>
  <f7-page name="enroll">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.enroll_guest.title')" sliding />

    <f7-block bg-color="white">
      <h2>{{ $t('message.enroll_guest.header') }}</h2>

      <table>
        <tr>
          <td valign="top">
            <f7-block>
              <f7-icon size="36" ios="f7:persons" md="material:people" />
            </f7-block>
          </td>
          <td width="400">
            <f7-block-title>{{ $t('message.enroll_guest.direct_subhead') }}</f7-block-title>
            <f7-block>
              <p>
                {{ $t('message.enroll_guest.network_name') }}:
                <span class="wifi">
                  <f7-icon material="wifi" size="16" /> {{ vaps['guest'].ssid }}
                </span>
              </p>
              <p>
                {{ $t('message.enroll_guest.network_passphrase') }}:
                <span class="wifi">
                  {{ vaps['guest'].passphrase }}
                </span>
              </p>
            </f7-block>
          </td>
        </tr>

        <tr>
          <td valign="top">
            <f7-block>
              <f7-icon size="36" ios="f7:message_fill" md="material:sms" />
            </f7-block>
          </td>
          <td>
            <f7-block-title>{{ $t('message.enroll_guest.sms_subhead') }}</f7-block-title>
            <f7-list no-hairlines>
              <f7-list-input
                :value="phoneInput"
                :placeholder="$t('message.enroll_guest.phone_placeholder')"
                :label="$t('message.enroll_guest.phone')"
                type="tel"
                required
                autofocus @input="onTelInput" />
              <f7-list-item>
                <f7-button
                  :disabled="!validForm"
                  fill
                  style="margin-left: auto"
                  @click="enrollGuest">
                  <span v-if="!enrolling">{{ $t('message.enroll_guest.send_sms') }}</span>
                  <span v-if="enrolling">
                    {{ $t('message.enroll_guest.sending') }}
                    <f7-preloader color="white" />
                  </span>
                </f7-button>
              </f7-list-item>
            </f7-list>
          </td>
        </tr>


        <!-- QR code will go here -->

      </table>
    </f7-block>
  </f7-page>

</template>
<script>

// n.b. our handling of phone number input only sort-of works.  Probably we
// should switch to cleave.js or some other framework.  If we roll our own,
// then phone number input should go into its own Vue component.

import Vuex from 'vuex';
import {isValidNumber, AsYouType} from 'libphonenumber-js';
import Debug from 'debug';
const debug = Debug('page:enroll-guest');
let phoneAYT = null;

export default {
  data: function() {
    return {
      phoneInput: '',
      emailInput: '',
      enrolling: false,
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...Vuex.mapGetters([
      'loggedIn',
      'vaps',
    ]),

    validForm: function() {
      if (!this.phoneInput || this.phoneInput === '') {
        return false;
      }
      return isValidNumber(this.phoneInput, 'US');
    },
  },

  methods: {
    onTelInput: function(event) {
      if (phoneAYT === null) {
        phoneAYT = new AsYouType('US');
      }
      phoneAYT.reset();
      this.phoneInput = phoneAYT.input(event.target.value);
    },

    enrollGuest: async function() {
      debug(`enrollGuest: ${this.phoneInput} ${this.email}`);
      this.enrolling = true;
      try {
        const resp = await this.$store.dispatch('enrollGuest', {kind: 'psk', phoneNumber: this.phoneInput});
        debug('enrollGuest response', resp);
        this.$f7.toast.show({
          text: this.$t('message.enroll_guest.psk_success'),
          closeButton: true,
          destroyOnClose: true,
          on: {
            close: () => {
              this.$f7router.back();
            },
          },
        });
      } catch (err) {
        debug('enrollGuestPSK: failed', err);
        this.$f7.toast.show({
          text: this.$t('message.enroll_guest.sms_failure'),
          closeButton: true,
          destroyOnClose: true,
        });
      } finally {
        this.enrolling = false;
      }
    },
  },
};
</script>
