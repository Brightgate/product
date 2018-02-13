<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <!-- App -->
  <div id="app">

    <!-- Statusbar -->
    <f7-statusbar></f7-statusbar>

    <!-- Main Views -->
    <f7-views>
      <f7-view id="main-view" main>
        <f7-pages navbar-fixed>
          <f7-page
            v-on:page:init="$store.dispatch('fetchDevices')"
            v-on:page:reinit="$store.dispatch('fetchDevices')">
            <f7-navbar>
              <!-- f7-nav-center doesn't seem to center properly without also
                   including left and right. -->
              <f7-nav-left>&nbsp;</f7-nav-left>
              <f7-nav-center><img src="img/bglogo.png"/></f7-nav-center>
              <f7-nav-right>&nbsp;</f7-nav-right>
            </f7-navbar>
            <f7-block-title>{{ $t("message.status.brightgate_status") }}</f7-block-title>
            <f7-block inner>
              <p v-html="$t('message.status.has_virus', {'serious_alerts': $t('message.alerts.serious_alerts')})"></p>
              <p>{{ $t("message.status.network_properly") }}</p>
            </f7-block>

            <f7-block-title>{{ $t("message.alerts.serious_alerts") }}</f7-block-title>
            <f7-list>
              <f7-list-item 
                    v-for="device in $store.getters.All_Devices"
                    v-if="device.alert"
                    :title="$t('message.alerts.wannacry', {'device': device.network_name})"
                    :link="'/details?uniqid=' + device.uniqid">
              </f7-list-item>
            </f7-list>

            <f7-block-title>{{ $t("message.tools.tools") }}</f7-block-title>
            <f7-list>
              <f7-list-item link="/devices/" :title="$t('message.tools.manage_devices', {'device_count': $store.getters.Device_Count})"></f7-list-item>
              <f7-list-item :title="$t('message.tools.open_setup_network')">
                <f7-input type="switch" slot="after" :checked="setupOn"></f7-input>
              </f7-list-item>
              <f7-list-item :title="$t('message.tools.accept_devices')">
                <span slot="after"><f7-button @click="openAcceptPopup">{{ $t("message.general.accept") }}</f7-button></span>
              </f7-list-item>
            </f7-list>

            <f7-block-title>{{ $t("message.notifications.notifications") }}</f7-block-title>
            <f7-list>
              <f7-list-item 
                    v-for="device in $store.getters.All_Devices"
                    v-if="device.notification"
                    :title="$t('message.notifications.update_device', {'device': device.network_name})"
                    :link="'/details?uniqid=' + device.uniqid">
              </f7-list-item>
            </f7-list>

            <f7-block-title>{{ $t("message.testing.testing") }}</f7-block-title>
            <f7-list>
              <f7-list-item :title="$t('message.testing.enable_mock')">
                <f7-input type="switch" slot="after" checked @change="$store.state.enable_mock = !$store.state.enable_mock; $store.dispatch('fetchDevices')"></f7-input>
              </f7-list-item>
              <f7-list-item link="/login/" title="Login"></f7-list-item>
            </f7-list>

          </f7-page>
        </f7-pages>
      </f7-view>
    </f7-views>

    <f7-popup id="acceptPop" :opened="acceptOpen">
      <f7-block v-if="devicesAccepted">
        <p>{{ $t('message.tools.success', {'devicesChanged': devicesChanged}) }}</p>
      </f7-block>
      <f7-block v-if="devicesAcceptedError != ''">
        <p v-html="$t('message.tools.fail', {'devicesAcceptedError': devicesAcceptedError})"></p>
      </f7-block>
      <f7-button @click="closeAcceptPopup"> {{ $t('message.general.close') }} </f7-button>
    </f7-popup>

  </div>
</template>

<script>
import superagent from 'superagent';

export default {

  data: function () {
    return {
      setupOn: false,
      acceptOpen: false,
      devicesAccepted: false,
      devicesAcceptedError: "",
    }
  },

  methods: {

    openAcceptPopup: function () {
      console.log("Accepting devices");
      // get the popup open
      this.acceptOpen = true;
      // clear error and accepted
      this.devicesAccepted = false;
      this.devicesChanged = 0;
      this.devicesAcceptedError = "";
      superagent.get('/apid/supreme').end((err, res) => {
        if (err) {
          console.log("Error accepting devices: ", err);
          this.devicesAcceptedError = err.toString();
        } else {
          console.log("Succeeded accepting devices: " + res.text);
          var res_json = JSON.parse(res.text)
          this.devicesAccepted = true;
          this.devicesChanged = res_json.changed ? res_json.changed : -1;
        }
      })
    },

    closeAcceptPopup: function () {
      this.acceptOpen = false
    },

  }
}
</script>
