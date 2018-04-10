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
    <f7-navbar :back-link="$t('message.general.back')" :title="dev.network_name + $t('message.dev_details._details')" sliding>
    </f7-navbar>

    <div v-if="dev.notification">
      <f7-block-title>{{ $t("message.notifications.security_notifications") }}</f7-block-title>
      <f7-block inner>
        <li>{{ $t("message.notifications.msg.0") }}</li>
        <li>{{ $t("message.notifications.msg.1") }}</li>
        <li>{{ $t("message.notifications.msg.2") }}</li>
      </f7-block>

    </div>

    <div v-if="dev.alert">
      <f7-block-title>{{ $t("message.alerts.important_alert") }}</f7-block-title>
      <f7-block inner>
        <li>{{ $t("message.alerts.msg.0") }}</li>
        <li>{{ $t("message.alerts.msg.1") }}</li>
        <li>{{ $t("message.alerts.msg.2") }}</li>
      </f7-block>
    </div>

    <f7-list>

      <f7-list-item v-if="dev.certainty !== 'low'"
          :title="$t('message.dev_details.device')"
          :footer="dev.manufacturer">
        <span>{{ dev.model }}
        <f7-icon v-if="dev.certainty === 'medium'" f7="help" /></span>
      </f7-list-item>
      <f7-list-item v-else
          :title="$t('message.dev_details.device')">
        {{ $t('message.dev_details.unknown_device') }}
      </f7-list-item>

      <f7-list-item :title="$t('message.dev_details.os_version')">{{ os_version }}</f7-list-item>
      <f7-list-item :title="$t('message.dev_details.network_name')">{{ dev.network_name }}</f7-list-item>
      <f7-list-item
        :title="$t('message.dev_details.owner')"
        :link="'/users/'">
        <span>
          {{ dev.owner }}
          <f7-link
            v-if="dev.owner_email"
            :href="`mailto:${dev.owner_email}`"
            icon-f7="email_fill"
            style="padding-left: 0.25em; padding-right: 0.25em;"
            external>
          </f7-link>
          <f7-link
            v-if="dev.owner_phone"
            :href="`tel:${dev.owner_phone}`"
            icon-f7="phone_fill"
            style="padding-left: 0.25em; padding-right: 0.25em;"
            external>
          </f7-link>
          <f7-link
            v-if="dev.owner_phone"
            :href="`sms:${dev.owner_phone}`"
            icon-f7="chat_fill"
            style="padding-left: 0.25em; padding-right: 0.25em;"
            external>
          </f7-link>
        </span>
      </f7-list-item>
    </f7-list>

    <f7-block-title>{{ $t("message.dev_details.access.access_control") }}</f7-block-title>
    <f7-list form>
      <f7-list-item>
      <f7-label>{{ $t("message.dev_details.access.security_ring") }}</f7-label>
      <span v-if="ring_changing" class="preloader"></span>
      <f7-input v-else type="select" :value="dev.ring" @input="changeRing($event.target.value)">
        <option v-for="ring in rings" v-bind:value="ring" v-bind:key="ring">{{ring}}</option>
      </f7-input>
      </f7-list-item>
    </f7-list>

    <f7-list>
      <f7-list-item v-if="dev.alert"
          :title="$t('message.dev_details.access.status')"
          :after="$t('message.dev_details.access.blocked')"
          :text="$t('message.dev_details.access.blocked_text')" />
      <f7-list-item v-else
          :title="$t('message.dev_details.access.status')"
          :after="$t('message.dev_details.access.normal')" />
    </f7-list>

  </f7-page>
</template>
<script>
import assert from 'assert';

export default {
  beforeCreate: function() {
    return this.$store.dispatch('fetchRings');
  },

  methods: {

    changeRing: function(wanted_ring) {
      assert(typeof wanted_ring === 'string');
      console.log(`Change Ring to ${wanted_ring}`);
      if (this.ring_changing) {
        return;
      }
      this.ring_changing = true;
      this.$store.dispatch('changeRing', {
        deviceUniqID: this.dev.uniqid,
        newRing: wanted_ring,
      }).then(() => {
        this.ring_changing = false;
      }).catch((err) => {
        const txt = `Failed to change security ring for ${this.dev.network_name} to ${wanted_ring}: ${err}`;
        this.ring_changing = false;
        this.$f7.toast.show({
          text: txt,
          closeButton: true,
          destroyOnClose: true,
        });
        this.$f7router.back();
      });
    },
  },

  computed: {
    os_version: function() {
      return this.dev.os_version ? this.dev.os_version : this.$t('message.dev_details.os_version_unknown');
    },
    dev: function() {
      const uniqid = this.$f7route.params.UniqID;
      return this.$store.getters.Device_By_UniqID(uniqid);
    },
    rings: function() {
      return this.$store.getters.Rings;
    },
  },
  data: function() {
    return {
      ring_changing: false,
    };
  },
};
</script>
