/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */
export default [
  {
      path: '/',
      component: require('./pages/home.vue'),
  },
  {
      path: '/devices',
      component: require('./pages/devices.vue'),
  },
  {
      path: '/details/',
      component: require('./pages/details.vue'),
  },
  {
      path: '/enroll/',
      component: require('./pages/enroll.vue')
  },
  {
      path: '/malwareWarn/',
      component: require('./pages/malwareWarn.vue')
  },
  {
      path: '/users/',
      component: require('./pages/users.vue'),
  },
  {
      path: '/users/:UUID/',
      component: require('./pages/user_details.vue'),
  },
  {
      path: '/users/:UUID/editor/',
      component: require('./pages/user_editor.vue'),
  },
]
