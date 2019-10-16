<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
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
