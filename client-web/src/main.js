/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */
// Import Vue
import Vue from 'vue'

// Import VueI18n
import VueI18n from 'vue-i18n'

// Import F7
import Framework7 from 'framework7'

// Import F7 Vue Plugin
import Framework7Vue from 'framework7-vue'

// Import Routes
import Routes from './routes.js'

// Import App Component
import App from './app.vue'

import store from './store'
import { messages, i18n } from './i18n'

// Init F7 Vue Plugin
Vue.use(Framework7Vue)

// Init VueI18n Plugin
Vue.use(VueI18n)

// Init App
var vm = new Vue({
  i18n,
  el: '#app',
  template: '<app/>',
  store,
  // Init Framework7 by passing parameters here
  framework7: {
    root: '#app',
    /* Uncomment to enable Material theme: */
    // material: true,
    routes: Routes,
  },
  methods: {
    onF7Init: function () {
      console.log('f7-init');
    }
  },
  // Register App Component
  components: {
    app: App
  }
});
