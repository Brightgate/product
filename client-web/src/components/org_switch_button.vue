<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->

<style scoped>
.md .org-switch-button {
  margin: 32px 16px;
  border-radius: 4px;
  text-transform: none !important;
  min-width: 64px;
  text-align: left;
  color: #2196f3 !important;
}

.ios .org-switch-button {
  margin: 16px 0px;
  padding-top: 8px;
  padding-left: 16px;
  height: 44px;
  text-transform: none !important;
  min-width: 64px;
  min-height: 32px;
  text-align: left;
  color: #2196f3 !important;
}

</style>

<template>
  <f7-button fill raised color="white" popup-open=".org-switch-popup" class="org-switch-button">
    <span>
      <f7-icon material="business" />&nbsp;{{ title }}<f7-icon class=".org-switch-arrow" material="arrow_drop_down" />
    </span>
  </f7-button>
</template>

<script>
export default {
  name: 'BgOrgSwitchButton',

  props: {
    title: {
      type: String,
      required: true,
    },
  },
};
</script>
