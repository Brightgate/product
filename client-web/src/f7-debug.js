/*
 * Copyright 2018 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


/*
 * The majority of code in this file was derived from the F7 plugins-api
 * documentation, https://framework7.io/docs/plugins-api.html.
 */

import Debug from 'debug';
const debug = Debug('f7');
const pageDebug = debug.extend('page-events');
const routerDebug = debug.extend('page-router');

export default {
  name: 'debugger',
  params: {
  },
  create: function() {
  },
  on: {
    init: function() {
      if (debug.enabled) {
        pageDebug('page events enabled');
        routerDebug('router events enabled');
      }
    },
    pageBeforeIn: function(page) {
      if (debug.enabled) {
        pageDebug(`pageBeforeIn: ${page.route.url}`, page);
        routerDebug(`pageBeforeIn: ${page.route.url} route`, page.route);
        routerDebug(`pageBeforeIn: ${page.route.url} history`, page.router.history);
      }
    },
    pageAfterIn: function(page) {
      if (debug.enabled) pageDebug(`pageAfterIn ${page.route.url}`, page);
    },
    pageBeforeOut: function(page) {
      if (debug.enabled) pageDebug(`pageBeforeOut ${page.route.url}`, page);
    },
    pageAfterOut: function(page) {
      if (debug.enabled) pageDebug(`pageAfterOut ${page.route.url}`, page);
    },
    pageInit: function(page) {
      if (debug.enabled) pageDebug(`pageInit: ${page.route.url}`, page);
    },
    pageMounted: function(page) {
      if (debug.enabled) pageDebug(`pageMounted: ${page.route.url}`, page);
    },
    pageBeforeRemove: function(page) {
      if (debug.enabled) pageDebug(`pageBeforeRemove: ${page.route.url}`, page);
    },
  },
};

