<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->

<style scoped>
div.org {
  display: flex;
  flex: auto;
  box-sizing: border-box;
  justify-content: space-between;
  min-width: 0;
}

/*
 * Setting min-width is crucially important here; see
 * https://css-tricks.com/flexbox-truncated-text/
 */
div.org-left {
  min-width: 0;
  flex-grow: 10;
}
div.org-right {
  min-width: 0;
  width: 48px;
  flex-shrink: 0;
}

div.org-left >>> a {
  border: none; /* remove for f7-4.0 */
  color: #2196f3 !important;
  padding: 0;
  font-size: 16px;
  font-weight: normal;
  text-align: left;
  text-overflow: ellipsis;
  text-transform: none !important;
  white-space: nowrap;
  line-height: 28px;
}

div.org-right >>> a {
  border: none; /* remove for f7-4.0 */
  color: #2196f3 !important;
  padding: 0;
  text-overflow: unset;
  text-transform: none !important;
  width: 48px;
  min-width: 0;
  line-height: 28px;
}
</style>

<template>
  <div class="org">
    <div class="org-left">
      <f7-button icon-color="gray" icon-material="business" panel-close href="/">
        {{ title }}
      </f7-button>
    </div>
    <div class="org-right">
      <f7-button icon-material="list" popup-open=".org-switch-popup" />
    </div>
  </div>
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
