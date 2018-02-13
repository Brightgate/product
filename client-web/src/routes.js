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
      path: '/about/',
      component: require('./pages/about.vue')
  },
  {
      path: '/devices/',
      component: require('./pages/devices.vue')
  },
  {
      path: '/details/',
      component: require('./pages/details.vue')
  },
  {
      path: '/enroll/',
      component: require('./pages/enroll.vue')
  },
  {
      path: '/enrollApple/',
      component: require('./pages/enrollApple.vue')
  },
  {
      path: '/malwareWarn/',
      component: require('./pages/malwareWarn.vue')
  },
  {
      path: '/form/',
      component: require('./pages/form.vue')
  },
  {
      path: '/dynamic-route/blog/:blogId/post/:postId/',
      component: require('./pages/dynamic-route.vue')
  },
  {
      path: '/login/',
      component: require('./pages/login.vue')
  }
]
