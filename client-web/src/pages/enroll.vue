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
    <center><h2>Getting Online with Brightgate</h2></center>
    <center><h4>Sign up using your phone number</h4></center>

    <f7-list inset>
      <f7-list-item>
        <f7-label>Phone</f7-label>
        <f7-input v-on:keyup.enter="enrollSMS" v-model="magicphone" type="tel" placeholder="Your Phone #" required autofocus lazy/>
      </f7-list-item>
    </f7-list>
    <f7-block inset>
      <p>
      Give us your phone number, and we'll text you the password to access this network.
      </p>
      <f7-button fill big v-bind:color="valid_number ? 'green' : 'blue'" @click="enrollSMS">
        <span v-if="!enrolling">Text Me</span>
        <span v-if="enrolling">Sending <span class="preloader"></span></span>
      </f7-button>
    </f7-block>

    <f7-block v-if="sms_sent">
    <p>Great!  You should receive an SMS momentarily with the network
    name and password.</p>
    </f7-block>
    <f7-block v-if="sms_error">
    <p>Oops, something went wrong sending your SMS message.</p>
    </f7-block>

  </f7-page>

</template>


<script>

import Promise from 'bluebird'
import libphonenumber from 'libphonenumber-js'
const phone_ayt = new libphonenumber.asYouType("US")

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

  computed: {
    magicphone: {
      set: function(newValue) {
        const ph = libphonenumber.parse(newValue, "US")
        phone_ayt.reset()
        this.phone = phone_ayt.input(newValue)
      },
      get: function() {
        return this.phone
      }
    },

    valid_number: function() {
      const ph = libphonenumber.parse(this.phone, "US")
      console.log("ph is", ph)
      return libphonenumber.isValidNumber(ph)
    }
  },

  data: function() {
    return {
      sms_sent: false,
      sms_error: false,
      phone: "",
      enrolling: false,
    }
  }
}
</script>
