# client-web

## Project setup
```
npm install
```

You should also re-run this after `git pull`.

### Compiles and hot-reloads for development
```
npm run serve
```

The development server is ideal during prototyping and working through user
interface problems, as it cuts the edit-compile-reload cycle down to less then
5 seconds.  You will probably need to augment this with some additional
configuration.  Webpack's dev HTTP server can be configured to proxy API
requests to a backend server of your choice.  An example of how this might look
is as follows, but the exact backend server may vary depending on your
environment.

```
--- a/client-web/vue.config.js
+++ b/client-web/vue.config.js
@@ -29,6 +29,18 @@ module.exports = {
       openAnalyzer: false,
     },
   },
+  devServer: {
+    proxy: {
+      '/api': {
+        target: 'http://localhost:9090',
+        changeOrigin: true,
+      },
+      '/auth': {
+        target: 'http://localhost:9090',
+        changeOrigin: true,
+      },
+    },
+  },
   chainWebpack: (config) => {
     // Fiddle with the webpack "chain".  The idea is to add an instance of the
     // Preload Plugin which is smart enough to generate preloads for our icon
```

### Compiles and minifies for production
```
npm run build
```

### Lint files
```
npm run lint
```

### Lint and fix files
```
npm run lint-fix
```

(This works amazingly well)

## Debugging and Logging

- Install [Vue Devtools](https://github.com/vuejs/vue-devtools)
- Install [Chrome Lighthouse](https://developers.google.com/web/tools/lighthouse/)

For in-browser debug logging, we use [debug](https://www.npmjs.com/package/debug).  See the webpage for more information, but to get started, in your browser console, type:

```
localStorage.debug='*'
```

(Note that this isn't an instant-enable; you have to reload the app).

## Pre-integration testing

For now, we manually test using latest Chrome, Firefox, Safari, and Edge.  No automated testing yet.
