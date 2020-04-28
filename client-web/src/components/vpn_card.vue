<!--
  COPYRIGHT 2020 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
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
