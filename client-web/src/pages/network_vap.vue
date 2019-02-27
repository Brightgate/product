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
div.subtitle { font-size: 16pt; }
div.explainer { color: gray; margin-top: 1em; }

span.pw-toggle {
  margin-right: 16px;
}

</style>
<template>
  <f7-page @page:beforein="onPageBeforeIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.network_vap.title')" sliding />

    <f7-block>
      <h1>{{ $t('message.network.names.' + vapName) }}</h1>
      <div class="subtitle">
        <f7-icon material="wifi" size="24" /> {{ vap.ssid }}
      </div>
      <div class="explainer">
        {{ $t('message.network_vap.descriptions.' + vapName) }}
      </div>
    </f7-block>

    <f7-block-title>{{ $t('message.network_vap.properties') }} </f7-block-title>
    <f7-list>
      <f7-list-item :title="$t('message.network_vap.key_mgmt')">
        {{ vap.keyMgmt }}
      </f7-list-item>
      <f7-list-item :title="$t('message.network_vap.default_tg')">
        {{ vap.defaultRing }}
      </f7-list-item>

      <!-- PSK specific password input -->
      <f7-list-item v-if="vapName === appDefs.VAP_PSK || vapName === appDefs.VAP_GUEST"
                    title="Passphrase">
        {{ passphraseDisplayed }}
        <div slot="content-end">
          <span class="pw-toggle">
            <f7-link icon-only icon-f7="eye_fill" @click="togglePassphrase" />
          </span>
        </div>
      </f7-list-item>

      <f7-list-item
        v-if="appMode === appDefs.APPMODE_CLOUD && vapName === appDefs.VAP_GUEST"
        :title="$t('message.site_controls.enroll_guest')"
        :link="`/sites/${$f7route.params.siteID}/enroll_guest/`" />

    </f7-list>

    <f7-block-title>{{ $t('message.network_vap.ring_config') }}</f7-block-title>
    <f7-list>
      <f7-list-item v-for="(ring, ringName) in rings" :key="ringName" :title="ringName" accordion-item>
        <f7-accordion-content>
          <f7-list inset>
            <f7-list-item title="Subnet">{{ ring.subnet }}</f7-list-item>
            <f7-list-item title="Lease Duration">
              {{ leaseDurationMinutes(ring.leaseDuration) }}
              {{ ring.leaseDuration >= 120 ? '(' + leaseDuration(ring.leaseDuration) + ')' : "" }}
            </f7-list-item>
          </f7-list>
        </f7-accordion-content>
      </f7-list-item>
    </f7-list>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Debug from 'debug';
import {f7AccordionContent} from 'framework7-vue';
import {pickBy} from 'lodash-es';
import {formatDistanceStrict} from '../date-fns-wrapper';
import appDefs from '../app_defs';

const debug = Debug('page:network_vap');

export default {
  components: {f7AccordionContent},
  data: function() {
    return {
      appDefs: appDefs,
      passphraseVisible: false,
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'appMode',
    ]),

    vap: function() {
      const vapName = this.$f7route.params.vapName;
      return this.$store.getters.vaps[vapName];
    },

    vapName: function() {
      return this.$f7route.params.vapName;
    },

    passphraseDisplayed: function() {
      const vap = this.$store.getters.vaps[this.$f7route.params.vapName];
      let val = '';
      if (this.passphraseVisible) {
        val = vap.passphrase;
      } else {
        let pwlen = 8;
        if (vap.passphrase) {
          pwlen = vap.passphrase.length;
        }
        val = '•'.repeat(pwlen);
      }
      return val;
    },

    rings: function() {
      const vapName = this.$f7route.params.vapName;
      const vap = this.$store.getters.vaps[vapName];
      const rings = this.$store.getters.rings;
      debug('rings: vap, rings', vap, rings);
      return pickBy(rings, (val, key) => {
        return vap.rings.includes(key);
      });
    },
  },

  methods: {
    togglePassphrase: function() {
      this.passphraseVisible = !this.passphraseVisible;
    },

    onPageBeforeIn: function() {
      const vapName = this.$f7route.params.vapName;
      debug('onPageBeforeIn', this.$store.getters.vaps[vapName]);
      this.passphraseValue = this.$store.getters.vaps[vapName].passphrase;
      this.passphraseVisible = false;
    },

    leaseDurationMinutes: function(minutes) {
      return formatDistanceStrict(minutes * 60 * 1000, 0, {'unit': 'minute'});
    },
    leaseDuration: function(minutes) {
      return formatDistanceStrict(minutes * 60 * 1000, 0);
    },
  },
};
</script>
