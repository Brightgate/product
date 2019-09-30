<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>
div.shorter-block {
  margin: 16px 0;
}
</style>
<template>
  <f7-page ptr @ptr:refresh="pullRefresh">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.nodes.title')" sliding />
    <bg-site-breadcrumb :siteid="$f7route.params.siteID" />

    <f7-list class="shorter-block">
      <f7-list-item v-for="(node, nodeID) of nodes"
                    :key="nodeID"
                    :title="node.name || $t('message.nodes.unnamed_hw', {id: nodeID})"
                    :link="`${$f7route.url}${nodeID}/`"
                    media-item>
        <div slot="media">
          <bg-hw-icon :model="node.hwModel" width="48px" height="48px" />
        </div>
        <div slot="subtitle">
          {{ node.role === "gateway" ?
            $t('message.nodes.gateway_role') :
          $t('message.nodes.satellite_role') }}
        </div>
      </f7-list-item>
    </f7-list>

  </f7-page>
</template>
<script>
import Vuex from 'vuex';
import BGSiteBreadcrumb from '../components/site_breadcrumb.vue';
import BGHWIcon from '../components/hw_icon.vue';

export default {
  components: {
    'bg-site-breadcrumb': BGSiteBreadcrumb,
    'bg-hw-icon': BGHWIcon,
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...Vuex.mapGetters([
      'nodes',
    ]),
  },

  methods: {
    pullRefresh: async function(event, done) {
      try {
        await this.$store.dispatch('fetchNodes');
      } finally {
        done();
      }
    },
  },
};
</script>
