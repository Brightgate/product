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
requests to a backend server of your choice, and we have created a default
version of that, activated by an environment variable:

```
APISERVER=http://localhost:9090 npm run serve
```

This creates a clause in the webpack.devServer configuration which looks like this:
```
  devServer: {
    ...
    proxy: {
      '/api': {
        target: 'http://localhost:9090',
        changeOrigin: true,
      },
      '/auth': {
        target: 'http://localhost:9090',
        changeOrigin: true,
      },
    },
  }
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

## Test Tools

To enable our test tools, including the mock-data facility, open the browser console and type:

```
localStorage.testTools='yes'
```

(Note that this isn't an instant-enable; you have to reload the app).  To
enable mock data:
- Reload the app after enabling testTools
- Close the login window (top right corner X button)
- Visit the hamburger menu, then select 'test tools'
- Turn on "Mock API Responses", "Simulate being Logged In" and "Force Cloud Mode".
- Then go to hamburger menu and select "Dunder Mifflin" to return home.

## Pre-integration testing

For now, we manually test using latest Chrome, Firefox, Safari, and Edge.  No automated testing yet.
