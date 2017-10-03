<template>
  <!-- App -->
  <div id="app">

    <!-- Statusbar -->
    <f7-statusbar></f7-statusbar>

    <!-- Main Views -->
    <f7-views>
      <f7-view id="main-view" main>
        <f7-pages navbar-fixed>
          <f7-page>
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
                <f7-list-item link="/details?alert=true&network_name=jsmith" title="🚫&nbsp;&nbsp;WannaCry on 'jsmith'"/>
            </f7-list>

            <f7-block-title>Tools</f7-block-title>
            <f7-list>
              <f7-list-item link="/devices/" :title="'Manage Devices (' + devices.count + ')'"></f7-list-item>
              <f7-list-item title="Open Setup Network">
                <f7-input type="switch" slot="after" :checked="setupOn"></f7-input></span>
              </f7-list-item>
            </f7-list>

            <f7-block-title>Notifications</f7-block-title>
            <f7-list>
                <f7-list-item link="/details?notification=true&network_name=catpad" title="⚠️&nbsp;&nbsp;Update iPad 'catpad'"/>
                <f7-list-item link="/details?notification=true&network_name=samsung-un50" title="⚠️&nbsp;&nbsp;Update Smart TV 'samsung-un50'"/>
            </f7-list>


          </f7-page>
        </f7-pages>
      </f7-view>
    </f7-views>

  </div>
</template>

<script>
import { mockDevices } from "./mock_devices";

export default {
  data: function () {
    return {
      devices: mockDevices,
      setupOn: false,
    }
  }
}
</script>
