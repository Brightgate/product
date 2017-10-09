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
            <f7-block-title>Brightgate Status</f7-block-title>
            <f7-block inner>
              <p><b>One of your computers has a virus.</b><br/>
                See the "Serious Alerts" below.</p>
              <p>Your network is working properly.</p>
            </f7-block>

            <f7-block-title>Serious Alerts</f7-block-title>
            <f7-list>
              <f7-list-item link="/details?uniqid=aa:aa:c8:83:1c:11" title="🚫&nbsp;&nbsp;WannaCry on 'jsmith'"/>
            </f7-list>

            <f7-block-title>Tools</f7-block-title>
            <f7-list>
              <f7-list-item link="/devices/" :title="'Manage Devices (' + $store.getters.Device_Count + ')'"></f7-list-item>
              <f7-list-item title="Open Setup Network">
                <f7-input type="switch" slot="after" :checked="setupOn"></f7-input>
              </f7-list-item>
              <f7-list-item title="Accept Devices">
                <span slot="after"><f7-button @click="openAcceptPopup">Accept</f7-button></span>
              </f7-list-item>
            </f7-list>

            <f7-block-title>Notifications</f7-block-title>
            <f7-list>
              <f7-list-item link="/details?uniqid=c8:bc:c8:83:1c:33" title="⚠️&nbsp;&nbsp;Update iPad 'catpad'"/>
              <f7-list-item link="/details?uniqid=c8:bc:c8:83:1c:22" title="⚠️&nbsp;&nbsp;Update Smart TV 'samsung-un50'"/>
            </f7-list>

          </f7-page>
        </f7-pages>
      </f7-view>
    </f7-views>

    <f7-popup id="acceptPop" v-bind:opened="acceptOpen">
      <f7-block v-if="devicesAccepted">
        <p>Devices Acceptance succeeded.  {{devicesChanged}} devices were affected.</p>
      </f7-block>
      <f7-block v-if="devicesAcceptedError != ''">
        <p>There was an error accepting devices:
          <b>{{ devicesAcceptedError }}.</b></p>
      </f7-block>
      <f7-button @click="closeAcceptPopup">Close</f7-button>
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
