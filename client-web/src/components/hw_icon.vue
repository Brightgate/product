<!--
   Copyright 2019 Brightgate Inc.

   This Source Code Form is subject to the terms of the Mozilla Public
   License, v. 2.0. If a copy of the MPL was not distributed with this
   file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

