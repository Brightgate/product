<!--
   Copyright 2019 Brightgate Inc.

   This Source Code Form is subject to the terms of the Mozilla Public
   License, v. 2.0. If a copy of the MPL was not distributed with this
   file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

<template>
  <f7-page>

    <f7-navbar>
      <!-- f7-nav-title doesn't seem to center properly without also
         including left and right. -->
      <f7-nav-left v-if="!leftPanelVisible">
        <f7-link panel-open="left" icon-ios="f7:menu" icon-md="material:menu" />
      </f7-nav-left>

      <f7-nav-title v-if="!leftPanelVisible">
        <img v-if="this.$f7router.app.theme === 'ios'"
             alt="Brightgate"
             style="padding-top:4px"
             src="img/bglogo_navbar_ios.png"
             srcset="img/bglogo_navbar_ios.png,
                  img/bglogo_navbar_ios@2x.png 2x">
        <img v-else
             alt="Brightgate"
             style="padding-top:4px"
             src="img/bglogo_navbar_md.png"
             srcset="img/bglogo_navbar_md.png,
                  img/bglogo_navbar_md@2x.png 2x">
      </f7-nav-title>

      <f7-nav-right v-if="!leftPanelVisible">
        &nbsp;
      </f7-nav-right>
    </f7-navbar>

    <f7-block-title>Alpha/Beta Program Support</f7-block-title>
    <f7-block>
      Brightgate is using Freshdesk to gather feedback and provide assistance
      during its Alpha and Beta programs.  As neccessary, Brightgate may
      share your support request with your managed service provider.
    </f7-block>
    <f7-block>
      <bg-freshdesk />
    </f7-block>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import BGFreshdesk from '../components/freshdesk.vue';

export default {
  components: {
    'bg-freshdesk': BGFreshdesk,
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'leftPanelVisible',
    ]),
  },
};
</script>

