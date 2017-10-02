<template>
  <f7-page>
    <f7-navbar back-link="Back" title="Brightgate - Devices" sliding>
    </f7-navbar>

    <f7-list v-for="category in categories" v-if="category.list.length > 0">
      <f7-list-item divider/>


      <f7-list-item v-if="category == devices.recent" group-title 
                    v-bind:title="category.name + ' (' + category.list.length.toString(10) + ')'"/>
      <f7-list-item v-if="category == devices.recent">
        <f7-link v-on:click="showRecent = true" v-if="!showRecent">Show Recent Attempts...</f7-link>
        <f7-link v-on:click="showRecent = false" v-if="showRecent">Hide Recent Attempts...</f7-link>
      </f7-list-item>
      <f7-list-item v-else group-title v-bind:title="category.name"/>

      <f7-list-item 
            v-if="showRecent || category != devices.recent"
            v-for="device in category.list"
            v-bind:title="device.network_name"
            v-bind:media="device.media"
            v-bind:link="'/details?network_name=' + device.network_name 
                        + (device.alert ? '&alert=true' : '') 
                        + (device.notification ? '&notification=true' : '') ">
        <div v-if="device.alert">
          <f7-link open-popover="#virus">🚫</f7-link>
        </div>
        <div v-if ="device.notification">
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

    <f7-popup id="popup">
      <f7-view navbar-fixed>
        <f7-pages>
          <f7-page>
            <f7-subnavbar title="Connect">
              <f7-nav-right>
                <f7-link close-popup>Close</f7-link>
              </f7-nav-right>
            </f7-subnavbar>
            <f7-block>Lorem ipsum dolor sit amet, consectetur adipisicing elit. Neque, architecto. Cupiditate laudantium rem nesciunt numquam, ipsam. Voluptates omnis, a inventore atque ratione aliquam. Omnis iusto nemo quos ullam obcaecati, quod.</f7-block>
          </f7-page>
        </f7-pages>
      </f7-view>
    </f7-popup>

  </f7-page>
</template>
<script>

// TODO: Need to make this global 
      var myDevices = {
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
              alert: 'alert',
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
        showRecent: false,

        devices: myDevices,
        categories: [myDevices.recent, myDevices.phones, myDevices.computers,
                     myDevices.media, myDevices.things],
      }
    }
  }
</script>
