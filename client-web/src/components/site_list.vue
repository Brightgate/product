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
  <f7-list>
    <f7-list-item
      v-for="site in sites"
      :key="site.id"
      :title="site.name"
      :link="`/sites/${site.id}/`"
      :class="currentSite === site.id ? 'selected' : undefined"
      :badge="currentSite === site.id ? $t('message.site_list.current') : undefined"
      @click="$emit('site-change', site.id)" />
  </f7-list>
</template>

<script>

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
};
</script>
