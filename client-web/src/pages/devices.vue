<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page ptr @ptr:refresh="pullRefresh">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.devices.title')" sliding />

    <f7-list>
      <f7-list-group v-for="catkey in device_category_order"
                     v-if="Device_Count(Devices_By_Category(catkey)) > 0"
                     :key="catkey">

        <f7-list-item :title="$t(`message.devices.cats.${catkey}`) +
                      (catkey == 'recent' ? ` (${Device_Count(Devices_By_Category(catkey))})` : '')"
                      group-title />
        <f7-list-item v-if="catkey == 'recent'">
          <f7-link v-if="!showRecent" @click="showRecent = true">{{ $t('message.devices.show_recent') }}</f7-link>
          <f7-link v-if="showRecent" @click="showRecent = false">{{ $t('message.devices.hide_recent') }}</f7-link>
        </f7-list-item>

        <template v-if="showRecent || catkey != 'recent'">
          <f7-list-item
            v-for="device in Devices_By_Category(catkey)"
            :key="device.uniqid"
            :title="device.network_name"
            :link="`/devices/${device.uniqid}/`">
            <div slot="media">
              <img :alt="device.category" :src="media_icon(device)" width="32" height="32">
            </div>
            <div v-if="alert(device)">
              <f7-icon f7="bolt_round_fill" color="red" />
            </div>
            <div v-if="device.notification">
              <f7-link popover-open="#notification">⚠️</f7-link>
            </div>
          </f7-list-item>
        </template>

      </f7-list-group>
    </f7-list>

    <f7-popover id="notification">
      <f7-block>
        <ul>
          <li>{{ $t("message.notifications.msg.0") }}</li>
          <li>{{ $t("message.notifications.msg.1") }}</li>
          <li>{{ $t("message.notifications.msg.2") }}</li>
        </ul>
      </f7-block>
    </f7-popover>

  </f7-page>
</template>
<script>
import {f7Popover} from 'framework7-vue';
import Vuex from 'vuex';
import {sortBy} from 'lodash-es';

const device_category_order = ['recent', 'phone', 'computer', 'printer', 'media', 'iot', 'unknown'];

export default {
  components: {f7Popover},

  data: function() {
    return {
      showRecent: false,
      device_category_order,
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...Vuex.mapGetters([
      'Device_Count',
    ]),
    Devices_By_Category: function() {
      return (category) => {
        const devs = this.$store.getters.Devices_By_Category(category);
        // Sort by lowercase network name, then by uniqid in case of clashes
        return sortBy(devs, [(device) => {
          return device.network_name.toLowerCase();
        }, 'uniqid']);
      };
    },
  },

  methods: {
    pullRefresh: function(event, done) {
      this.$store.dispatch('fetchDevices').then(() => {
        return done();
      }).catch((err) => {
        return done(err);
      });
    },

    media_icon: function(dev) {
      return dev.active ?
        `img/nova-solid-${dev.media}-active.png` :
        `img/nova-solid-${dev.media}.png`;
    },
    alert: function(dev) {
      return dev.activeVulnCount > 0;
    },
  },
};
</script>
