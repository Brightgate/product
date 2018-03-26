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
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.enroll.title')" sliding>
    </f7-navbar>

    <center><h2>{{ $t('message.enroll.header') }}</h2></center>
    <center><h4>{{ $t('message.enroll.subheader') }}</h4></center>

    <div v-if="! $store.getters.Is_Logged_In">
      <p>{{ $t('message.general.need_login') }}</p>
    </div>
    <div v-else>
      <f7-list inset>
        <f7-list-item>
          <f7-label>{{ $t('message.enroll.phone') }}</f7-label>
          <f7-input type="tel"
                :value="this.phone_input"
                @input="onTelInput"
                :placeholder="$t('message.enroll.phone_placeholder')"
                required autofocus lazy>
          </f7-input>
        </f7-list-item>
      </f7-list>
      <f7-block inset>
        <f7-button fill big v-bind:color="valid_number ? 'green' : 'blue'" :active="valid_number" @click="enrollSMS">
          <span v-if="!enrolling">{{ $t('message.enroll.send_sms') }}</span>
          <span v-if="enrolling">
            {{ $t('message.enroll.sending') }}
          <span class="preloader"></span>
          </span>
        </f7-button>

      </f7-block>
    </div>
  </f7-page>

</template>

<script>

import Promise from 'bluebird'
import libphonenumber from 'libphonenumber-js'
const phone_ayt = new libphonenumber.AsYouType("US")

export default {
  data: function() {
    return {
      phone_input: "",
      enrolling: false,
    }
  },

  // n.b. this is arcane and only sort-of works.  Probably we should switch to
  // cleave.js or some other framework.  If we roll our own, then phone number
  // input should go into its own Vue component.
  computed: {
    valid_number: function() {
      if (!this.phone_input || this.phone_input === "") {
        return false
      }
      return libphonenumber.isValidNumber(this.phone_input, "US")
    },
  },

  methods: {
    onTelInput: function (event) {
      phone_ayt.reset();
      this.phone_input = phone_ayt.input(event.target.value);
    },

    enrollSMS: function () {
      console.log(`enrollSMS: ${this.phone_input}`)
      this.enrolling = true
      return this.$store.dispatch('enrollSMS', {
        phone: this.phone_input
      }).then(() => {
        console.log("enrollSMS: success")
        const self = this;
        this.enrolling = false
	this.$f7.toast.show({
          text: this.$t('message.enroll.send_success'),
          closeButton: true,
          destroyOnClose: true,
          on: {
            close: function () {
              console.log("toast onclose handler")
              self.$f7router.back()
            }
          }
        })
      }).catch(() => {
        console.log("enrollSMS: failed")
        this.enrolling = false
	this.$f7.toast.show({
          text: this.$t('message.enroll.send_failure'),
          closeButton: true,
          destroyOnClose: true,
        })
      })
    },
  },
}
</script>
