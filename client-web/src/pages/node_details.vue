<!--
  COPYRIGHT 2020 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>
h2.model-name {
  margin-block-end: 0.1em;
}

div.topmost-block {
  margin: 32px 0 16px 0;
}
div.shorter-block {
  margin: 16px 0;
}

img.glamour {
  display: block;
  width: 90%;
  max-width: 400px;
  margin-left: 1em;
  margin-right: auto;
}
</style>
<template>
  <f7-page ptr @ptr:refresh="pullRefresh">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.node_details.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-block class="topmost-block">
      <img v-if="node.hwModel === 'model100'" class="glamour" src="img/model100-glamour.png">
      <img v-else-if="node.hwModel === 'rpi3'" class="glamour" src="img/rpi3-glamour.png">

      <h2 v-if="node.hwModel === 'model100'" class="model-name">Brightgate Model 100</h2>
      <h2 v-else-if="node.hwModel === 'rpi3'" class="model-name">Raspberry Pi 3</h2>
      <h2 v-else class="model-name">{{ node.hwModel }}</h2>
    </f7-block>

    <f7-list class="shorter-block">
      <f7-list-item
        :class="!node.serialNumber ? 'disabled' : ''"
        :header="$t('message.node_details.serial_number')">
        <span slot="title">
          <tt>{{ node.serialNumber || $t('message.node_details.sn_none') }}</tt>
        </span>
      </f7-list-item>
      <f7-list-item :header="$t('message.node_details.name')">
        <span slot="title">
          {{ node.name || $t('message.node_details.unnamed_hw', {id: nodeID}) }}
        </span>
        <f7-link slot="after" icon-material="edit" @click="nodeNameDialog" />
      </f7-list-item>
      <f7-list-item :header="$t('message.node_details.role')">
        <div slot="title">
          {{ node.role === "gateway" ?
            $t('message.node_details.gateway') :
            $t('message.node_details.satellite') }}
        </div>
      </f7-list-item>
    </f7-list>

    <!-- Radios -->
    <f7-block-title>
      {{ $t('message.node_details.radios') }}
    </f7-block-title>
    <f7-list class="shorter-block">
      <f7-list-item v-for="nic in sortedNicsByKind('wireless')"
                    :key="nic.name"
                    :title="$t('message.node_details.wifi_radio', {silkscreen: nic.silkscreen})"
                    :link="nic.wifiInfo ? `${$f7route.url}radios/${nic.name}/` : undefined"
                    chevron-center media-item>
        <bg-port-label slot="media" :silkscreen="nic.silkscreen" type="wifi" />

        <div slot="subtitle">
          <div v-if="nic.wifiInfo">
            {{
              $t('message.node_details.wifi_details', {
                band: nic.wifiInfo.activeBand,
                channel: nic.wifiInfo.activeChannel,
                width: nic.wifiInfo.activeWidth,
              })
            }}
          </div>
          {{ nic.macaddr }}
        </div>
      </f7-list-item>
    </f7-list>

    <f7-block-title>
      {{ $t('message.node_details.ports') }}
    </f7-block-title>
    <f7-list class="shorter-block">
      <!-- WAN Ports -->
      <f7-list-item v-for="nic in sortedNicsByKind('wired:uplink')"
                    :key="nic.name"
                    :title="$t('message.node_details.wan_port')"
                    media-item>
        <bg-port-label slot="media" silkscreen="wan" type="ethernet" />

        <div slot="subtitle">
          {{ nic.macaddr }}
        </div>
      </f7-list-item>

      <!-- LAN Ports -->
      <f7-list-item v-for="nic in sortedNicsByKind('wired:lan')"
                    :key="nic.name"
                    :title="$t('message.node_details.lan_port', {silkscreen: nic.silkscreen})"
                    :link="`${$f7route.url}lanports/${nic.name}/`"
                    chevron-center media-item>
        <bg-port-label slot="media" :silkscreen="nic.silkscreen" type="ethernet" />

        <div slot="subtitle">
          <span v-if="nic.name === 'wan'">
            {{ nic.macaddr }} <br>
          </span>
          Trust group: {{ $te('message.general.rings.' + nic.ring) ? $t('message.general.rings.' + nic.ring) : nic.ring }}
        </div>
      </f7-list-item>
    </f7-list>

  </f7-page>
</template>
<script>
import assert from 'assert';
import Vuex from 'vuex';
import Debug from 'debug';
import uiUtils from '../uiutils';
import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';
import BGPortLabel from '../components/port_label.vue';

const debug = Debug('page:node_details');

export default {
  components: {
    'bg-site-breadcrumb': BGSiteBreadcrumb,
    'bg-port-label': BGPortLabel,
  },

  data: function() {
    return {
    };
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
  },

  methods: {
    pullRefresh: async function(done) {
      try {
        await this.$store.dispatch('fetchNodes');
      } finally {
        done();
      }
    },

    sortedNicsByKind: function(kind) {
      assert.equal(typeof kind, 'string');
      const nics = this.node.nics || [];
      const sortedNics = nics.filter((x) => x.kind === kind);
      sortedNics.sort((x, y) => {return x.name.localeCompare(y.name);});
      return sortedNics;
    },

    nodeNameDialog: async function() {
      debug('nodeNameDialog');
      let newName;
      try {
        const title = this.$t('message.node_details.rename_title');
        const text = this.$t('message.node_details.rename_text');
        newName = await new Promise((resolve, reject) => {
          this.$f7.dialog.prompt(text, title, resolve, reject, this.node.name);
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

      // Could do additional name validation in the future
      if (newName === '') {
        return;
      }

      debug('nodeNameDialog got new name', newName);
      const storeArg = {
        nodeID: this.nodeID,
        name: newName,
      };
      await uiUtils.submitConfigChange(this, 'changeNodeName', 'setNodeName',
        storeArg, (err) => {
          return this.$t('message.node_details.change_name_err',
            {name: newName, err: err});
        }
      );
    },
  },
};
</script>
