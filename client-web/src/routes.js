export default [
  {
      path: '/about/',
      component: require('./pages/about.vue')
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
      path: '/form/',
      component: require('./pages/form.vue')
  },
  {
      path: '/dynamic-route/blog/:blogId/post/:postId/',
      component: require('./pages/dynamic-route.vue')
  }
]
