<!--
   Copyright 2019 Brightgate Inc.

   This Source Code Form is subject to the terms of the Mozilla Public
   License, v. 2.0. If a copy of the MPL was not distributed with this
   file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


<style scoped>
div.container {
  display: inline-block;
  margin: 0;
  padding: 0;
}
div.ind {
  display: inline-block;
  width: 20px;
  height: 4px;
  margin: 1px;
}
div.lit {
  background: var(--bg-color-green-60);
}
div.dim {
  background: lightgrey;
}
</style>

<template>
  <div class="container">
    <div v-for="n in 5" :key="n" :class="n <= bars ? 'lit' : 'dim'" class="ind" />
  </div>
</template>

<script>
import uiUtils from '../uiutils';

export default {
  name: 'BgWifiStrength',

  props: {
    dbm: {
      type: Number,
      required: true,
    },
  },

  computed: {
    bars: function() {
      return uiUtils.dBmToStrength(this.dbm);
    },
  },
};
</script>

