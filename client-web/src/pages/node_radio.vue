<!--
   Copyright 2019 Brightgate Inc.

   This Source Code Form is subject to the terms of the Mozilla Public
   License, v. 2.0. If a copy of the MPL was not distributed with this
   file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

<style scoped>
h2.model-name {
  margin-block-end: 0.1em;
  margin-top: 0;
}

div.shorter-block {
  margin: 16px 0;
  min-height: 64px;
}

</style>
<template>
  <f7-page>
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.node_radio.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-block />

    <f7-block>
      <f7-row>
        <f7-col width="20">
          <bg-port-label :silkscreen="nic.silkscreen" type="wifi" />
        </f7-col>
        <f7-col width="80">
          <h2 v-if="node.hwModel === 'model100'" class="model-name">Brightgate Model 100</h2>
          <h2 v-else-if="node.hwModel === 'rpi3'" class="model-name">Raspberry Pi 3</h2>
          <h2 v-else class="model-name">{{ node.hwModel }}</h2>
          {{ node.name || $t('message.node_details.unnamed_hw', {id: nodeID}) }}
        </f7-col>
      </f7-row>
    </f7-block>

    <f7-list class="shorter-block">

      <f7-list-item :title="$t('message.node_radio.radio_label')">
        {{ nic.silkscreen }}
      </f7-list-item>
      <f7-list-item :title="$t('message.node_radio.band_label')">
        {{ nic.wifiInfo.configBand === '' ?
          $t('message.node_radio.auto_band', {band: nic.wifiInfo.activeBand}) :
          $t('message.node_radio.config_band', {band: nic.wifiInfo.activeBand})
        }}
      </f7-list-item>

      <f7-list-item v-if="nicMode" :title="$t('message.node_radio.protocol_label')">
        {{ nicMode }}
      </f7-list-item>

      <f7-list-item :title="$t('message.node_radio.width_label')">
        {{ nic.wifiInfo.configWidth === '' ?
          $t('message.node_radio.auto_width', {width: nic.wifiInfo.activeWidth}) :
          $t('message.node_radio.config_width', {width: nic.wifiInfo.activeWidth})
        }}
      </f7-list-item>

      <f7-list-input
        ref="channelInput"
        :key="0"
        :label="$t('message.node_radio.channel_label')"
        :value="nic.wifiInfo.configChannel"
        inline-label
        type="select"
        @change="changeChannel($event.target.value)">
        <option
          key="automatic"
          :value="0">
          {{ nic.wifiInfo.configChannel === 0 ?
            $t('message.node_radio.channel_automatic', {active: nic.wifiInfo.activeChannel}) :
            $t('message.node_radio.channel_automatic_no_channel')
          }}
        </option>
        <option
          v-for="channelName in validChannels"
          :key="channelName"
          :value="channelName"
          :selected="channelName === nic.wifiInfo.configChannel">
          {{ channelName }}
        </option>
      </f7-list-input>

    </f7-list>

  </f7-page>
</template>
<script>
import Vuex from 'vuex';
import Debug from 'debug';
import uiUtils from '../uiutils';
import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';
import BGPortLabel from '../components/port_label.vue';

const debug = Debug('page:node_radio');

// These are cribbed from wificaps.go:
const hiBand20MHz = new Set([36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108,
  112, 116, 120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161, 165]);
const hiBand40MHz = new Set([36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108,
  112, 116, 120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161]);
const hiBand80MHz = new Set([36, 52, 100, 116, 132, 149]);

export default {
  components: {
    'bg-site-breadcrumb': BGSiteBreadcrumb,
    'bg-port-label': BGPortLabel,
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...Vuex.mapGetters([
      'nodes',
    ]),

    nodeID: function() {
      return this.$f7route.params.nodeID;
    },

    node: function() {
      return this.nodes[this.nodeID];
    },

    validChannels: function() {
      let r = [];
      if (this.nic.wifiInfo.activeBand === '2.4GHz') {
        r = [...this.nic.wifiInfo.validLoChannels];
      } else if (this.nic.wifiInfo.activeBand === '5GHz') {
        // ES6 lacks intersect
        const intersect = (set1, set2) => [...set1].filter((x) => set2.has(x));

        const hiChanSet = new Set(this.nic.wifiInfo.validHiChannels);
        const width = this.nic.wifiInfo.activeWidth;
        if (width === '20') {
          r = intersect(hiChanSet, hiBand20MHz);
        } else if (width === '40') {
          r = intersect(hiChanSet, hiBand40MHz);
        } else if (width === '80') {
          r = intersect(hiChanSet, hiBand80MHz);
        } else {
          r = [...hiChanSet];
        }
      }
      // Sort numerically
      r.sort((a, b) => parseInt(a) - parseInt(b));
      debug('validChannels: returning', r);
      return r;
    },

    nic: function() {
      return this.node.nics.find((elem) => elem.name === this.$f7route.params.portID);
    },

    nicMode: function() {
      if (this.nic.wifiInfo) {
        if (this.nic.wifiInfo.activeMode) {
          return `802.11${this.nic.wifiInfo.activeMode}`;
        }
        // Fallback heuristic for systems running older s/w which doesn't
        // report activeMode
        if (this.nic.wifiInfo.activeBand === '2.4GHz') {
          return '802.11b/g/n';
        }
        if (this.nic.wifiInfo.activeBand === '5GHz') {
          return '802.11a';
        }
      }
      return undefined;
    },
  },

  methods: {
    changeChannel: async function(newChannel) {
      const storeArg = {
        nodeID: this.nodeID,
        portID: this.$f7route.params.portID,
        config: {channel: parseInt(newChannel)},
      };

      debug('changeChannel', newChannel);
      await uiUtils.submitConfigChange(this, 'changeChannel', 'setNodePortConfig',
        storeArg, (err) => {
          return this.$t('message.node_radio.change_channel_err',
            {nic: this.nic.silkscreen, channel: newChannel, err: err.message});
        }
      );
      debug('this.nic', this.nic);
    },
  },
};
</script>

