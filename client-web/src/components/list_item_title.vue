<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->

<!--
  This component renders markup for an f7-list-item's title; use this when
  the title needs a tooltip.

  Properties:
    - tip: tooltip text
    - title: the title text

  The default slot can also be used to pass the title, as
  <bg-list-item-title>Hello</bg-list-item-title>

-->
<style>
.bg-list-item-title-tooltip {
  max-width: 80vw;
  background: var(--bg-color-blue-70);
}
/* prevent excessive width on desktop */
.device-desktop .bg-list-item-title-tooltip {
  max-width: 40vw;
}
</style>

<style scoped>
.tip-link {
  border-bottom: 1px dotted grey;
  user-select: none;
}

</style>

<template>
  <span>
    <span
      ref="title"
      :class="{ 'tip-link': !!tip }">
      <!-- default slot -->
      <slot>{{ title }}</slot>
    </span>
  </span>
</template>

<script>
export default {
  name: 'BgListItemTitle',

  props: {
    tip: {
      type: String,
      required: false,
      default: null,
    },
    title: {
      type: String,
      required: false,
      default: '',
    },
  },

  data: function() {
    return {
      f7tooltip: null,
    };
  },

  mounted: function() {
    if (!this.tip) {
      return;
    }
    const titleRef = this.$refs.title;
    this.f7tooltip = this.$f7.tooltip.create({
      targetEl: titleRef,
      text: this.tip,
      cssClass: 'bg-list-item-title-tooltip',
    });
  },

  beforeDestroy: function() {
    if (this.f7tooltip) {
      this.$f7.tooltip.destroy(this.f7tooltip);
      this.f7tooltip = null;
    }
  },
};
</script>
