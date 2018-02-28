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
          <f7-input
                v-on:keyup.enter="enrollSMS"
                v-model="magicphone"
                type="tel"
                :placeholder="$t('message.enroll.phone_placeholder')"
                required autofocus lazy/>
        </f7-list-item>
      </f7-list>
      <f7-block inset>
        <f7-button fill big v-bind:color="valid_number ? 'green' : 'blue'" @click="enrollSMS">
          <span v-if="!enrolling">{{ $t('message.enroll.send_sms') }}</span>
          <span v-if="enrolling">
            {{ $t('message.enroll.sending') }}
          <span class="preloader"></span>
          </span>
        </f7-button>

        <f7-block v-if="sms_sent">
          <p>{{ $t('message.enroll.send_success') }}</p>
          <f7-button fill back>{{ $t('message.general.close') }}</f7-button>
        </f7-block>
        <f7-block v-if="sms_error">
          <p>{{ $t('message.enroll.send_failure') }}</p>
        </f7-block>
      </f7-block>
    </div>
  </f7-page>

</template>

<script>

import Promise from 'bluebird'
import libphonenumber from 'libphonenumber-js'
const phone_ayt = new libphonenumber.AsYouType("US")

export default {
  methods: {
    enrollSMS: function () {
      console.log(`enrollSMS: ${this.phone}`)
      this.sms_error = false
      this.enrolling = true
      return this.$store.dispatch('enrollSMS', {
        phone: this.phone
      }).then(() => {
        console.log("enrollSMS: success")
        this.sms_sent = true
        this.enrolling = false
      }).catch(() => {
        console.log("enrollSMS: failed")
        this.sms_error = true
        this.enrolling = false
      })
    },
  },

  // n.b. this is arcane and only sort-of works.  Probably we should switch to
  // cleave.js or some other framework.  If we roll our own, then phone number
  // input should go into its own Vue component.
  computed: {
    magicphone: {
      set: function(inputEvt) {
	this.phone = inputEvt.target.value;
	phone_ayt.reset()
	this.formattedPhone = phone_ayt.input(this.phone)
	this.$emit('input', this.formattedPhone)
      },
      get: function() {
        return this.formattedPhone
      }
    },

    valid_number: function() {
      var phonestr = this.phone ? this.phone : ""
      var res = libphonenumber.isValidNumber(phonestr)
      return libphonenumber.isValidNumber(phonestr, "US")
    }
  },

  data: function() {
    return {
      sms_sent: false,
      sms_error: false,
      phone: "",
      formattedPhone: "",
      enrolling: false,
    }
  }
}
</script>
