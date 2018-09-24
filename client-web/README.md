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
