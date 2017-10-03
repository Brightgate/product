<template>
  <f7-page>
    <f7-navbar back-link="Back" v-bind:title="device_details.network_name + ' - Details'" sliding>
    </f7-navbar>

    <div v-if="show_notification">
      <f7-block-title>⚠️&nbsp;&nbsp;Security Notification</f7-block-title>
      <f7-block inner>
        <li>This device is less secure because it is running old software.</li>
        <li>Brightgate can't automatically update this device.</li>
        <li>Follow the manufacturer's instructions to update its software.</li>
      </f7-block>
    </div>

    <div v-if="show_alert">
      <f7-block-title>🚫&nbsp;&nbsp;Important Alert</f7-block-title>
      <f7-block inner>
        <li>Brightgate detected WannaCry ransomware on this device.</li>
        <li>For your security, Brightgate has disconnected it from the network
          and attempted to prevent the ransomware from encrypting more files.</li>
        <li>Visit brightgate.com from another computer for more help.</li>
      </f7-block>
    </div>

    <f7-block-title>Device Details</f7-block-title>
    <f7-list inner>
      <f7-list-item title="Device">{{ device_details.device }}</f7-list-item>
      <f7-list-item title="Network Name">{{ device_details.network_name }}</f7-list-item>
      <f7-list-item title="Owner">
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

    <f7-block-title>Access Control</f7-block-title>
    <f7-block inner>
      <div v-if="show_alert">
        <p>🚫&nbsp;&nbsp;For your security, Brightgate has blocked this device
        from your network.  See the alert above for more details.
        </p>
      </div>
      <p>
      Guest access expires in {{ render_time(expiration) }}.<br/><br/>
      <f7-grid>
        <f7-col>
          <f7-button big color="green" v-on:click="expiration=(expiration+60)">Extend 1 h</f7-button>
        </f7-col>
        <f7-col>
          <div v-if="!paused">
            <f7-button big color="orange" v-on:click="paused=true">Pause</f7-button>
          </div><div v-if="paused">
            <f7-button big fill color="orange" v-on:click="paused=false">Unpause</f7-button>
          </div>
        </f7-col>
        <f7-col><f7-button big open-popover="#confirm-remove" color="red">Remove</f7-button></f7-col>
      </f7-grid>

      </p>
    </f7-block>

    <f7-block-title>Activity</f7-block-title>
    <f7-list inner>
      <f7-list-item v-for="log_day in log_details" :title="log_day.day">
        <span>{{ render_time(log_day.time) }} &mdash;
          <a href="#" v-bind:data-popover="'#logs' + log_day.log_id" class="open-popover link">Details</a>
          <!-- <f7-link v-link="log_day.link">Details</f7-link> -->
        </span>
      </f7-list-item>
      <f7-list-item>
        <f7-link open-popover="#limited-logs">Older...</f7-link>
      </f7-list-item>
    </f7-list>

    <f7-block-title>Additional Information</f7-block-title>
    <f7-list inner>
      <f7-list-item title="Network Name">{{ device_details.network_name }}</f7-list-item>
      <f7-list-item title="OS Version">{{ device_details.os_version }}</f7-list-item>
      <f7-list-item title="Activated">{{ device_details.activated }}</f7-list-item>
      <f7-list-item title="Owner">{{ device_details.owner }}</f7-list-item>
      <f7-list-item title="Owner Email">
          <f7-link v-bind:href="'mailto:' + device_details.owner_email" external>{{ device_details.owner_email }}</f7-link>
      </f7-list-item>
      <f7-list-item title="Owner Phone">
          <f7-link v-bind:href="'tel:' + device_details.owner_phone" external>{{ device_details.owner_phone }}</f7-link>
      </f7-list-item>
    </f7-list>

    <f7-popover v-for="log_day in log_details" v-bind:id="'logs' + log_day.log_id">
      <f7-block>
        <ul>
          <div v-for="entry in log_day.entries">
            <li>{{ entry.name }} ({{render_time(entry.time)}})</li>
          </div>
        </ul>
      </f7-block>
    </f7-popover>

    <f7-popover id="limited-logs">
      <f7-block>
        Earlier logs are not yet supported.
      </f7-block>
    </f7-popover>

    <f7-popover id="confirm-remove">
      <f7-block>
        <p>
        Please confirm you'd like to permanently remove the device
        "{{ device_details.network_name }}" from the network.
        </p>
        <f7-grid>
          <f7-col width=20>&nbsp;</f7-col>
          <f7-col width=40>
            <f7-button big color="gray" close-popover>Cancel</f7-button>
          </f7-col>
          <f7-col width=40>
            <f7-button big fill close-popover color="red">Confirm</f7-button>
          </f7-col>
        </f7-grid>
      </f7-block>
    </f7-popover>

  </f7-page>
</template>
<script>
import { mockDevices } from "../mock_devices";

export default {
  data: function () {
    // vue's idea of the current query params
    var query = this.$route.query
    console.log("Device details: query is " + JSON.stringify(query))
    return {
      render_time: function (mins) {
        var days  = Math.floor(mins / 1440);
        var hours = Math.floor((mins % 1440) / 60);
        var rest  = Math.floor(mins % 60);
        var result = '';
        if (days > 0) {
          result += " " + days + " d";
        }
        if (hours > 0) {
          result += " " + hours + " h";
        }
        if (rest > 0) {
          result += " " + rest + " m";
        }
        return result;
      },
      paused: false,
      expiration: 314,
      // In the future, we can use this query to filter specific device info
      // if we can't get dynamic routes to work properly
      query: query,
      show_alert: query.alert,
      show_notification: query.notification,
      device_details: mockDevices.devices.by_netname[query.network_name],
      log_details: [
        { log_id: "0", day: "Today",     time: 71,
          entries: [
            { time: 41, name: 'League of Legends' },
            { time: 20, name: 'Gmail' },
            { time: 10, name: 'Facebook' },
          ],
        },
        { log_id: "1", day: "Yesterday", time: 162,
          entries: [
            { time: 162, name: 'League of Legends' },
          ],
        },
        { log_id: "2", day: "Sunday",    time: 211,
          entries: [
            { time: 181, name: 'League of Legends' },
            { time: 20, name: 'Gmail' },
            { time: 10, name: 'Facebook' },
          ],
        },
        { log_id: "3", day: "Saturday",  time: 424,
          entries: [
            { time: 361, name: 'League of Legends' },
            { time:  23, name: 'Gmail' },
            { time:  40, name: 'Facebook' },
          ],
        },
        { log_id: "4", day: "29 Sep",    time: 332,
          entries: [
            { time: 210, name: 'Google Docs' },
            { time: 60, name: 'Wikipedia' },
            { time: 31, name: 'Gmail' },
            { time: 31, name: 'Facebook' },
          ],
        },
      ]
    }
  }
}
</script>
