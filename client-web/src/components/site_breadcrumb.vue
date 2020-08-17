<!--
   Copyright 2019 Brightgate Inc.

   This Source Code Form is subject to the terms of the Mozilla Public
   License, v. 2.0. If a copy of the MPL was not distributed with this
   file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


<!--
  This component renders markup representing breadcrumbs indicating
  organization > site.
-->

<style scoped>

div.breadcrumb {
  position: relative;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  color: rgba(0, 0, 0, 0.54);
  padding: 8px 16px 4px 16px;
  line-height: 16px;
  font-weight: 500;
  border-bottom: 1px solid rgba(0, 0, 0, 0.25);
}

</style>

<template>
  <div v-if="appMode === appDefs.APPMODE_CLOUD" class="breadcrumb">
    <!--
      Note non-breaking vs breaking spaces; the effect is as follows:
        | Dunder Mifflin > Scranton Office   |

        | Dunder Mifflin       |
        | > Scranton Office    |

        | Dunder... |
        | > Scra... |
    -->
    {{ orgNameBySiteID(site.id) | nbsp }} &gt;&nbsp;{{ site.regInfo.name | nbsp }}
  </div>
</template>

<script>
import Vuex from 'vuex';
import appDefs from '../app_defs';

export default {
  name: 'BgSiteBreadcrumb',

  filters: {
    // replace space with ASCII non breaking space
    nbsp: function(value) {
      return value.replace(/ /g, '\xA0');
    },
  },

  props: {
    siteid: {
      type: String,
      required: true,
    },
  },

  data: function() {
    return {
      appDefs: appDefs,
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...Vuex.mapGetters([
      'appMode',
      'orgNameBySiteID',
    ]),

    site: function() {
      return this.$store.getters.siteByID(this.siteid);
    },
  },
};
</script>

