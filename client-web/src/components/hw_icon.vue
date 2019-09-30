<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->

<style scoped>
svg.model100 {
  fill: #232e68;
}
svg.rpi {
}
</style>

<template>
  <span>
    <template v-if="model === 'model100'">
      <model100-icon :style="{ width: width, height: height }" class="model100" />
    </template>
    <template v-else-if="model.startsWith('rpi')">
      <rpi-icon :style="{ width: width, height: height }" class="rpi" />
    </template>
  </span>
</template>

<script>
import RpiIcon from '../assets/rpi-icon.svg';
import Model100Icon from '../assets/model100-icon.svg';

export default {
  name: 'BgHwIcon',

  components: {
    'model100-icon': Model100Icon,
    'rpi-icon': RpiIcon,
  },

  props: {
    model: {
      type: String,
      required: true,
    },
    width: {
      type: String,
      required: false,
      default: 'auto',
    },
    height: {
      type: String,
      required: false,
      default: 'auto',
    },
  },
};
</script>
