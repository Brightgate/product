/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */
/* eslint-disable import/no-commonjs */
module.exports = {
  baseUrl: '/client-web/',
  pages: {
    index: {
      entry: 'src/main.js',
      template: 'public/index.html',
      filename: 'index.html',
      title: 'Brightgate Administration',
    },
    malwareWarn: {
      entry: 'src/main.js',
      template: 'public/malwareWarn.html',
      filename: 'malwareWarn.html',
      title: 'Malware Warning',
    },
  },
  pluginOptions: {
    webpackBundleAnalyzer: {
      openAnalyzer: false,
    },
  },
  chainWebpack: (config) => {
    // Fiddle with the webpack "chain".  The idea is to add an instance of the
    // Preload Plugin which is smart enough to generate preloads for our icon
    // font.  Without this, the font loading can be a long pole in startup,
    // because it is deferred until very late by the browser. 'preload' will
    // force the loading to start as soon as possible.
    config.plugin('preload-fonts').use(require('@vue/preload-webpack-plugin'), [
      {
        rel: 'preload',
        include: 'allAssets',
        as: 'font',
        fileWhitelist: [/\.woff2$/],
      },
    ]);
  },
  lintOnSave: false,
};
