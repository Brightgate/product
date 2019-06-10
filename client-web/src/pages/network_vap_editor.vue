<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->

<style scoped>
h1 { margin-block-end: 0.1em; }
div.explainer { color: gray; margin-top: 1em; }

span.pw-toggle {
  margin-right: 16px;
}

</style>
<template>
  <f7-page @page:beforein="onPageBeforeIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.network_vap_editor.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-block-title>{{ $t('message.network_vap_editor.titles.' + vapName) }}</f7-block-title>

    <f7-block>
      <div class="explainer">
        {{ $t('message.network_vap.descriptions.' + vapName) }}
      </div>
    </f7-block>

    <f7-list no-hairlines>
      <!-- ssid input -->
      <f7-list-input
        :error-message="validateSSIDErr"
        :error-message-force="!!validateSSIDErr"
        :value="vapSSID"
        :label="$t('message.network_vap_editor.ssid')"
        type="text"
        @input="onSSIDInput" />

      <!-- passphrase input -->
      <f7-list-input
        v-if="hasPassphrase"
        :type="passphraseVisible ? 'text' : 'password'"
        :error-message="validatePassphraseErr"
        :error-message-force="!!validatePassphraseErr"
        :value="vapPassphrase"
        :label="$t('message.network_vap_editor.passphrase')"
        autocomplete="new-password"
        @input="onPassphraseInput">
        <div slot="content-end">
          <span class="pw-toggle">
            <f7-link icon-only icon-f7="eye_fill" @click="togglePassphrase" />
          </span>
        </div>
      </f7-list-input>
    </f7-list>

    <!-- buttons -->
    <f7-block strong>
      <f7-row>
        <f7-col>
          <f7-button back outline>{{ $t('message.general.cancel') }}</f7-button>
        </f7-col>
        <f7-col>
          <f7-button fill raised @click="saveVAP">{{ $t('message.general.save') }}</f7-button>
        </f7-col>
      </f7-row>
    </f7-block>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Debug from 'debug';
import appDefs from '../app_defs';
import siteApi from '../api/site';
import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';
import 'fast-text-encoding'; // polyfill for TextEncoder

const debug = Debug('page:network_vap_editor');

export default {
  components: {
    'bg-site-breadcrumb': BGSiteBreadcrumb,
  },

  data: function() {
    return {
      appDefs: appDefs,
      passphraseVisible: false,
      hasPassphrase: false,
      validateSSIDErr: '',
      validatePassphraseErr: '',
      vapSSID: '',
      vapPassphrase: '',
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'appMode',
      'siteAdmin',
    ]),

    vap: function() {
      const vapName = this.$f7route.params.vapName;
      return this.$store.getters.vaps[vapName];
    },

    vapName: function() {
      return this.$f7route.params.vapName;
    },
  },

  methods: {
    togglePassphrase: function() {
      this.passphraseVisible = !this.passphraseVisible;
    },

    saveVAP: async function() {
      // Step 1: rerun validations
      this.validateSSID();
      this.validatePassphrase();
      if (this.validateSSIDErr || this.validatePassphraseErr) {
        return;
      }

      // Step 2: Confirmation dialog on local appliance
      let p = Promise.resolve();
      if (this.$store.getters.appMode === appDefs.APPMODE_LOCAL) {
        p = new Promise((resolve, reject) => {
          this.$f7.dialog.confirm(
            this.$t('message.network_vap_editor.warning'),
            this.$t('message.network_vap_editor.warning_title'),
            resolve, reject);
        });
      }
      try {
        await p;
      } catch (err) {
        return;
      }

      // Step 3: Submit the change
      const siteID = this.$f7route.params.siteID;
      const vapName = this.$f7route.params.vapName;
      const vapConfig = {
        ssid: this.vapSSID,
      };
      if (this.hasPassphrase) {
        vapConfig.passphrase = this.vapPassphrase;
      }

      try {
        await siteApi.siteVAPPost(siteID, vapName, vapConfig);
        await this.$store.dispatch('fetchVAPs');
        this.$f7router.back();
      } catch (err) {
        debug('err saving', err);
        this.$f7.toast.show({
          text: err.toString(),
          closeButton: true,
          destroyOnClose: true,
        });
      }
    },

    onSSIDInput: function(evt) {
      this.vapSSID = evt.target.value;
      this.validateSSID();
    },

    onPassphraseInput: function(evt) {
      this.vapPassphrase = evt.target.value;
      this.validatePassphrase();
    },

    // This is a twin to the check in configd; returns a string
    // describing validation problems, or '' if none.
    validateSSID: function() {
      const ssid = this.vapSSID;
      const octets = (new TextEncoder().encode(ssid));
      this.validateSSIDErr = '';
      if (octets.length === 0) {
        this.validateSSIDErr = this.$t('message.network_vap_editor.valid_ssid.not_set');
      }
      if (octets.length > 32) {
        this.validateSSIDErr = this.$t('message.network_vap_editor.valid_ssid.too_long', {len: octets.length});
      }
      for (const o of octets) {
        if (o < 0x20 || o > 0x7e) {
          this.validateSSIDErr = this.$t('message.network_vap_editor.valid_ssid.invalid');
        }
      }
    },

    // This is a twin to the check in configd
    validatePassphrase: function() {
      this.validatePassphraseErr = '';
      if (!this.hasPassphrase) {
        return;
      }
      const pp = this.vapPassphrase;
      if (pp.length === 64) {
        if (pp.match(/^[a-fA-F0-9]+$/) === null) {
          this.validatePassphraseErr = this.$t('message.network_vap_editor.valid_pp.hex');
        }
        return;
      }
      const octets = (new TextEncoder().encode(pp));
      if (octets.length < 8 || octets.length > 63) {
        this.validatePassphraseErr = this.$t('message.network_vap_editor.valid_pp.len');
      }
      for (const o of octets) {
        if (o < 0x20 || o > 0x7e) {
          this.validatePassphraseErr = this.$t('message.network_vap_editor.valid_pp.invalid');
        }
      }
    },

    onPageBeforeIn: function() {
      const vapName = this.$f7route.params.vapName;
      const vap = this.$store.getters.vaps[vapName];
      debug('onPageBeforeIn', vap);

      this.vapSSID = vap.ssid;
      if (vapName === 'psk' || vapName === 'guest') {
        this.hasPassphrase = true;
        this.passphraseVisible = false;
        this.vapPassphrase= vap.passphrase;
      }
    },
  },
};
</script>
