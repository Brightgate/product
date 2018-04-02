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
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.devices.title')" sliding>
    </f7-navbar>

    <f7-list v-for="catkey in device_category_order"
             v-if="$store.getters.NumUniqIDs_By_Category(catkey) > 0"
             v-bind:key="catkey">
      <f7-list-item divider/>

      <f7-list-item group-title
              v-bind:title="device_category_description[catkey] +
              (catkey == 'recent' ? ` (${$store.getters.NumUniqIDs_By_Category(catkey)})` : '')"/>
      <f7-list-item v-if="catkey == 'recent'">
        <f7-link v-on:click="showRecent = true" v-if="!showRecent">{{ $t('message.devices.show_recent') }}</f7-link>
        <f7-link v-on:click="showRecent = false" v-if="showRecent">{{ $t('message.devices.hide_recent') }}</f7-link>
      </f7-list-item>

      <f7-list-item
            v-if="showRecent || catkey != 'recent'"
            v-for="device in $store.getters.Devices_By_Category(catkey)"
            v-bind:key="device.uniqid"
            v-bind:title="device.network_name"
            v-bind:link="`/devices/${device.uniqid}/`">
        <div slot="media">
          <img v-bind:src="'img/nova-solid-' + device.media + '.png'" width=32 height=32>
        </div>
        <div v-if="device.alert">
          <f7-link open-popover="#virus">🚫</f7-link>
        </div>
        <div v-if="device.notification">
          <f7-link open-popover="#notification">⚠️</f7-link>
        </div>
      </f7-list-item>

    </f7-list>

    <f7-popover id="virus">
      <f7-block>
        <ul>
            <li>{{ $t("message.alerts.msg.0") }}</li>
            <li>{{ $t("message.alerts.msg.1") }}</li>
            <li>{{ $t("message.alerts.msg.2") }}</li>
        </ul>
      </f7-block>
    </f7-popover>

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

const device_category_description = {
  recent: 'Recent Attempted Connections',
  phone: 'Phones & Tablets',
  computer: 'Computers',
  media: 'Media',
  iot: 'Things',
};

const device_category_order = ['recent', 'phone', 'computer', 'media', 'iot'];

export default {
  data: function() {
    return {
      showRecent: false,
      device_category_description,
      device_category_order,
    };
  },

  methods: {
    pullRefresh: function(event, done) {
      this.$store.dispatch('fetchDevices').then(() => {
        return done();
      }).catch((err) => {
        return done(err);
      });
    },
  },
};
</script>
