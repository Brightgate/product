<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
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
