<!--
  COPYRIGHT 2020 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style>
div.help-content {
  max-width: 800px;
}

div.help-content figure {
  text-align: center;
}

div.help-content p.note {
  border-left: solid 4px var(--bg-color-yellow-100);
  border-radius: 4px;
  padding: 0.5em;
  background: var(--bg-color-yellow-10);
  margin-top: 6px;
  margin-bottom: 6px;
}

div.help-content p.note::before {
  font-weight: bold;
  content: "Note: ";
}

div.help-content img {
  margin: 0.5em 0;
  max-width: 100%;  /* prevent horizontal overflow */
  max-height: 60vh; /* 60% of viewport height */
}

div.help-content figure.screenshot img {
  border: 1px solid #bbb;
  box-shadow: var(--f7-elevation-4);
}

</style>
<template>
  <f7-page id="help-page" @page:beforein="onPageBeforeIn" @page:init="onPageBeforeIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="helpTopicName" sliding />

    <!--
        Redeclaring page-content is a hack, because f7 starts the page content
        under the navbar and I couldn't work out how to get something scrolling
        inside of that.  What we really want is something like scroll-padding-top,
        but it isn't universally supported.

        Various polyfills and other enhancements are available in this space for
        future experimentation.
      -->
    <div class="page-content">
      <f7-block v-if="helpfile === ''">
        <center>
          {{ helpTopicName }} is Loading
        </center>
        <p>
          <center>
            <f7-preloader />
          </center>
        </p>
      </f7-block>
      <f7-block class="help-content" v-html="helpfile" /> <!-- eslint-disable-line vue/no-v-html -->
    </div>

  </f7-page>
</template>
<script>
import Vue from 'vue';
import Debug from 'debug';
import axios from 'axios';
const debug = Debug('page:help');

// We support a fixed set of help topics, keyed by a name passed in via the
// router (ex: /help/end_customer_guide/)

// eslint-disable-next-line import/no-commonjs
const endCustomerGuideURL = require('../../../doc/build/end_customer_guide-body.html');
const helpTopics = {
  'end_customer_guide': {
    url: endCustomerGuideURL,
    name: 'Admin Guide',
  },
};

export default {
  data: function() {
    return {
      helpfile: '',
      helpTopicName: '',
    };
  },

  methods: {
    onPageBeforeIn: async function() {
      const helpTopic = this.$f7route.params.helpTopic;
      if (helpTopics[helpTopic] === undefined) {
        this.helpfile = `Internal error: Help not found for topic ${helpTopic}`;
        return;
      }
      this.helpTopicName = helpTopics[helpTopic].name;

      const resp = await axios.get(helpTopics[helpTopic].url);
      this.helpfile = resp.data;

      debug('target help anchor is', this.$f7route.params.anchor);
      if (this.$f7route.params.anchor) {
        const anchor = `#${ this.$f7route.params.anchor}`;
        // Wait for DOM update
        Vue.nextTick(() => {
          debug(`trying to scroll to ${anchor}`);
          const scrollTo = this.Dom7(anchor);
          scrollTo[0].scrollIntoView(true);
        });
      }

      // attach a click handler for <a>'s inside of the help-content div
      this.Dom7('.help-content').on('click', 'a', (e) => {
        debug('link clicked', e.srcElement.href);
        if (e.srcElement.href === '') {
          return;
        }
        const url = new URL(e.srcElement.href);
        debug('url is', url);

        // See if the path seems to be relative to this app
        // if we abandon publicPath in the future this check might
        // need to be relaxed.  Better would be to be able to select
        // by class.  When doing this check, note that firefox doesn't include
        // the trailing /, so we normalize both paths.
        // eslint-disable-next-line camelcase,no-undef
        const pubPath = __webpack_public_path__.replace(/\/$/, '');
        const urlPath = url.pathname.replace(/\/$/, '');
        if (urlPath === pubPath) {
          debug('internal target link clicked', e.srcElement.hash);
          const scrollTo = this.Dom7(e.srcElement.hash);
          scrollTo[0].scrollIntoView(true);
        } else {
          debug('non hash link clicked; trying to open', url);
          const win = window.open(url, '_blank');
          win.focus();
        }
      });
    },
  },
};

</script>
