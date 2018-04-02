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
    <f7-navbar :back-link="$t('message.general.back')" :title="device_details.network_name + $t('message.dev_details.details._details')" sliding>
    </f7-navbar>

    <div v-if="device_details.notification">
      <f7-block-title>{{ $t("message.notifications.security_notifications") }}</f7-block-title>
      <f7-block inner>
        <li>{{ $t("message.notifications.msg.0") }}</li>
        <li>{{ $t("message.notifications.msg.1") }}</li>
        <li>{{ $t("message.notifications.msg.2") }}</li>
      </f7-block>

    </div>

    <div v-if="device_details.alert">
      <f7-block-title>{{ $t("message.alerts.important_alert") }}</f7-block-title>
      <f7-block inner>
        <li>{{ $t("message.alerts.msg.0") }}</li>
        <li>{{ $t("message.alerts.msg.1") }}</li>
        <li>{{ $t("message.alerts.msg.2") }}</li>
      </f7-block>
    </div>

    <f7-block-title>{{ $t("message.dev_details.details.device_details") }}</f7-block-title>
    <f7-list inner>
      <f7-list-item :title="$t('message.dev_details.details.device')">{{ device_details.device }}</f7-list-item>
      <f7-list-item :title="$t('message.dev_details.details.network_name')">{{ device_details.network_name }}</f7-list-item>
      <f7-list-item :title="$t('message.dev_details.details.owner')">
        <span>
          {{ device_details.owner }} |
          <f7-link v-bind:href="'mailto:' + device_details.owner_email" external>📧</f7-link>
          &nbsp;
          <f7-link v-bind:href="'tel:' + device_details.owner_phone" external>📞</f7-link>
          &nbsp;
          <f7-link v-bind:href="'sms:' + device_details.owner_phone" external>💬</f7-link>
        </span>
      </f7-list-item>
    </f7-list>

    <f7-block-title>{{ $t("message.dev_details.access.access_control") }}</f7-block-title>
    <f7-list form>
      <f7-list-item>
      <f7-label>{{ $t("message.dev_details.access.security_ring") }}</f7-label>
      <span v-if="ring_changing" class="preloader"></span>
      <f7-input v-else type="select" :value="device_details.ring" @input="changeRing($event.target.value)">
        <option v-for="ring in $store.getters.Rings" v-bind:value="ring" v-bind:key="ring">{{ring}}</option>
      </f7-input>
      </f7-list-item>
    </f7-list>

    <f7-list media-list>
      <f7-list-item v-if="device_details.alert" :title="$t('message.dev_details.access.status')" :after="$t('message.dev_details.access.blocked')" :text="$t('message.dev_details.access.blocked_text')" />
      <f7-list-item v-else :title="$t('message.dev_details.access.status')" :after="$t('message.dev_details.access.normal')" />

    </f7-list>

    <f7-block>
      <p>
        {{ $t('message.dev_details.access.guest_access.time', {'time': render_time(expiration)}) }} <br/><br/>
      </p>
      <f7-row>
        <f7-col>
          <f7-button big outline color="green" v-on:click="expiration=(expiration+60)">{{ $t('message.dev_details.access.guest_access.extend') }}</f7-button>
        </f7-col>
        <f7-col>
          <f7-button v-if="!paused" big outline color="orange" v-on:click="paused=true">{{ $t('message.dev_details.access.guest_access.pause') }}</f7-button>
          <f7-button v-if="paused" big fill color="orange" v-on:click="paused=false">{{ $t('message.dev_details.access.guest_access.unpause') }}</f7-button>
        </f7-col>
        <f7-col>
          <f7-button big outline popover-open="#confirm-remove" color="red">{{ $t('message.dev_details.access.guest_access.remove') }}</f7-button>
        </f7-col>
      </f7-row>
    </f7-block>

    <f7-block-title>{{ $t("message.dev_details.activity.activity") }}</f7-block-title>
    <f7-list inner>
      <f7-list-item
          v-for="log_day in log_details"
          v-bind:key="log_day.log_id"
          v-bind:title="log_day.day">
        <span>{{ render_time(log_day.time) }} &mdash;
          <f7-link v-bind:data-popover="'#logs' + log_day.log_id" popover-open>{{ $t("message.general.details") }}</f7-link>
        </span>
      </f7-list-item>
      <f7-list-item>
        <f7-link popover-open="#limited-logs">{{ $t("message.dev_details.activity.older") }}</f7-link>
      </f7-list-item>
    </f7-list>

    <f7-block-title>{{ $t("message.dev_details.add_info.additional_information") }}</f7-block-title>
    <f7-list inner>
      <f7-list-item :title="$t('message.dev_details.details.network_name')">{{ device_details.network_name }}</f7-list-item>
      <f7-list-item :title="$t('message.dev_details.details.os_version')">{{ device_details.os_version }}</f7-list-item>
      <f7-list-item :title="$t('message.dev_details.details.activated')">{{ device_details.activated }}</f7-list-item>
      <f7-list-item :title="$t('message.dev_details.details.owner')">{{ device_details.owner }}</f7-list-item>
      <f7-list-item :title="$t('message.dev_details.details.owner_email')">
          <f7-link v-bind:href="'mailto:' + device_details.owner_email" external>{{ device_details.owner_email }}</f7-link>
      </f7-list-item>
      <f7-list-item :title="$t('message.dev_details.details.owner_phone')">
          <f7-link v-bind:href="'tel:' + device_details.owner_phone" external>{{ device_details.owner_phone }}</f7-link>
      </f7-list-item>
    </f7-list>

    <f7-popover
        v-for="log_day in log_details"
        v-bind:key="log_day.log_id"
        v-bind:id="'logs' + log_day.log_id">
      <f7-block>
        <ul>
          <div
              v-for="entry in log_day.entries"
              v-bind:key="entry.time">
            <li>{{ entry.name }} ({{render_time(entry.time)}})</li>
          </div>
        </ul>
      </f7-block>
    </f7-popover>

    <f7-popover id="limited-logs">
      <f7-block>
        {{ $t("message.dev_details.activity.not_supported") }}
      </f7-block>
    </f7-popover>

    <f7-popover id="confirm-remove">
      <f7-block>
        <p>
        {{ $t('message.dev_details.access.guest_access.confirm_remove', {'device': device_details.network_name}) }}
        </p>
        <f7-row>
          <f7-col width=20>&nbsp;</f7-col>
          <f7-col width=40>
            <f7-button big color="gray" popover-close>{{ $t('message.general.cancel') }}</f7-button>
          </f7-col>
          <f7-col width=40>
            <f7-button big fill popover-close color="red">{{ $t('message.general.confirm') }}</f7-button>
          </f7-col>
        </f7-row>
      </f7-block>
    </f7-popover>

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
      this.ring_changing = true;
      this.$store.dispatch('changeRing', {
        deviceUniqID: this.device_details.uniqid,
        newRing: wanted_ring,
      }).then(() => {
        this.ring_changing = false;
      }).catch((err) => {
        this.ring_changing = false;
        alert(`Failed to change security ring for ${this.device_details.network_name} to ${wanted_ring}: ${err}`);
      });
    },
  },

  computed: {
    device_details: function() {
      const uniqid = this.$f7route.params.UniqID;
      return this.$store.getters.Device_By_UniqID(uniqid);
    },
    rings: function() {
      return this.$store.getters.Rings;
    },
  },
  data: function() {
    return {
      render_time: function(mins) {
        const days = Math.floor(mins / 1440);
        const hours = Math.floor((mins % 1440) / 60);
        const rest = Math.floor(mins % 60);
        let result = '';
        if (days > 0) {
          result += ` ${days} d`;
        }
        if (hours > 0) {
          result += ` ${hours} h`;
        }
        if (rest > 0) {
          result += ` ${rest} m`;
        }
        return result;
      },
      ring_changing: false,
      paused: false,
      expiration: 314,
      log_details: [
        {log_id: '0', day: this.$t('message.dev_details.activity.dates.today'), time: 71,
          entries: [
            {time: 41, name: 'League of Legends'},
            {time: 20, name: 'Gmail'},
            {time: 10, name: 'Facebook'},
          ],
        },
        {log_id: '1', day: this.$t('message.dev_details.activity.dates.yesterday'), time: 162,
          entries: [
            {time: 162, name: 'League of Legends'},
          ],
        },
        {log_id: '2', day: this.$t('message.dev_details.activity.dates.sunday'), time: 211,
          entries: [
            {time: 181, name: 'League of Legends'},
            {time: 20, name: 'Gmail'},
            {time: 10, name: 'Facebook'},
          ],
        },
        {log_id: '3', day: this.$t('message.dev_details.activity.dates.saturday'), time: 424,
          entries: [
            {time: 361, name: 'League of Legends'},
            {time: 23, name: 'Gmail'},
            {time: 40, name: 'Facebook'},
          ],
        },
        {log_id: '4', day: '29 Sep', time: 332,
          entries: [
            {time: 210, name: 'Google Docs'},
            {time: 60, name: 'Wikipedia'},
            {time: 31, name: 'Gmail'},
            {time: 31, name: 'Facebook'},
          ],
        },
      ],
    };
  },
};
</script>
