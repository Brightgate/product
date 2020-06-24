<!--
  COPYRIGHT 2020 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page ptr @ptr:refresh="pullRefresh" @page:beforein="onPageBeforeIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.network_wg_editor.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-block-title>{{ $t('message.network_wg_editor.properties') }} </f7-block-title>
    <f7-list>
      <f7-list-item :title="$t('message.network_wg_editor.enabled')">
        <f7-toggle
          slot="after"
          :checked="wg.enabled"
          @change="(evt) => wg.enabled = evt.target.checked" />
      </f7-list-item>
      <f7-list-input
        id="wg_address_input"
        :label="$t('message.network_wg_editor.address')"
        :value="wg.address"
        :error-message-force="showAddrErr"
        type="text"
        info="IP Address (a.b.c.d) or DNS (vpn.example.com)"
        error-message="IP Address (a.b.c.d) or DNS (vpn.example.com)"
        @change="(evt) => wg.address = evt.target.value"
      />
      <f7-list-input
        id="wg_port_input"
        :label="$t('message.network_wg_editor.port')"
        :value="wg.port"
        :error-message-force="showPortErr"
        type="number"
        info="UDP Port in the range 1024-65535; default is 51820"
        error-message="UDP Port in the range 1024-65535; default is 51820"
        @change="(evt) => wg.port = Number(evt.target.value)"
      />
    </f7-list>

    <!-- buttons -->
    <f7-block strong>
      <f7-row>
        <f7-col>
          <f7-button back outline>{{ $t('message.general.cancel') }}</f7-button>
        </f7-col>
        <f7-col>
          <f7-button fill raised @click="saveVPN">{{ $t('message.general.save') }}</f7-button>
        </f7-col>
      </f7-row>
    </f7-block>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Debug from 'debug';

import isFQDN from 'validator/es/lib/isFQDN';
import isIP from 'validator/es/lib/isIP';
import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';
import appDefs from '../app_defs';
import uiUtils from '../uiutils';

const debug = Debug('page:network_wg_editor');

export default {
  components: {
    'bg-site-breadcrumb': BGSiteBreadcrumb,
  },
  data: function() {
    const defaultWG = {
      enabled: false,
      address: '',
      port: 0,
    };

    return {
      wg: defaultWG,
      showPortErr: false,
      showAddrErr: false,
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'networkConfig',
    ]),
  },

  methods: {
    toggleEnabled: function(evt) {
      debug('toggle evt', evt.target.checked);
      this.wg.enabled = evt.target.checked;
    },

    pullRefresh: async function(done) {
      try {
        await this.$store.dispatch('fetchNetworkConfig');
      } finally {
        done();
      }
    },

    saveVPN: async function() {
      debug('saveVPN', this.wg);
      // Step 1: rerun validations
      if (this.wg.port < 1024 || this.wg.port > 65535) {
        this.showPortErr = true;
        return;
      }

      if (!isFQDN(this.wg.address) && !isIP(this.wg.address, 4)) {
        this.showAddrErr = true;
        return;
      }

      if (this.networkConfig.wg) {
        if (this.networkConfig.wg.address !== this.wg.address ||
          Number(this.networkConfig.wg.port) !== Number(this.wg.port)) {
          // Step 2: Confirmation dialog
          try {
            const title = this.$t('message.network_wg_editor.warning_title');
            const text = this.$t('message.network_wg_editor.warning');
            await new Promise((resolve, reject) => {
              this.$f7.dialog.confirm(text, title, resolve, reject);
            });
          } catch (err) {
            debug('dialog box err', err);
            return;
          }
        }
      }
      // Step 3: Submit the change
      const storeArg = {wgConfig: this.wg};
      debug('saveVPN calling submitConfigChange', storeArg);
      try {
        await uiUtils.submitConfigChange(this, 'saveWG', 'updateWGConfig',
          storeArg, (err) => {
            return this.$t('message.network_wg_editor.error_update',
              {err: err.message});
          }
        );
      } catch (err) {
        debug('config change err', err);
        throw err;
      }
      this.$f7router.back();
    },

    onPageBeforeIn: async function() {
      if (!this.networkConfig.wg) {
        await this.$store.dispatch('fetchNetworkConfig').catch(() => {});
      }
      if (this.networkConfig.wg) {
        // only grab subset of properties from wgConfig that we operate on
        this.wg.address = this.networkConfig.wg.address;
        // Fill in default WG port if not set
        this.wg.port = this.networkConfig.wg.port ? this.networkConfig.wg.port : appDefs.WIREGUARD_PORT;
        this.wg.enabled = this.networkConfig.wg.enabled;
        debug('onPageBeforeIn: updated local copy of wg', this.wg);
      }
    },
  },
};
</script>
