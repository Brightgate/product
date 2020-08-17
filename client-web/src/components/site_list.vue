<!--
   Copyright 2019 Brightgate Inc.

   This Source Code Form is subject to the terms of the Mozilla Public
   License, v. 2.0. If a copy of the MPL was not distributed with this
   file, You can obtain one at https://mozilla.org/MPL/2.0/.
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
      <span slot="text">{{ orgNameBySiteID(site.id) }}</span>
    </f7-list-item>
  </f7-list>
</template>

<script>
import Vuex from 'vuex';
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
    // Map various $store elements as computed properties for use in the
    // template.
    ...Vuex.mapGetters([
      'orgNameBySiteID',
      'currentOrg',
    ]),

    orderedSites: function() {
      debug('sites', this.sites);
      const sites = [];
      // Bail if currentOrg isn't set yet
      if (!this.currentOrg) {
        return sites;
      }
      // Copy out to a standard array for sorting
      Object.keys(this.sites).forEach((key) => {
        debug('this.sites[key] = ', key, this.sites[key]);
        debug('orderedSites, currentOrg is', this.currentOrg);
        if (this.sites[key].regInfo.organizationUUID === this.currentOrg.id) {
          sites.push(this.sites[key]);
        }
      });
      const sorted = sites.sort((a, b) => {
        if (a.regInfo.relationship === 'self' && b.regInfo.relationship !== 'self') {
          return -1;
        }
        if (a.regInfo.relationship !== 'self' && b.regInfo.relationship === 'self') {
          return 1;
        }
        const aOrg = this.orgNameBySiteID(a.id);
        const bOrg = this.orgNameBySiteID(b.id);
        debug(`sorting: a:${aOrg} b:${bOrg}`);
        const x = aOrg.localeCompare(bOrg);
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

