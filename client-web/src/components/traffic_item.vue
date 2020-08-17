<!--
   Copyright 2019 Brightgate Inc.

   This Source Code Form is subject to the terms of the Mozilla Public
   License, v. 2.0. If a copy of the MPL was not distributed with this
   file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


<style scoped>

span.column {
  display: inline-block;
  padding-left: 6px;
}
span.arrow {
  color: #606060;
}
</style>

<template>
  <div>
    <span v-if="typeof(bytesRcvd) === 'number'" class="column">
      <span class="arrow">&#x2B07;</span>{{ bytesRcvd | prettyBytes }}/s
    </span>
    <span v-if="typeof(bytesSent) === 'number'" class="column">
      <span class="arrow">&#x2B06;</span>{{ bytesSent | prettyBytes }}/s
    </span>
  </div>
</template>

<script>
import prettyBytes from 'pretty-bytes';
import appDefs from '../app_defs';

const MetricToPersec = {
  [appDefs.METRIC_INTERVALS.SECOND]: 1,
  [appDefs.METRIC_INTERVALS.MINUTE]: 60,
  [appDefs.METRIC_INTERVALS.HOUR]: 60 * 60,
  [appDefs.METRIC_INTERVALS.DAY]: 60 * 60 * 24,
};

export default {
  name: 'BgTrafficItem',

  filters: {
    prettyBytes: function(value) {
      return prettyBytes(value, true);
    },
  },

  props: {
    metrics: {
      type: Object,
      required: true,
    },
    // appDefs.METRIC_INTERVALS
    interval: {
      type: Number,
      required: true,
    },
  },

  computed: {
    bytesRcvd: function() {
      let res;
      if (this.metrics.bytesRcvd) {
        res = this.metrics.bytesRcvd[this.interval] / MetricToPersec[this.interval];
        // Make sure we don't show 0.33333333...
        if (res < 1) {
          res = Number(res.toFixed(2));
        }
      }
      return res;
    },

    bytesSent: function() {
      let res;
      if (this.metrics.bytesSent) {
        res = this.metrics.bytesSent[this.interval] / MetricToPersec[this.interval];
        // Make sure we don't show 0.33333333...
        if (res < 1) {
          res = Number(res.toFixed(2));
        }
      }
      return res;
    },
  },
};
</script>

