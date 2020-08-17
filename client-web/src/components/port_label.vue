<!--
   Copyright 2019 Brightgate Inc.

   This Source Code Form is subject to the terms of the Mozilla Public
   License, v. 2.0. If a copy of the MPL was not distributed with this
   file, You can obtain one at https://mozilla.org/MPL/2.0/.
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
  /*
   * Setting 'width' should be unnecessary but seems to be needed for MS Edge
   * to size the SVG properly. 6px-margin + 20px-contents + 6px-margin = 32px,
   * which is the width of the enclosing div.silkscreen.
   */
  width: 20px;
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

