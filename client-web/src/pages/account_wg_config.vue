<!--
  COPYRIGHT 2020 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page @page:beforein="onPageBeforeIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.account_wg_config.title')" sliding />

    <div v-if="!created" id="vpn-phase1">
      <f7-block>
        {{ $t('message.account_wg_config.create_explain') }}
      </f7-block>
      <f7-list>
        <f7-list-input
          :label="$t('message.account_wg_config.site')"
          required
          type="select"
          @change="(evt) => { siteID = evt.target.value; }">
          <optgroup v-if="wgSites(true).length > 0"
                    :label="$t('message.account_wg_config.enabled_optgroup')">
            <option v-for="site in wgSites(true)"
                    :disabled="!enabledSiteMap[site.id]"
                    :key="site.id"
                    :value="site.id"
                    :selected="site.id == siteID">
              {{ site.name }}
            </option>
          </optgroup>
          <optgroup v-if="wgSites(false).length > 0"
                    :label="$t('message.account_wg_config.disabled_optgroup')">
            <option v-for="site in wgSites(false)"
                    :key="site.id"
                    :value="site.id"
                    :selected="site.id == siteID"
                    disabled>
              {{ site.name }}
            </option>
          </optgroup>
        </f7-list-input>
        <f7-list-input
          id="wg_label"
          :label="$t('message.account_wg_config.name')"
          :placeholder="$t('message.account_wg_config.name_placeholder')"
          pattern="^[a-zA-Z0-9_=+.-]{1,15}$"
          info="1-15 letters, numbers, _=+.- with no spaces"
          error-message="1-15 letters, numbers, _=+.- with no spaces"
          validate
          required
          minlength="1"
          maxlength="15"
          type="text"
          input-id="wg_label_input"
          @change="(evt) => { label = evt.target.value; }" />
      </f7-list>

      <!-- Controls: Cancel, Create -->
      <f7-block>
        <f7-row>
          <f7-col>
            <f7-button :text="$t('message.general.cancel')" outline back />
          </f7-col>
          <f7-col>
            <f7-button
              :text="$t('message.account_wg_config.create_button')"
              fill
              raised
              @click="createConfig" />
          </f7-col>
        </f7-row>
      </f7-block>
    </div>

    <template v-if="created">
      <f7-block>
        {{ $t('message.account_wg_config.success_explain') }}
      </f7-block>
      <bg-vpn-card :site-name="sites[vpnConfig.siteUUID].name" :vpn-config="vpnConfig" download-controls>
        <template slot="controlfooter">
          <f7-button
            :text="$t('message.account_wg_config.download_button')"
            fill
            icon-md="material:cloud_download"
            icon-ios="f7:cloud_download"
            @click="downloadClick" />
          <f7-button
            v-if="$f7.device.desktop"
            :text="$t('message.account_wg_config.qr_scan_button')"
            popup-open=".wg-config-qr-popup"
            fill
            icon-md="f7:qrcode"
            icon-ios="f7:qrcode" />
          <f7-button
            v-else
            :text="$t('message.account_wg_config.qr_scan_button')"
            sheet-open=".wg-config-qr-sheet"
            fill
            icon-md="f7:qrcode"
            icon-ios="f7:qrcode" />
        </template>
      </bg-vpn-card>

      <f7-button
        :text="$t('message.general.done')"
        back />

      <!-- popup for desktop class environments -->
      <f7-popup class="wg-config-qr-popup">
        <f7-block>
          {{ $t('message.account_wg_config.qr_scan_explain') }}
        </f7-block>
        <center>
          <qrcode :value="vpnConfig.confData" :options="{ width: 400, color: {dark: '#002d5cff' } }" />
        </center>
        <f7-button :text="$t('message.general.close')" popup-close />
      </f7-popup>

      <!-- sheet for mobile class environments -->
      <f7-sheet class="wg-config-qr-sheet" style="height:auto;" swipe-to-close backdrop>
        <f7-button :text="$t('message.general.close')" sheet-close />
        <f7-block>
          {{ $t('message.account_wg_config.qr_scan_explain') }}
        </f7-block>
        <center>
          <qrcode :value="vpnConfig.confData" :options="{ color: {dark: '#002d5cff' } }" />
        </center>
      </f7-sheet>
    </template>
  </f7-page>
</template>

<script>
import assert from 'assert';
import vuex from 'vuex';
import Debug from 'debug';
import {saveAs} from 'file-saver';
import base64ArrayBuffer from 'base64-arraybuffer';
import VueQrcode from '@chenfengyuan/vue-qrcode';
import siteApi from '../api/site';
import BgVpnCard from '../components/vpn_card.vue';

const debug = Debug('page:vpn_provision_config');

export default {
  components: {
    'bg-vpn-card': BgVpnCard,
    'qrcode': VueQrcode,
  },

  data: function() {
    return {
      created: false,
      vpnConfig: null,
      siteID: null,
      label: '',
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'currentOrg',
      'myAccountUUID',
      'myAccount',
      'sites',
    ]),

    enabledSiteMap: function() {
      // Bail if currentOrg or wg isn't set yet
      if (!this.currentOrg || !this.myAccount.wg) {
        return {};
      }
      // Convert to a map
      const enabled = {};
      this.myAccount.wg.enabledSites.forEach((siteUU) => {enabled[siteUU] = true;});
      return enabled;
    },
  },

  methods: {
    onPageBeforeIn: function() {
      // We should already have account WG info by the time we are here.
      const wgEnabledSites = this.wgSites(true);
      if (wgEnabledSites.length === 0) {
        this.siteID = null;
      } else {
        this.siteID = wgEnabledSites[0].id;
      }
    },

    wgSites: function(enabled) {
      debug(`wgSites(${enabled}) currentOrg=${this.currentOrg}`);
      assert.equal(typeof enabled, 'boolean');
      const unsorted = [];
      Object.keys(this.sites).forEach((siteUU) => {
        debug('this.sites ', siteUU, this.sites[siteUU]);
        if (this.sites[siteUU].regInfo.organizationUUID === this.currentOrg.id) {
          if (enabled === Boolean(this.enabledSiteMap[siteUU])) {
            unsorted.push(this.sites[siteUU]);
          }
        }
      });
      const sorted = unsorted.sort((a, b) => {
        return a.name.localeCompare(b.name);
      });
      debug('sorted sites', sorted);
      return sorted;
    },

    createConfig: async function() {
      debug('siteID', this.siteID);

      const valid = this.$f7.input.validate('#wg_label');
      debug(`label_input valid=${valid}; this.label=${this.label}`);
      if (!valid || !this.label) {
        // Find the div below the li, and add item-input-with-error-message to
        // it to force its error text to be red.
        const contentItem = this.Dom7('#wg_label').find('div.item-content');
        debug('contentItem is', contentItem);
        contentItem.addClass('item-input-with-error-message');
        return;
      }

      try {
        this.$f7.preloader.show();
        const result = await siteApi.accountWGSiteNewPost(this.myAccountUUID, this.siteID, this.label);
        debug('result is', result);
        this.vpnConfig = result;
      } catch (err) {
        this.$f7.preloader.hide();
        debug('POST failed', err);
        let msg = err.toString();
        if (err.response && err.response.data && err.response.data.message) {
          msg = err.response.data.message;
        }

        this.$f7.toast.show({
          text: this.$t('message.account_wg_config.create_failed', {msg}),
          closeButton: true,
          destroyOnClose: true,
        });
        return;
      }

      this.Dom7('#vpn-phase1').css('position', 'relative');
      const self = this;
      // a simple animation which slides the elements up and off screen
      this.Dom7('#vpn-phase1').animate(
        {
          'top': -1000,
          'opacity': 0.2,
        },
        {
          duration: 600,
          complete: function(elem) {self.created = true; self.$f7.preloader.hide();},
        });
    },

    downloadClick: function() {
      debug('downloadConfBody is', this.vpnConfig.downloadConfBody);
      const bin = base64ArrayBuffer.decode(this.vpnConfig.downloadConfBody);
      debug('length of bin is', bin.byteLength);
      debug('bin is', bin);
      const blob = new Blob([bin], {type: this.vpnConfig.downloadConfContentType});
      saveAs(blob, this.vpnConfig.downloadConfName);
    },

  },
};
</script>
