/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
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
