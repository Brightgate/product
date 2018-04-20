/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */
import Vue from 'vue';
import VueI18n from 'vue-i18n';
import Framework7 from 'framework7';
import Framework7Vue from 'framework7-vue';
import BrowserLocale from 'browser-locale';
import moment from 'moment';
import 'moment/min/locales.min';

import Routes from './routes.js';
import App from './app.vue';

// Our store (VueX) implementation
import store from './store';

// Our messages and current locale
import messages from './i18n';

Vue.use(VueI18n);
Vue.use(Framework7Vue, Framework7);

const locale = BrowserLocale().substring(0, 2);
console.log(`Trying locale ${locale}`);
const i18n = new VueI18n({
  fallbackLocale: 'en',
  locale,
  messages,
});
moment.locale(locale);
console.log(`moment locale: ${moment.locale()}`);

// Init App
new Vue({
  i18n,
  el: '#app',
  store,
  // Init Framework7 by passing parameters here
  framework7: {
    id: 'net.b10e.appliance-admin',
    name: 'Brightgate Appliance Admin Tool',
    theme: 'auto',
    routes: Routes,
  },
  // Register App Component
  components: {
    app: App,
  },
  template: '<app/>',
});
