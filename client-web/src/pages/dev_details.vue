<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

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

</style>
<template>
  <f7-page>
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.dev_details.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-block>
      <f7-row>
        <!-- use margin-auto to center the icon -->
        <f7-col style="margin: auto" width="20">
          <img :src="mediaIcon" width="48" height="48">
        </f7-col>
        <f7-col width="80">
          <template v-if="dev.certainty !== 'low'">
            <div style="font-size: 16pt; font-weight: bold">{{ devModel }}</div>
            <div style="font-size: 12pt; font-weight: normal; color: rgba(0,0,0,.5);">{{ devManufacturer }}</div>
            <div v-if="dev.certainty === 'medium'" style="font-size: 10pt; font-weight: normal; color: rgba(0,0,0,0.5);">
              {{ $t('message.dev_details.uncertain_device') }}
            </div>
          </template>
          <template v-else>
            <div style="font-size: 16pt; font-weight: bold">{{ dev.displayName }}</div>
          </template>
        </f7-col>
      </f7-row>

      <f7-card v-for="(vuln, vulnid) in activeVulns" :key="vulnid" class="vuln-card">
        <f7-card-header>
          <f7-icon class="vuln-icon" f7="bolt_round_fill" size="32px" color="red" />
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

      <f7-row v-if="dev.notification">
        <f7-col style="margin: auto" width="20">
          <f7-icon f7="bolt_round_fill" size="32px" color="yellow" />
        </f7-col>
        <f7-col width="80">
          <!-- tweak the ul rendering not to inset so much (default is 40px) -->
          <ul style="-webkit-padding-start: 20px; padding-left: 20px;">
            <li>{{ $t("message.notifications.msg.0") }}</li>
            <li>{{ $t("message.notifications.msg.1") }}</li>
            <li>{{ $t("message.notifications.msg.2") }}</li>
          </ul>
        </f7-col>
      </f7-row>
    </f7-block>

    <f7-list>

      <f7-list-item :title="$t('message.dev_details.network_name')">
        {{ dev.displayName }}
      </f7-list-item>
      <f7-list-item :title="$t('message.dev_details.ipv4_addr')">
        {{ dev.ipv4Addr ? dev.ipv4Addr : $t("message.dev_details.ipv4_addr_none") }}
      </f7-list-item>
      <f7-list-item :title="$t('message.dev_details.hw_addr')">
        {{ dev.hwAddr }}
      </f7-list-item>

      <f7-list-item :title="$t('message.dev_details.activity')">
        {{ activity }}
      </f7-list-item>
      <f7-list-item v-if="dev.active && dev.connVAP" :title="$t('message.dev_details.conn_vap')">
        <span>
          <f7-icon material="wifi" size="16" />
          {{ vaps[dev.connVAP].ssid }}
          <template v-if="dev.connBand">, {{ dev.connBand }}</template>
        </span>
      </f7-list-item>

      <f7-list-item :title="$t('message.dev_details.vuln_scan')">
        {{ lastVulnScan }}
      </f7-list-item>
    </f7-list>

    <f7-block-title>{{ $t("message.dev_details.access_control") }}</f7-block-title>
    <f7-list form>
      <f7-list-item
        v-if="ringChanging"
        :title="$t('message.dev_details.security_ring')">
        <f7-preloader />
      </f7-list-item>
      <f7-list-input
        v-else
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
  </f7-page>
</template>
<script>
import assert from 'assert';
import vuex from 'vuex';
import {isBefore, isEqual} from 'date-fns';
import {pickBy} from 'lodash-es';
import Debug from 'debug';
import {format, formatRelative, parseISO} from '../date-fns-wrapper';

import vulnerability from '../vulnerability';
import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';

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
    'bg-site-breadcrumb': BGSiteBreadcrumb,
  },

  data: function() {
    return {
      ringChanging: false,
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'vaps',
    ]),

    devModel: function() {
      return (this.dev.certainty === 'low') ?
        this.$t('message.dev_details.unknown_model') :
        this.dev.model;
    },
    devManufacturer: function() {
      return (this.dev.certainty === 'low') ?
        this.$t('message.dev_details.unknown_manufacturer') :
        this.dev.manufacturer;
    },
    mediaIcon: function() {
      return this.dev.active ?
        `img/nova-solid-${this.dev.media}-active.png` :
        `img/nova-solid-${this.dev.media}.png`;
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

    // Return the subset of rings acceptable for the device's VAP.
    // If the VAP is missing, or something else goes wrong, return all rings.
    //
    // XXX This is arguably dangerous and we might need to adjust.
    // Another option would be to return no rings and disable the UI.
    vapRings: function() {
      const uniqid = this.$f7route.params.UniqID;
      const dev = this.$store.getters.deviceByUniqID(uniqid);
      const allRings = this.$store.getters.rings;
      const vaps = this.$store.getters.vaps;
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
    changeRing: function(newRing) {
      assert(typeof newRing === 'string');
      const uniqid = this.$f7route.params.UniqID;
      const dev = this.$store.getters.deviceByUniqID(uniqid);
      debug(`Change Ring ${dev.ring} -> ${newRing}`);
      if (this.ringChanging) {
        return;
      }
      if (newRing === dev.ring) {
        return;
      }
      this.ringChanging = true;
      this.$store.dispatch('changeRing', {
        deviceUniqID: this.dev.uniqid,
        newRing: newRing,
      }).then(() => {
        this.ringChanging = false;
      }).catch((err) => {
        debug('Change Ring failed', err);
        const txt = `Failed to change trust group for ${this.dev.displayName} to ${newRing}: ${err}`;
        this.ringChanging = false;
        this.$f7.toast.show({
          text: txt,
          closeButton: true,
          destroyOnClose: true,
        });
      });
    },
  },
};
</script>
