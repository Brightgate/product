<!--
  COPYRIGHT 2020 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>

div.download-button-container {
  margin: 8px 16px 8px 8px;
  /* Keep this container snug around its contents */
  display: block;
  width: fit-content;
}

a.download-button {
  margin-bottom: 8px;
  text-transform: none;
}

a.download-button >>> i.icon {
  padding-right: 8px;
}

/* fix minor misalignment on ios */
.ios a.download-button >>> i.icon {
  top: -2px;
}

/*
 * Cause the trademark message to float to the bottom of viewport
 * https://stackoverflow.com/questions/12239166/footer-at-bottom-of-page-or-content-whichever-is-lower
 */
div.content-container {
  display: flex;
  flex-direction: column;
  min-height: 100%;
}
div.content-flex {
  flex: 1;
}
div.trademark {
  font-size: small;
  width: 80%; /* avoid the FAB on this page */
}
</style>
<template>
  <f7-page @page:beforein="onPageBeforeIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.account_wg.title')" sliding />

    <f7-fab color="pink" href="NEW/">
      <f7-icon size="32" ios="f7:plus" md="material:add" />
    </f7-fab>
    <div class="content-container">
      <div class="content-flex">
        <f7-block-title>{{ $t('message.account_wg.download') }} </f7-block-title>
        <f7-block>
          {{ $t('message.account_wg.download_explain') }}

          <div class="download-button-container">
            <!-- ios -->
            <f7-link
              v-if="$f7.device.ios"
              external
              icon-f7="logo_apple"
              icon-size="18"
              target="_blank"
              class="download-button"
              href="https://itunes.apple.com/us/app/wireguard/id1441195189?ls=1&mt=8">
              iOS App Store (iPhone, iPad)
            </f7-link>

            <!-- android -->
            <f7-link
              v-if="$f7.device.android"
              external
              icon-material="android"
              icon-size="18"
              target="_blank"
              class="download-button"
              href="https://play.google.com/store/apps/details?id=com.wireguard.android">
              On Google Play (Android)
            </f7-link>

            <f7-link
              v-if="$f7.device.macos"
              external
              icon-f7="logo_apple"
              icon-size="18"
              target="_blank"
              class="download-button"
              href="https://apps.apple.com/us/app/wireguard/id1451685025?ls=1&mt=12">
              Mac App Store
            </f7-link>

            <f7-link
              v-if="$f7.device.windows"
              external
              icon-f7="logo_windows"
              icon-size="18"
              target="_blank"
              class="download-button"
              href="https://www.wireguard.com/install/">
              Windows 7, 8, 8.1 &amp; 10
            </f7-link>

            <br>
            <f7-link
              external
              icon-material="devices_other"
              icon-size="18"
              target="_blank"
              class="download-button"
              href="https://www.wireguard.com/install/">
              {{ $t('message.account_wg.plat_other') }}
            </f7-link>
          </div>

        </f7-block>

        <f7-block-title>{{ $t('message.account_wg.configs') }} </f7-block-title>
        <template v-if="orderedCfgs.length === 0">
          <f7-block>
            None yet
          </f7-block>
        </template>
        <template v-for="cfg in orderedCfgs">
          <bg-vpn-card
            :key="`${cfg.siteUUID}${cfg.mac}`"
            :site-name="sites[cfg.siteUUID].name"
            :vpn-config="cfg">
            <!-- delete control in the footer -->
            <template slot="controlfooter">
              <span />
              <f7-link
                icon-ios="f7:trash"
                icon-md="material:delete"
                @click="deleteConfig(cfg.siteUUID, cfg.mac, cfg.publicKey)">
                {{ $t('message.account_wg.delete_button') }}
              </f7-link>
            </template>
          </bg-vpn-card>
        </template>
      </div>

      <f7-block class="trademark">
        "WireGuard" is a registered trademark of Jason A. Donenfeld.
      </f7-block>
    </div>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Debug from 'debug';
import siteApi from '../api/site';
import uiutils from '../uiutils';
import BgVpnCard from '../components/vpn_card.vue';

const debug = Debug('page:account_wg');

export default {
  components: {
    'bg-vpn-card': BgVpnCard,
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'myAccountUUID',
      'myAccount',
      'sites',
    ]),

    orderedCfgs: function() {
      let cfgs = [];
      if (this.myAccount.wg && this.myAccount.wg.configs) {
        cfgs = [...this.myAccount.wg.configs];
      }
      cfgs.sort((a, b) => {
        return a.label.localeCompare(b.label);
      });
      return cfgs;
    },
  },

  methods: {
    onPageBeforeIn: async function() {
      debug('onPageBeforeIn: fetching wg');
      this.$store.dispatch('fetchAccountWG', this.myAccountUUID
      ).catch((err) => {
        debug('onPageBeforeIn: fetching wg failed!', err);
      });
    },

    deleteConfig: async function(siteUUID, mac, publicKey) {
      debug('deleteConfig', this.myAccountUUID, siteUUID, mac, publicKey);
      try {
        this.$f7.preloader.show();
        await siteApi.accountWGSiteMacDelete(this.myAccountUUID, siteUUID, mac, publicKey);
      } catch (err) {
        this.$f7.preloader.hide();
        debug(`WG delete config failed`, err);
        uiutils.configChangeErrorHandler(this, err, (err) => {
          return this.$t('message.account_wg.delete_failed', {err: err});
        });
      } finally {
        this.$store.dispatch('fetchAccountWG', this.myAccountUUID);
        this.$f7.preloader.hide();
      }
    },
  },
};
</script>
