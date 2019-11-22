<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <!-- App -->
  <f7-app :params="f7params">

    <!-- Left panel -->
    <f7-panel
      v-if="startRoute === '/'"
      id="bgLeftPanel"
      :visible-breakpoint="960"
      side="left"
      effect="cover"
      @panel:breakpoint="onPanelBreakpoint"
    >
      <f7-view url="/left_panel/" links-view=".view-main" />
    </f7-panel>

    <!-- Main View -->
    <!-- Some notes on this view declaration:
         1. Without stackPages, back navigation doesn't work as you'd expect.
            XXX might be able to revisit this when we eject the page below into
            a separate component.
         2. startRoute dynamically computes the URL to load for this view; this
            enables the alternate landing pages such as the malware warning.
            There's probably a better way to pass this information in besides
            a window level property, but I couldn't figure it out.
    -->
    <f7-view id="main-view" :url="startRoute" :stack-pages="true" :push-state="true" :push-state-on-load="false" :push-state-separator="'#'" :main="true" />

    <bg-login-screen
      v-if="startRoute === '/'"
      id="bgLoginScreen" />

  </f7-app>
</template>

<script>
import {f7App} from 'framework7-vue';
import Debug from 'debug';
import routes from './routes';

import bgLoginScreen from './components/login_screen.vue';
const debug = Debug('page:app.vue');

export default {
  components: {
    'bg-login-screen': bgLoginScreen,
    f7App,
  },

  data: function() {
    return {
      f7params: {
        id: 'net.b10e.appliance-admin',
        name: 'Brightgate Appliance Admin Tool',
        theme: 'auto',
        routes: routes,
        debugger: true,
      },
    };
  },

  computed: {
    startRoute() {
      return window.navigateTo || '/';
    },
  },

  beforeMount: function() {
    this.$store.dispatch('fetchPeriodic');
  },

  methods: {
    // Fired when the visibility due to the "breakpoint" changes-- either because
    // the panel became invisible (size reduces) or the panel becomes visible
    // (size increases).
    onPanelBreakpoint: function() {
      debug('onPanelBreakpoint');
      const pdom = this.Dom7('#bgLeftPanel')[0];
      const visible = pdom.classList.contains('panel-in-breakpoint');
      debug('visible is', visible);
      this.$store.commit('setLeftPanelVisible', visible);
    },
  },
};
</script>
