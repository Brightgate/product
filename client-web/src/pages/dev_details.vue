<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page>
    <f7-navbar :back-link="$t('message.general.back')" :title="dev.network_name + $t('message.dev_details._details')" sliding />

    <f7-block>
      <f7-row>
        <!-- use margin-auto to center the icon -->
        <f7-col style="margin: auto" width="20">
          <img :src="media_icon" width="32" height="32">
        </f7-col>
        <f7-col width="80">
          <div style="font-size: 16pt; font-weight: bold">{{ dev_model }}</div>
          <div style="font-size: 12pt; font-weight: normal; color: rgba(0,0,0,.5);">{{ dev_manufacturer }}</div>
          <div v-if="dev.certainty === 'medium'" style="font-size: 10pt; font-weight: normal; color: rgba(0,0,0,0.5);">
            {{ $t('message.dev_details.uncertain_device') }}
          </div>
        </f7-col>
      </f7-row>

      <f7-row v-for="(vuln, vulnid) in activeVulns" :key="vulnid">
        <!-- use margin-auto to center the icon -->
        <f7-col style="margin: auto" width="20">
          <f7-icon f7="bolt_round_fill" size="32px" color="red" />
        </f7-col>
        <f7-col width="80">
          <!-- tweak the ul rendering not to inset so much (default is 40px) -->
          <h3>{{ vulnHeadline(vulnid) }}</h3>
          <ul style="-webkit-padding-start: 20px; padding-left: 20px;">
            <!-- XXXI18N-- note that presently we don't support localized explanation text -->
            <!-- Note: allowed to have HTML content.
                 Future work here is to parse the HTML and decorate <a> links
                 with target= properly.  Or to support some non-HTML markup -->
            <li v-html="vulnExplanation(vulnid)" />

            <!-- Note: allowed to have HTML content.
                 Future work here is to parse the HTML and decorate <a> links
                 with target= properly.  Or to support some non-HTML markup -->
            <!-- XXXI18N-- note that presently we don't support localized remediation text -->
            <li v-html="$t('message.dev_details.vuln_remediation', { text: vulnRemediation(vulnid) })" />
            <li>{{ $t('message.dev_details.vuln_first_detected', {time: timeAbs(vuln.first_detected)}) }}</li>
            <li>{{ $t('message.dev_details.vuln_latest_detected', {time: timeRel(vuln.latest_detected)}) }}</li>
          </ul>
          <span v-for="vmore in vulnMoreInfo(vulnid)"
                :key="vmore">
            <!-- use <a> here instead of f7-link, as that appears to strip out target="_blank".
                 n.b. that this will probably need more work if we use cordova/phonegap -->
            <a :href="vmore" rel="noopener" target="_blank" class="link external">
              {{ $t('message.dev_details.vuln_more_info') }} &gt;
            </a>
            <br>
          </span>
        </f7-col>
      </f7-row>

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

      <f7-list-item :title="$t('message.dev_details.network_name')">{{ dev.network_name }}</f7-list-item>
      <f7-list-item :title="$t('message.dev_details.ipv4_addr')">{{ dev.ipv4_addr }}</f7-list-item>
      <f7-list-item :title="$t('message.dev_details.hw_addr')">{{ dev.hwaddr }}</f7-list-item>
      <f7-list-item :title="$t('message.dev_details.os_version')">{{ os_version }}</f7-list-item>

      <f7-list-item :title="$t('message.dev_details.activity')">
        {{ activity }}
      </f7-list-item>

      <f7-list-item :title="$t('message.dev_details.vuln_scan')">
        {{ lastVulnScan }}
      </f7-list-item>
    </f7-list>

    <f7-block-title>{{ $t("message.dev_details.access_control") }}</f7-block-title>
    <f7-list form>
      <f7-list-item item-input inline-label>
        <f7-label>{{ $t('message.dev_details.security_ring') }}</f7-label>
        <f7-preloader v-if="ring_changing" />
        <f7-input v-else :value="dev.ring" type="select" @input="changeRing($event.target.value)">
          <option v-for="ring in rings" :value="ring" :key="ring">{{ ring }}</option>
        </f7-input>
      </f7-list-item>
    </f7-list>
  </f7-page>
</template>
<script>
import assert from 'assert';
import isBefore from 'date-fns/isBefore';
import isEqual from 'date-fns/isEqual';
import {pickBy} from 'lodash-es';
import Debug from 'debug';
import {format, formatRelative} from '../date-fns-wrapper';

import vulnerability from '../vulnerability';
const debug = Debug('page:dev-details');

export default {
  data: function() {
    return {
      ring_changing: false,
    };
  },

  computed: {
    os_version: function() {
      return this.dev.os_version ? this.dev.os_version : this.$t('message.dev_details.os_version_unknown');
    },
    dev_model: function() {
      return (this.dev.certainty === 'low') ?
        this.$t('message.dev_details.unknown_model') :
        this.dev.model;
    },
    dev_manufacturer: function() {
      return (this.dev.certainty === 'low') ?
        this.$t('message.dev_details.unknown_manufacturer') :
        this.dev.manufacturer;
    },
    media_icon: function() {
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
      if (this.dev && this.dev.scans && this.dev.scans.vulnerability) {
        start = this.dev.scans.vulnerability.start || null;
        finish = this.dev.scans.vulnerability.finish || null;
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
      return this.$store.getters.Device_By_UniqID(uniqid);
    },
    rings: function() {
      return this.$store.getters.Rings;
    },
  },
  beforeCreate: function() {
    return this.$store.dispatch('fetchRings');
  },

  methods: {
    timeAbs: function(t) {
      return format(t, 'Pp');
    },
    timeRel: function(t) {
      return formatRelative(t, Date.now());
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

    changeRing: function(wanted_ring) {
      assert(typeof wanted_ring === 'string');
      debug(`Change Ring to ${wanted_ring}`);
      if (this.ring_changing) {
        return;
      }
      this.ring_changing = true;
      this.$store.dispatch('changeRing', {
        deviceUniqID: this.dev.uniqid,
        newRing: wanted_ring,
      }).then(() => {
        this.ring_changing = false;
      }).catch((err) => {
        debug('Change Ring failed', err);
        const txt = `Failed to change security ring for ${this.dev.network_name} to ${wanted_ring}: ${err}`;
        this.ring_changing = false;
        this.$f7.toast.show({
          text: txt,
          closeButton: true,
          destroyOnClose: true,
        });
        this.$f7router.back();
      });
    },
  },
};
</script>
