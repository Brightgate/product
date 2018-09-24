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

import Framework7 from 'framework7/framework7.esm.bundle';
import Framework7Vue, {
  f7Block,
  f7BlockTitle,
  f7Button,
  f7Card,
  f7Col,
  f7Fab,
  f7Icon,
  f7Input,
  f7Label,
  f7Link,
  f7List,
  f7ListGroup,
  f7ListItem,
  f7Navbar,
  f7NavLeft,
  f7NavRight,
  f7NavTitle,
  f7Page,
  f7Preloader,
  f7Row,
  f7Toggle,
  f7View,
} from 'framework7-vue';

import BrowserLocale from 'browser-locale';

/* eslint-disable no-unused-vars */
// Import Icons and App Custom Styles
// This forces webpack to include these assets
import F7Styles from 'framework7/css/framework7.css';
import F7Icons from 'framework7-icons';
import MDIcons from 'material-design-icons';
import AppStyles from './css/app.css';
/* eslint-enable no-unused-vars */

import App from './app.vue';
// Our store (VueX) implementation
import store from './store';

import './registerServiceWorker';

// Our messages and current locale
import messages from './i18n';

Vue.use(VueI18n);
Framework7.use(Framework7Vue);

const comps = [
  f7Block,
  f7BlockTitle,
  f7Button,
  f7Card,
  f7Col,
  f7Fab,
  f7Icon,
  f7Input,
  f7Label,
  f7Link,
  f7List,
  f7ListGroup,
  f7ListItem,
  f7Navbar,
  f7NavLeft,
  f7NavRight,
  f7NavTitle,
  f7Page,
  f7Preloader,
  f7Row,
  f7Toggle,
  f7View,
];

comps.forEach((m) => {
  Vue.component(m.name, m);
});

Vue.config.productionTip = false;

const locale = BrowserLocale().substring(0, 2);
window.__b10e_locale__ = BrowserLocale().toLowerCase();
const i18n = new VueI18n({
  fallbackLocale: 'en',
  locale,
  messages,
});

// Init App
new Vue({
  i18n,
  el: '#app',
  render: (h) => h(App),
  store,
});
