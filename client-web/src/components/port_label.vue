<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->

<style scoped>

/*
 * The container gets position: relative; so that absolute positioned items
 * inside work properly.
 */
div.content-container {
  display: inline-block;
  position: relative;
  width: 64px;
  height: 64px;
}

div.port-label {
  display: inline-block;
}

div.silkscreen {
  display: block;
  height: 32px;
  width: 32px;
  border-radius: 32px;
  background: #ddd;
  text-align: center;
  line-height: 32px;
  z-index: 2;
  background: #ddd;
  position: absolute;
  top: 32px;
  left: 32px;
}

.port-icon {
  display: inline;
  height: 48px !important;
  width: 48px !important;
  color: gray;
  fill: currentColor;
}

div.silkscreen >>> svg.wan {
  margin: 6px;
}
</style>

<template>
  <div class="content-container">
    <div class="port-label">
      <div class="silkscreen">
        <template v-if="silkscreen === 'wan'">
          <img-wan class="wan" />
        </template>
        <template v-else>
          {{ silkscreen }}
        </template>
      </div>
      <img-wifi v-if="type === 'wifi'" class="port-icon type-wifi" />
      <img-ethernet-jack v-if="type === 'ethernet'" class="port-icon type-ethernet" />
    </div>
  </div>
</template>

<script>
import ImgEthernetJack from '../assets/ethernet.svg';
import ImgWifi from '../assets/wifi.svg';
import ImgWan from '../assets/silkscreen-wan.svg';

export default {
  name: 'BgPortLabel',

  components: {
    'img-ethernet-jack': ImgEthernetJack,
    'img-wifi': ImgWifi,
    'img-wan': ImgWan,
  },

  props: {
    silkscreen: {
      type: String,
      required: true,
    },
    type: {
      type: String,
      required: true,
    },
  },
};
</script>
