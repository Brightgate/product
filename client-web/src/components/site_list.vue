<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->

<!--
  This component renders markup representing a list of sites.

  Properties:
    - sites: an array of sites
    - currentSite: a string representing the site to mark as the
      'current' or 'most recently used' one.
-->

<style scoped>
/* lightly color the background of selected components */
.md li.selected {
  background: #2196f310;
}
.ios li.selected {
  background: #007aff10;
}
</style>
<template>
  <f7-list media-list>
    <f7-list-item
      v-for="site in orderedSites"
      :key="site.id"
      :title="site.name"
      :link="`/sites/${site.id}/`"
      :class="currentSite === site.id ? 'selected' : undefined"
      :badge="currentSite === site.id ? $t('message.site_list.current') : undefined"
      @click="$emit('site-change', site.id)">
      <span slot="text">{{ site._regInfo.organization }}</span>
    </f7-list-item>
  </f7-list>
</template>

<script>
import Debug from 'debug';
const debug = Debug('component:site_list');

export default {
  name: 'BgSiteList',

  props: {
    currentSite: {
      type: String,
      required: true,
    },
    sites: {
      type: Object,
      required: true,
    },
  },

  computed: {
    orderedSites: function() {
      debug('sites', this.sites);
      const sites = [];
      // Copy out to a standard array for sorting
      Object.keys(this.sites).forEach((key) => {
        sites.push(this.sites[key]);
      });
      const sorted = sites.sort((a, b) => {
        if (a._regInfo.relationship === 'self' && b._regInfo.relationship !== 'self') {
          return -1;
        }
        if (a._regInfo.relationship !== 'self' && b._regInfo.relationship === 'self') {
          return 1;
        }
        const x = a._regInfo.organization.localeCompare(b._regInfo.organization);
        if (x !== 0) {
          return x;
        }
        return a.name.localeCompare(b.name);
      });
      debug('sorted sites', sorted);
      return sorted;
    },
  },

};
</script>
