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
    <f7-navbar>
      <!-- f7-nav-title doesn't seem to center properly without also
           including left and right. -->
      <f7-nav-left>&nbsp;</f7-nav-left>
      <f7-nav-title><img src="img/bglogo.png"/></f7-nav-title>
      <f7-nav-right>&nbsp;</f7-nav-right>
    </f7-navbar>

    <br />
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
