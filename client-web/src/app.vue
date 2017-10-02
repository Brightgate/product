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
// TODO: Need to make this global 
      var myDevices = {
        count: 9,
        alerts: 1,
        notifications: 2,

        recent: {
          name: 'Recent Attempted Connections',
          list: [
            {
              device: 'Apple iPhone 8',
              network_name: 'nosy-neighbor',
              os_version: 'iOS 11.0.1',
              owner: 'unknown',
              activated: '',
              owner_phone: '',
              owner_email: '',
              media: '<img src="img/nova-solid-mobile-phone-1.png" width=32 height=32>'
            },
          ]
        },

        phones: {
          name: 'Phones & Tablets',
          list: [
            {
              device: 'Apple iPhone 6 Plus',
              network_name: 'CAT',
              os_version: 'iOS 10.3.3',
              owner: 'Christopher Thorpe',
              activated: 'August 10, 2017',
              owner_phone: '+1-617-259-4751',
              owner_email: 'cat@brightgate.com',
              media: '<img src="img/nova-solid-mobile-phone-1.png" width=32 height=32>'
            },
            {
              device: 'Samsung Galaxy S8',
              network_name: 'sch',
              os_version: 'Android',
              owner: 'Stephen Hahn',
              activated: 'August 10, 2017',
              owner_phone: '+1-617-259-4751',
              owner_email: 'stephen@brightgate.com',
              media: '<img src="img/nova-solid-mobile-phone-1.png" width=32 height=32>'
            },
            {
              device: 'Apple iPad 2',
              network_name: 'catpad',
              os_version: 'iOS 9.1.1',
              owner: 'Christopher Thorpe',
              activated: 'August 10, 2017',
              owner_phone: '+1-617-259-4751',
              owner_email: 'cat@brightgate.com',
              notification: 'notification',
              media: '<img src="img/nova-solid-tablet.png" width=32 height=32>'
            },
          ],
        },

        computers: {
          name: 'Computers',
          list: [
            {
              device: 'Apple Macbook Pro',
              network_name: 'catbook',
              os_version: 'MacOS 10.12.4',
              owner: 'Christopher Thorpe',
              activated: 'August 10, 2017',
              owner_phone: '+1-617-259-4751',
              owner_email: 'cat@brightgate.com',
              media: '<img src="img/nova-solid-laptop-2.png" width=32 height=32>'
            },
            {
              device: 'Unknown Linux PC',
              network_name: 'schbook',
              os_version: 'Linux',
              owner: 'Stephen Hahn',
              activated: 'August 10, 2017',
              owner_phone: '+1-617-259-4751',
              owner_email: 'stephen@brightgate.com',
              media: '<img src="img/nova-solid-laptop-1.png" width=32 height=32>'
            },
            {
              device: 'Toshiba Notebook PC',
              network_name: 'jsmith',
              os_version: 'Windows 10',
              owner: 'Christopher Thorpe',
              activated: 'August 10, 2017',
              owner_phone: '+1-617-259-4751',
              owner_email: 'cat@brightgate.com',
              show_alert: 'alert',
              media: '<img src="img/nova-solid-laptop-1.png" width=32 height=32>'
            },
          ],
        },

        media: {
          name: 'Media',
          list: [
            {
              device: 'SONOS Audio System',
              network_name: 'SONOS',
              os_version: 'SONOS',
              owner: 'Christopher Thorpe',
              activated: 'August 10, 2017',
              owner_phone: '+1-617-259-4751',
              owner_email: 'cat@brightgate.com',
              media: '<img src="img/nova-solid-radio-3.png" width=32 height=32>'
            },
            {
              device: 'Apple TV',
              network_name: 'cat-apple-TV',
              os_version: 'Apple TV',
              owner: 'Stephen Hahn',
              activated: 'August 10, 2017',
              owner_phone: '+1-617-259-4751',
              owner_email: 'stephen@brightgate.com',
              media: '<img src="img/nova-solid-television.png" width=32 height=32>'
            },
            {
              device: 'Samsung UN50MU6300 Series 6',
              network_name: 'samsung-un50',
              os_version: '3.0.1',
              owner: 'Christopher Thorpe',
              activated: 'August 10, 2017',
              owner_phone: '+1-617-259-4751',
              owner_email: 'cat@brightgate.com',
              notification: 'notification',
              media: '<img src="img/nova-solid-television.png" width=32 height=32>'
            },
          ]
        },

        things: {
          name: 'Things',
          list: [
            {
              device: 'Logic Circle Security Camera 1',
              network_name: 'logicircle-1',
              os_version: '4.4.4.448',
              owner: 'Christopher Thorpe',
              activated: 'August 10, 2017',
              owner_phone: '+1-617-259-4751',
              owner_email: 'cat@brightgate.com',
              media: '<img src="img/nova-solid-webcam-1.png" width=32 height=32>'
            },
            {
              device: 'Logic Circle Security Camera 2',
              network_name: 'logicircle-2',
              os_version: '4.4.4.448',
              owner: 'Christopher Thorpe',
              activated: 'August 10, 2017',
              owner_phone: '+1-617-259-4751',
              owner_email: 'cat@brightgate.com',
              media: '<img src="img/nova-solid-webcam-1.png" width=32 height=32>'
            },
            {
              device: 'Unknown Device',
              network_name: 'device',
              os_version: 'unknown',
              owner: 'Christopher Thorpe',
              activated: 'August 10, 2017',
              owner_phone: '+1-617-259-4751',
              owner_email: 'cat@brightgate.com',
              media: '<img src="img/nova-solid-interface-question-mark.png" width=32 height=32>'
            },
          ]
        },

      };

export default {
  data: function () {
    return {
      devices: myDevices,
      setupOn: false,
    }
  }
}
</script>
