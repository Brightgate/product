<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->

<!--
  This component renders markup for the organization switcher.  It is
  a flex-layout container with left, center, and right children:

   -------------------------------------------------------
   | Icon | Title-Text                            | Icon |
   | 24px | Flexible size                         | 36px |
   -------------------------------------------------------

  Clicking the left icon or the title text returns to the "home" for the
  organization in the UI.  Clicking the rightmost icon pops up the
  organization switcher.

  Be aware that if the layout evolves to be more complex, then the
  techniques in https://css-tricks.com/flexbox-truncated-text/ may be
  needed.

  Properties:
  - title: the text to display in the button area.
-->

<style scoped>
div.org {
  display: flex;
  width: 100%;
  box-sizing: border-box;
  align-items: center;
}

.left {
  flex: 0 0 24px;
}

.center {
  display: block;      /* needed for text-overflow to work */
  flex: 1;             /* consume available space */
  text-align: left;
  overflow: hidden;
  text-transform: none;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.right {
  flex: 0 0 36px;
}

</style>

<template>
  <div class="org">
    <f7-link href="/" class="left" color="gray" icon-material="business" />
    <f7-link href="/" class="center" panel-close>{{ title }}</f7-link>
    <f7-link class="right" icon-material="list" popup-open=".org-switch-popup" />
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
