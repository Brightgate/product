<!--
   Copyright 2020 Brightgate Inc.

   This Source Code Form is subject to the terms of the Mozilla Public
   License, v. 2.0. If a copy of the MPL was not distributed with this
   file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


<!--
  This component renders markup representing a vpn configuration
  using f7-card components.

  Properties:
    - vpnConfig: object describing the vpn configuration
    - siteName: mapped site name corresponding to vpnConfig.siteUUID
-->

<style scoped>

span.public-key {
  word-wrap: break-word;
  word-break: break-all;
  white-space: normal;
  font-family: monospace;
}

</style>
<template>
  <f7-card>
    <f7-card-header>
      <span><f7-icon material="vpn_key" /> {{ vpnConfig.label }}</span>
    </f7-card-header>
    <f7-card-content>
      <f7-list class="vpn-card-list" no-hairlines no-hairlines-between>
        <f7-list-item
          :title="siteName"
          header="Site" />
        <f7-list-item
          :title="`${vpnConfig.assignedIP}/32`"
          header="Address" />
        <f7-list-item
          header="Public Key">
          <div slot="title"><span class="public-key">{{ vpnConfig.publicKey }}</span></div>
        </f7-list-item>
        <f7-list-item
          v-if="vpnConfig.serverAddress"
          :title="`${vpnConfig.serverAddress}:${vpnConfig.serverPort}`"
          header="Peer Endpoint" />
      </f7-list>
    </f7-card-content>
    <!-- only render footer if footer slot defined -->
    <f7-card-footer v-if="$slots.controlfooter">
      <slot name="controlfooter" />
    </f7-card-footer>
  </f7-card>
</template>

<script>

export default {
  name: 'BgVpnCard',

  props: {
    vpnConfig: {
      type: Object,
      required: true,
    },
    siteName: {
      type: String,
      required: true,
    },
  },
};

</script>

