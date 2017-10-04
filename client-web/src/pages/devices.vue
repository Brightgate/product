<template>
  <f7-page>
    <f7-navbar back-link="Back" title="Brightgate - Devices" sliding>
    </f7-navbar>

    <f7-list v-for="(category, catkey) in categories" v-if="category.network_names.length > 0">
      <f7-list-item divider/>

      <f7-list-item v-if="catkey == 'recent'" group-title 
        v-bind:title="category.name + ' (' + category.network_names.length.toString(10) + ')'"/>
      <f7-list-item v-if="catkey == 'recent'">
        <f7-link v-on:click="showRecent = true" v-if="!showRecent">Show Recent Attempts...</f7-link>
        <f7-link v-on:click="showRecent = false" v-if="showRecent">Hide Recent Attempts...</f7-link>
      </f7-list-item>
      <f7-list-item v-else group-title v-bind:title="category.name"/>

      <f7-list-item 
            v-if="showRecent || catkey != 'recent'"
            v-for="devname in category.network_names"
            v-bind:title="devname"
            v-bind:link="'/details?network_name=' + devname
                        + (by_netname[devname].alert ? '&alert=true' : '') 
                        + (by_netname[devname].notification ? '&notification=true' : '') ">
            <div slot="media">
              <img v-bind:src="'img/nova-solid-' + by_netname[devname].media + '.png'" width=32 height=32>
            </div>
        <div v-if="by_netname[devname].alert">
          <f7-link open-popover="#virus">🚫</f7-link>
        </div>
        <div v-if ="by_netname[devname].notification">
          <f7-link open-popover="#notification">⚠️</f7-link>
        </div>
      </f7-list-item>

    </f7-list>


    <f7-popover id="virus">
      <f7-block> 
        <ul>
            <li>Brightgate detected WannaCry ransomware on this device.</li>
            <li>For your security, Brightgate has disconnected it from the network
                and attempted to prevent the ransomware from encrypting more
                files.</li>
            <li>Visit brightgate.com from another computer for more help.</li>
        </ul>
      </f7-block> 
    </f7-popover>

    <f7-popover id="notification">
      <f7-block> 
        <ul>
            <li>This device is less secure because it is running old software.</li>
            <li>Brightgate can't automatically update this device.</li>
            <li>Follow the manufacturer's instructions to update its software.</li>
        </ul>
      </f7-block> 
    </f7-popover>


  </f7-page>
</template>
<script>

import { mockDevices } from "../mock_devices.js";
var myDevices = mockDevices;

  export default {
    data: function () {
      return {
        showRecent: false,
        categories: myDevices.devices.categories,
        by_netname: myDevices.devices.by_netname
      }
    }
  }
</script>
