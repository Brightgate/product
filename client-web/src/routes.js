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
  }
]
