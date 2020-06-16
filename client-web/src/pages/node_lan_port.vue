<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
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
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.node_lan_port.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-block />

    <f7-block>
      <f7-row>
        <f7-col width="20">
          <bg-port-label :silkscreen="nic.silkscreen" type="ethernet" />
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

      <f7-list-item :title="$t('message.node_lan_port.port_label')">
        {{ nic.silkscreen }}
      </f7-list-item>

      <f7-list-input
        ref="ringInput"
        :title="$t('message.node_lan_port.port_ring')"
        :label="$t('message.node_lan_port.port_ring')"
        :key="0"
        inline-label
        type="select"
        @change="changeRing($event.target.value)">
        <option
          v-for="ringName in ['standard', 'core', 'devices', 'guest', 'internal']"
          :value="ringName"
          :selected="ringName === nic.ring"
          :key="ringName">
          {{ $t('message.general.rings.' + ringName) }}
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

const debug = Debug('page:node_lan_port');

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

    nic: function() {
      return this.node.nics.find((elem) => elem.name === this.$f7route.params.portID);
    },
  },

  methods: {
    changeRing: async function(newRing) {
      const storeArg = {
        nodeID: this.nodeID,
        portID: this.$f7route.params.portID,
        config: {ring: newRing},
      };

      debug('changeRing', newRing);
      await uiUtils.submitConfigChange(this, 'changeRing (lanport)', 'setNodePortConfig',
        storeArg, (err) => {
          return this.$t('message.node_lan_port.change_ring_err',
            {nic: this.nic.silkscreen, ring: newRing, err: err.message});
        }
      );
    },
  },
};
</script>
