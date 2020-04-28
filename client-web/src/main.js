/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

import Vue from 'vue';
import VueI18n from 'vue-i18n';

import Framework7 from 'framework7/framework7-lite.esm.bundle.js';
import 'framework7/css/framework7.bundle.min.css';

import Framework7Vue, {
  f7Accordion,
  f7AccordionContent,
  f7Block,
  f7BlockTitle,
  f7Button,
  f7Card,
  f7CardContent,
  f7CardFooter,
  f7CardHeader,
  f7Checkbox,
  f7Chip,
  f7Col,
  f7Fab,
  f7Icon,
  f7Input,
  f7Link,
  f7List,
  f7ListGroup,
  f7ListInput,
  f7ListItem,
  f7Navbar,
  f7NavLeft,
  f7NavRight,
  f7NavTitle,
  f7Page,
  f7Panel,
  f7Popover,
  f7Popup,
  f7Preloader,
  f7Row,
  f7Sheet,
  f7Swiper,
  f7SwiperSlide,
  f7Toggle,
  f7View,
} from 'framework7-vue';

import BrowserLocale from 'browser-locale';

// Icons and App Custom Styles
import 'framework7-icons';
import 'material-design-icons/iconfont/material-icons.css';
import './css/app.css';

import App from './app.vue';
// Our store (VueX) implementation
import {store, setStoreI18n} from './store';

import './registerServiceWorker';

// Our messages and current locale
import messages from './i18n';

import F7Debug from './f7-debug';

function appInit() {
  Vue.use(VueI18n);
  Framework7.use(Framework7Vue);
  Framework7.use(F7Debug);

  const comps = [
    f7Accordion,
    f7AccordionContent,
    f7Block,
    f7BlockTitle,
    f7Button,
    f7Card,
    f7CardContent,
    f7CardFooter,
    f7CardHeader,
    f7Checkbox,
    f7Chip,
    f7Col,
    f7Fab,
    f7Icon,
    f7Input,
    f7Link,
    f7List,
    f7ListGroup,
    f7ListInput,
    f7ListItem,
    f7Navbar,
    f7NavLeft,
    f7NavRight,
    f7NavTitle,
    f7Page,
    f7Panel,
    f7Popover,
    f7Popup,
    f7Preloader,
    f7Row,
    f7Sheet,
    f7Swiper,
    f7SwiperSlide,
    f7Toggle,
    f7View,
  ];

  comps.forEach((m) => {
    Vue.component(m.name, m);
  });

  Vue.config.productionTip = false;

  const locale = BrowserLocale().substring(0, 2);
  window.__b10e_locale__ = BrowserLocale().toLowerCase(); // eslint-disable-line camelcase
  const i18n = new VueI18n({
    fallbackLocale: 'en',
    locale,
    messages,
  });
  setStoreI18n(i18n);

  // Init App
  return new Vue({
    i18n,
    el: '#app',
    render: (h) => h(App),
    store,
  });
}

// This logic (storing window.__b10e_app__) works around a problem encountered
// with the app being double initialized when using with the webpack dev server
// and Hot-Module-Reloading.
if (!window.__b10e_app__) {
  window.__b10e_app__ = appInit(); // eslint-disable-line camelcase
}
