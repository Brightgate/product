<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>
span.ip-addr {
  color: #666;
  display: inline-block;
  padding-right: 1em;
}
i.subtitle-conn-icon {
  color: #666;
  display: inline-block;
  padding-right: 6px;
}
.md div.smaller-chip {
  height: 24px;
}
.md div.smaller-chip-media {
  width: 24px;
  height: 24px;
}
.md div.smaller-chip-label {
  margin-left: 0px;
}
.md i.smaller-chip-icon {
  font-size: 20px;
}
div.shorter-block {
  margin: 16px 0;
}
</style>
<template>
  <f7-page ptr @ptr:refresh="pullRefresh">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.devices.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-block class="shorter-block">
      <f7-checkbox :checked="showInactive" @change="toggleInactive" /> Show inactive
    </f7-block>
    <f7-list class="shorter-block">
      <f7-list-group v-for="ringkey in RING_ORDER"
                     v-if="deviceCount(devicesByRing(ringkey)) > 0"
                     :key="ringkey">

        <f7-list-item group-title>
          {{ $te('message.general.rings.' + ringkey) ? $t('message.general.rings.' + ringkey) : ringkey }}
        </f7-list-item>
        <f7-list-item v-for="device in devicesByRing(ringkey)"
                      :key="device.uniqid"
                      :title="device.displayName"
                      :link="`${$f7route.url}${device.uniqid}/`"
                      chevron-center media-item>
          <div slot="media">
            <img :alt="device.category" :src="mediaIcon(device)" width="32" height="32">
          </div>
          <div slot="subtitle">
            <f7-icon v-if="device.wireless" class="subtitle-conn-icon" material="wifi" />
            <f7-icon v-if="device.wireless === false" class="subtitle-conn-icon" material="settings_ethernet" />
            <span v-if="device.active && device.ipv4Addr" class="ip-addr">
              {{ device.ipv4Addr }}
            </span>
            <!--
              We need a little more control of styling here (to shrink the chip
              size for material design) than we can get using the vue
              component; so this is coded to F7 directly.
            -->
            <div v-if="alert(device)" class="chip color-red smaller-chip">
              <div class="chip-media smaller-chip-media bg-color-red">
                <f7-icon slot="media" class="smaller-chip-icon" f7="bolt_fill" />
              </div>
              <div class="chip-label smaller-chip-label">
                {{ $tc('message.devices.num_alerts', alert(device), {count: alert(device)}) }}
              </div>
            </div>
          </div>
          <div v-if="device.notification">
            <f7-link popover-open="#notification">⚠️</f7-link>
          </div>
        </f7-list-item>

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
import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';

const DEVICE_CATEGORY_ORDER = ['recent', 'phone', 'computer', 'printer', 'media', 'iot', 'unknown'];
const RING_ORDER = ['unenrolled', 'quarantine', 'core', 'standard', 'devices', 'guest'];

export default {
  components: {
    'bg-site-breadcrumb': BGSiteBreadcrumb,
    f7Popover,
  },

  data: function() {
    return {
      showInactive: false,
      showRecent: false,
      DEVICE_CATEGORY_ORDER,
      RING_ORDER,
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...Vuex.mapGetters([
      'deviceCount',
      'vaps',
    ]),
    // Alpha special: suppress list group titles if all devices are of
    // the 'unknown' type.
    showTitle: function() {
      const allDevs = this.$store.getters.devices;
      const unknownDevs = this.$store.getters.devicesByCategory('unknown');
      if (unknownDevs.length === allDevs.length) {
        return false;
      }
      return true;
    },
    devicesByCategory: function() {
      return (category) => {
        let devs = this.$store.getters.devicesByCategory(category);
        if (!this.showInactive) {
          devs = devs.filter((dev) => dev.active);
        }
        // Sort by lowercase display name, then by uniqid in case of clashes
        return sortBy(devs, [(device) => {
          return device.displayName.toLowerCase();
        }, 'uniqid']);
      };
    },
    devicesByRing: function() {
      return (ring) => {
        let devs = this.$store.getters.devicesByRing(ring);
        if (!this.showInactive) {
          devs = devs.filter((dev) => dev.active);
        }
        // Sort by lowercase display name, then by uniqid in case of clashes
        return sortBy(devs, [(device) => {
          return device.displayName.toLowerCase();
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

    mediaIcon: function(dev) {
      return dev.active ?
        `img/nova-solid-${dev.media}-active.png` :
        `img/nova-solid-${dev.media}.png`;
    },
    alert: function(dev) {
      return dev.activeVulnCount;
    },
    toggleInactive: function() {
      this.showInactive = !this.showInactive;
    },
  },
};
</script>
