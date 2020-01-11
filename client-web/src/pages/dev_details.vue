<!--
  COPYRIGHT 2020 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>
/*
 * See the Framework7 kitchen sink vue and CSS for more on card styling.
 * See the Vue docs for advice on deep >>> selectors.
 */
/* light grey header with bold font */
.vuln-card >>> .card-header {
        display: block;
        padding: 10px;
        font-size: 16px;
        font-weight: bold;
        background: #f8f8f8;
}

/* icon should push headline over a bit */
.vuln-icon {
        display: inline-block;
        padding-right: 4px;
}

span.dev-active {
  color: var(--bg-color-green-60);
  text-align: right;
}
span.dev-inactive {
  color: #888;
  text-align: right;
}

/*
 * Some list items need to have a primary piece of information as well as an
 * explanatory comment or other piece of information below.
 */
div.list-item-primary-info {
  text-align: right;
}
div.list-item-secondary-info {
  text-align: right;
  font-size: 12px;
  color: grey;
}

/*
 * When the accordion-item-opened class gets set on the enclosing li
 * we hide the summarized information in the closed accordion item.
 * Rather than the usual display:none, here we set the opacity to 0
 * (transparent) using a CSS transition to make it smooth.
 */
li.accordion-item >>> span.hide-when-accordion-open {
  opacity: 1;
  transition: 0.4s opacity ease-in;
}

li.accordion-item.accordion-item-opened >>> span.hide-when-accordion-open {
  opacity: 0;
  transition: 0.4s opacity ease-out;
}

div.my-inner-block {
  display: block;
  text-align: right;
}

div.avatar-container {
  display: inline-flex;
  align-items: center;
}

div.title-flex-container {
  display: flex;
  align-items: center;
}

img.title-media-icon {
  padding: 0px 12px 0px 0px;
}

div.title-displayname {
  font-size: 16px;
  font-weight: bold;
}

div.title-model {
  font-size: 14px;
}

</style>
<template>
  <f7-page>
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.dev_details.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-block>
      <div class="title-flex-container">
        <img :src="mediaIcon" class="title-media-icon" width="64" height="64">
        <div>
          <div class="title-displayname">{{ dev.displayName }}</div>
          <div v-if="devModel || devOS" class="title-model">{{ devModel || devOS }}</div>
        </div>
      </div>

      <f7-card v-for="(vuln, vulnid) in activeVulns" :key="vulnid" class="vuln-card">
        <f7-card-header>
          <f7-icon class="vuln-icon" f7="bolt_circle_fill" size="32px" color="red" />
          {{ vulnHeadline(vulnid) }}
        </f7-card-header>
        <f7-card-content>
          <!-- Note: allowed to have HTML content.
               Future work here is to parse the HTML and decorate <a> links
               with target= properly.  Or to support some non-HTML markup -->
          <!-- XXXI18N-- note that presently we don't support localized explanation text -->
          <div v-html="vulnExplanation(vulnid)" />
          <ul style="-webkit-padding-start: 20px; padding-left: 20px;">
            <!-- Note: allowed to have HTML content.
                 Future work here is to parse the HTML and decorate <a> links
                 with target= properly.  Or to support some non-HTML markup -->
            <!-- XXXI18N-- note that presently we don't support localized remediation text -->
            <li v-html="$t('message.dev_details.vuln_remediation', { text: vulnRemediation(vulnid) })" />
            <li>{{ $t('message.dev_details.vuln_first_detected', {time: timeAbs(vuln.first_detected)}) }}</li>
            <li>{{ $t('message.dev_details.vuln_latest_detected', {time: timeRel(vuln.latest_detected)}) }}</li>
            <li v-if="vuln.repaired">
              {{ $t('message.dev_details.vuln_repaired', {time: timeAbs(vuln.repaired)}) }}</li>
            <li v-if="vuln.details">{{ $t('message.dev_details.vuln_details') }}
              <ul>
                <li v-for="detail in vulnSplitDetails(vuln.details)" :key="detail">
                  {{ detail }}
                </li>
              </ul>
            </li>
            <li v-for="vmore in vulnMoreInfo(vulnid)"
                :key="vmore.url">
              <!-- use <a> here instead of f7-link, as that appears to strip out target="_blank".
                 n.b. that this will probably need more work if we use cordova/phonegap -->
              <a :href="vmore.url" rel="noopener" target="_blank" class="link external">
                {{ $t('message.dev_details.vuln_more_info', { source: vmore.source } ) }} &gt;
              </a>
            </li>
          </ul>
          <f7-row v-if="vulnRepairable(vulnid, vuln)">
            <f7-col width="100" desktop-width="50" tablet-width="50">
              <f7-button
                :disabled="vulnRepairing(vulnid, vuln)"
                fill
                @click="vulnRepair(vulnid, vuln)">
                Repair Vulnerability
                <f7-preloader v-if="vulnRepairing(vulnid, vuln)" />
              </f7-button>
            </f7-col>
          </f7-row>
        </f7-card-content>
      </f7-card>

    </f7-block>

    <f7-list>

      <!-- user name -->
      <!-- it's worth noting that we're papering over some technical debt
        here; username, email, UID are conflated and entangled. -->
      <f7-list-item
        v-if="acct"
        :link="`/accounts/${acct.accountUUID}/`"
        :title="$t('message.dev_details.user_name')">
        <div class="avatar-container">
          <div>{{ acct.name }}&nbsp;</div>
          <vue-avatar
            :username="acct.name"
            :src="acct.hasAvatar ? `/api/account/${acct.accountUUID}/avatar` : undefined"
            :size="24" />
        </div>
      </f7-list-item>
      <f7-list-item
        v-else-if="userByUID(dev.username)"
        :link="`/sites/${currentSiteID}/users/${userByUID(dev.username).UUID}/`"
        :title="$t('message.dev_details.user_name')">
        {{ userByUID(dev.username).DisplayName }}
      </f7-list-item>

      <!-- Network: for wired clients -->
      <f7-list-item v-if="!dev.wireless" :title="$t('message.dev_details.connection')">
        <span :class="dev.active ? 'dev-active' : 'dev-inactive'">
          <f7-icon :color="dev.active ? 'green' : 'grey'" material="settings_ethernet" size="16" />
          {{ $t('message.dev_details.wired_port') }}
        </span>
      </f7-list-item>
      <!-- Network: for inactive wireless clients -->
      <f7-list-item v-else-if="!dev.active" :title="$t('message.dev_details.connection')">
        <span class="dev-inactive">
          <f7-icon color="grey" material="signal_wifi_off" size="16" />
          {{ vaps[dev.connVAP].ssid }}
        </span>
      </f7-list-item>
      <!-- Network: for active wireless clients -->
      <f7-list-item v-else :title="$t('message.dev_details.connection')" accordion-item>
        <span slot="after" class="hide-when-accordion-open dev-active">
          <f7-icon color="green" material="wifi" size="16" />
          {{ vaps[dev.connVAP].ssid }}<template v-if="dev.connBand">, {{ dev.connBand }}</template>
          <div v-if="dev.signalStrength">
            <bg-wifi-strength :dbm="dev.signalStrength" />
          </div>
        </span>
        <f7-accordion-content>
          <f7-list inset>
            <f7-list-item :title="$t('message.dev_details.SSID')">
              <span class="dev-active">
                <f7-icon color="green" material="wifi" size="16" />
                {{ vaps[dev.connVAP].ssid }}, {{ dev.connBand }}
              </span>
            </f7-list-item>
            <f7-list-item v-if="dev.signalStrength" :title="$t('message.dev_details.signal_strength')">
              <div class="my-inner-block">
                <div>
                  {{ dev.signalStrength }} dBm, {{ $t('message.api.strength.str_' + strength) }}
                </div>
                <div>
                  <bg-wifi-strength :dbm="dev.signalStrength" />
                </div>
              </div>
            </f7-list-item>
            <f7-list-item v-if="node" :link="`/sites/${$f7route.params.siteID}/nodes/${dev.connNode}/`">
              <span slot="title"><bg-hw-icon :model="node.hwModel" height="30px" /></span>
              <span>{{ nodeName }}</span>
            </f7-list-item>
          </f7-list>
        </f7-accordion-content>
      </f7-list-item>

      <!-- ipv4 addr -->
      <f7-list-item :title="$t('message.dev_details.ipv4_addr')">
        {{ dev.ipv4Addr ? dev.ipv4Addr : $t("message.dev_details.ipv4_addr_none") }}
      </f7-list-item>

      <!-- mac addr -->
      <f7-list-item :title="$t('message.dev_details.hw_addr')">
        <div>
          <div class="list-item-primary-info">{{ dev.hwAddr }}</div>
          <div class="list-item-secondary-info">{{ devOUI }}</div>
        </div>
      </f7-list-item>

      <f7-list-item :title="$t('message.dev_details.vuln_scan')">
        {{ lastVulnScan }}
      </f7-list-item>
    </f7-list>

    <f7-block-title>{{ $t("message.dev_details.access_control") }}</f7-block-title>
    <f7-list form>
      <f7-list-input
        ref="ringInput"
        :title="$t('message.dev_details.security_ring')"
        :label="$t('message.dev_details.security_ring')"
        :value="dev.ring"
        :key="0"
        inline-label
        type="select"
        @change="changeRing($event.target.value)">
        <option
          v-for="(ring, ringName) in vapRings"
          :value="ringName"
          :key="ringName">
          {{ ringName }}
        </option>
      </f7-list-input>
    </f7-list>

    <f7-block-title>{{ $t("message.dev_details.attributes") }}</f7-block-title>
    <f7-list>
      <!-- name -->
      <f7-list-item :title="$t('message.dev_details.name')">
        <div>
          <div class="list-item-primary-info">{{ dev.displayName }}
            <f7-link v-if="supportsFriendlyNameChange" icon-material="edit" @click="clientFriendlyDialog" />
          </div>
          <div v-if="supportsFriendlyNameChange" class="list-item-secondary-info">
            {{ dev.friendlyName ? $t('message.dev_details.name_admin') : $t('message.dev_details.name_auto') }}
          </div>
        </div>
      </f7-list-item>

      <!-- dhcp id -->
      <f7-list-item>
        <bg-list-item-title
          slot="title"
          :title="$t('message.dev_details.dhcp_id')"
          :tip="$t('message.dev_details.dhcp_id_tip')" />
        <span v-if="dev.dhcpName">
          {{ dev.dhcpName }}
        </span>
        <span v-else class="disabled">
          {{ $t('message.dev_details.dhcp_id_none') }}
        </span>
      </f7-list-item>

      <!-- DNS Name -->
      <f7-list-item>
        <bg-list-item-title
          slot="title"
          :title="$t('message.dev_details.dns_name')"
          :tip="$t('message.dev_details.dns_name_tip')" />
        <span v-if="dnsName">
          {{ dnsName }}<span class="disabled">.{{ networkConfig.dns.domain }}</span>
        </span>
        <span v-else class="disabled">
          {{ $t('message.dev_details.dns_name_none') }}
        </span>
      </f7-list-item>

      <template v-if="dev.devID">
        <!-- Model Name -->
        <f7-list-item>
          <bg-list-item-title
            slot="title"
            :title="$t('message.dev_details.model_name')"
            :tip="$t('message.dev_details.devid_tip')" />
          <span v-if="devModel">
            {{ devModel }}
          </span>
          <span v-else class="disabled">
            {{ $t('message.dev_details.model_name_none') }}
          </span>
        </f7-list-item>

        <!-- OS Name -->
        <f7-list-item>
          <bg-list-item-title
            slot="title"
            :title="$t('message.dev_details.os_name')"
            :tip="$t('message.dev_details.devid_tip')" />
          <span v-if="devOS">
            {{ devOS }}
          </span>
          <span v-else class="disabled">
            {{ $t('message.dev_details.os_name_none') }}
          </span>
        </f7-list-item>
      </template>
    </f7-list>

  </f7-page>
</template>
<script>
import assert from 'assert';
import vuex from 'vuex';
import {isBefore, isEqual} from 'date-fns';
import {pickBy} from 'lodash-es';
import Debug from 'debug';
import VueAvatar from 'vue-avatar';
import {format, formatRelative, parseISO} from '../date-fns-wrapper';

import uiUtils from '../uiutils';
import vulnerability from '../vulnerability';
import BGHWIcon from '../components/hw_icon.vue';
import BGListItemTitle from '../components/list_item_title.vue';
import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';
import BGWifiStrength from '../components/wifi_strength.vue';

const debug = Debug('page:dev-details');

function repairable(vulnid, vuln) {
  const res = (vulnid === 'defaultpassword' &&
    vuln.details.includes('Service: ssh') &&
    (!vuln.repaired || vuln.repaired < vuln.latest_detected));
  debug(`repairable ${vulnid} returning ${res}`, vuln);
  return res;
}

export default {
  components: {
    'bg-hw-icon': BGHWIcon,
    'bg-list-item-title': BGListItemTitle,
    'bg-site-breadcrumb': BGSiteBreadcrumb,
    'bg-wifi-strength': BGWifiStrength,
    'vue-avatar': VueAvatar,
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'accountByEmail',
      'userByUID',
      'currentSiteID',
      'features',
      'networkConfig',
      'nodes',
      'vaps',
    ]),

    devModel: function() {
      if (!this.dev.devID || !this.dev.devID.deviceGenus || this.dev.devID.deviceGenus === 'unknown') {
        return '';
      }
      return this.dev.devID.deviceGenus;
    },
    devOUI: function() {
      if (!this.dev.devID || !this.dev.devID.ouiMfg) {
        return '';
      }
      return this.dev.devID.ouiMfg;
    },
    devOS: function() {
      if (!this.dev.devID || !this.dev.devID.osGenus) {
        return '';
      }
      return this.dev.devID.osGenus;
    },
    mediaIcon: function() {
      return this.dev.active ?
        `img/devid/${this.dev.media}-active.png` :
        `img/devid/${this.dev.media}.png`;
    },
    activity: function() {
      return this.dev.active ?
        this.$t('message.dev_details.active_true') :
        this.$t('message.dev_details.active_false');
    },
    activeVulns: function() {
      return pickBy(this.dev.vulnerabilities, 'active');
    },
    lastVulnScan: function() {
      let start = null;
      let finish = null;
      if (this.dev && this.dev.scans && this.dev.scans.vuln) {
        const sp = parseISO(this.dev.scans.vuln.start);
        if (!Number.isNaN(sp.valueOf())) {
          start = sp;
        }
        const fp = parseISO(this.dev.scans.vuln.finish);
        if (!Number.isNaN(fp.valueOf())) {
          finish = fp;
        }
      }
      if (start === null && finish === null) {
        return this.$t('message.dev_details.vuln_scan_notyet');
      }
      if (finish === null) {
        return this.$t('message.dev_details.vuln_scan_initial');
      }
      if (isBefore(start, finish) || isEqual(start, finish)) {
        return format(finish, 'Pp');
      }
      return this.$t('message.dev_details.vuln_scan_rescan');
    },
    dev: function() {
      const uniqid = this.$f7route.params.UniqID;
      return this.$store.getters.deviceByUniqID(uniqid);
    },
    node: function() {
      if (this.dev.connNode && this.nodes[this.dev.connNode]) {
        return this.nodes[this.dev.connNode];
      }
      return undefined;
    },
    nodeName: function() {
      return uiUtils.formatNodeName(this, this.nodes, this.dev.connNode);
    },
    acct: function() {
      if (!this.dev || !this.dev.username) {
        return undefined;
      }
      return this.accountByEmail(this.dev.username);
    },

    strength: function() {
      const dbm = this.dev.signalStrength;
      return String(uiUtils.dBmToStrength(dbm));
    },

    dnsName: function() {
      if (this.dev.dnsName) {
        return this.dev.dnsName;
      }
      if (this.dev.friendlyDNS) {
        return this.dev.friendlyDNS;
      }
      return undefined;
    },

    supportsFriendlyNameChange: function() {
      return !!this.features.clientFriendlyName;
    },

    // Return the subset of rings acceptable for the device's VAP.
    // If the VAP is missing, or something else goes wrong, return all rings.
    //
    // XXX This is arguably dangerous and we might need to adjust.
    // Another option would be to return no rings and disable the UI.
    vapRings: function() {
      const uniqid = this.$f7route.params.UniqID;
      const dev = this.$store.getters.deviceByUniqID(uniqid);
      const allRings = this.$store.getters.rings;
      const vaps = this.vaps;
      try {
        if (!dev.connVAP || !vaps[dev.connVAP]) {
          debug('missing information; returning allRings',
            dev.connVAP, vaps[dev.connVAP]);
          return allRings;
        }
        return pickBy(allRings, (val, key) => {
          return vaps[dev.connVAP].rings.includes(key);
        });
      } catch (err) {
        debug('error filtering rings', err);
        return allRings;
      }
    },
  },

  methods: {
    timeAbs: function(t) {
      return format(parseISO(t), 'Pp');
    },
    timeRel: function(t) {
      return formatRelative(parseISO(t), Date.now());
    },

    vulnHeadline: function(vulnid) {
      return vulnerability.headline(vulnid);
    },
    vulnExplanation: function(vulnid) {
      return vulnerability.explanation(vulnid);
    },
    vulnRemediation: function(vulnid) {
      return vulnerability.remediation(vulnid);
    },
    vulnMoreInfo: function(vulnid) {
      return vulnerability.moreInfo(vulnid);
    },
    vulnRepairable: function(vulnid, vuln) {
      return repairable(vulnid, vuln);
    },
    vulnRepair: function(vulnid, vuln) {
      const uniqid = this.$f7route.params.UniqID;
      if (!repairable(vulnid, vuln)) {
        debug('in repair but vulnerability is not repairable');
        return;
      }
      this.$store.dispatch('repairVuln', {deviceID: uniqid, vulnID: vulnid});
    },
    vulnRepairing: function(vulnid, vuln) {
      const uniqid = this.$f7route.params.UniqID;
      const dev = this.$store.getters.deviceByUniqID(uniqid);
      return dev.vulnerabilities &&
        dev.vulnerabilities[vulnid] &&
        dev.vulnerabilities[vulnid].repair === true;
    },
    vulnSplitDetails: function(details) {
      return details.split('|');
    },
    changeRing: async function(newRing) {
      assert(typeof newRing === 'string');
      const uniqid = this.$f7route.params.UniqID;
      const dev = this.$store.getters.deviceByUniqID(uniqid);

      if (newRing === dev.ring) {
        return;
      }

      const storeArg = {
        deviceUniqID: this.dev.uniqid,
        newRing: newRing,
      };
      await uiUtils.submitConfigChange(this, 'changeRing (device)', 'changeRing',
        storeArg, (err) => {
          return this.$t('message.dev_details.change_ring_err',
            {dev: this.dev.displayName, ring: newRing, err: err});
        }
      );
    },
    clientFriendlyDialog: async function() {
      debug('clientFriendlyDialog');
      let newFriendly;
      try {
        newFriendly = await new Promise((resolve, reject) => {
          this.$f7.dialog.prompt(this.$t('message.dev_details.name_entry'),
            resolve, reject, this.dev.friendlyName);
        });
      } catch (err) {
        if (typeof err === 'string') {
          // This is the user canceling the dialog
          debug('user canceled dialog');
          return;
        }
        // some other error
        throw err;
      }

      // Could do additional/more snazzy name validation in the future, but the
      // API will check the friendly name and reject if it's unacceptable.
      debug('clientFriendlyDialog got new name', newFriendly);
      const storeArg = {
        deviceUniqID: this.dev.uniqid,
        newFriendly: newFriendly,
      };
      await uiUtils.submitConfigChange(this, 'setDeviceFriendly', 'setDeviceFriendly',
        storeArg, (err) => {
          return this.$t('message.dev_details.name_entry_err',
            {dev: this.dev.hwAddr, newFriendly: newFriendly, err: err});
        }
      );
    },
  },
};
</script>
