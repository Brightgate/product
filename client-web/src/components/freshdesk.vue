<!--
   Copyright 2019 Brightgate Inc.

   This Source Code Form is subject to the terms of the Mozilla Public
   License, v. 2.0. If a copy of the MPL was not distributed with this
   file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


<!--
  This component renders markup for the freshdesk embedded widget.

  Properties: None
-->

<style type="text/css" media="screen, projection">
  /* If we decide to use the non-iframe version of the freshdesk widget:
   * @import url(https://s3.amazonaws.com/assets.freshdesk.com/widget/freshwidget.css);
   */
</style>
<template>
  <iframe id="freshwidget-embedded-form"
          title="Feedback Form"
          class="freshwidget-embedded-form"
          src="https://brightgate.freshdesk.com/widgets/feedback_widget/new?&widgetType=embedded&submitTitle=Request+Support&submitThanks=Thank+you+for+your+support+request!&screenshot=No"
          scrolling="no"
          height="450px"
          width="100%"
          frameborder="0" />
</template>

<script>
import Debug from 'debug';
const debug = Debug('component:freshdesk');

export default {
  name: 'BgFreshdesk',

  beforeMount() {
    debug('beforeMount');
    /*
     * This hook is here as a reminder that if we decide to use the non-iframe
     * version of the freshdesk widget, we need to mix in the freshwidget js:
     *  const script = document.createElement('script');
     *  script.src = 'https://s3.amazonaws.com/assets.freshdesk.com/widget/freshwidget.js';
     *  script.type = 'text/javascript';
     *  document.body.appendChild(script);
     */
  },
};
</script>

