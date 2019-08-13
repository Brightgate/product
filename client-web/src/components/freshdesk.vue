<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
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
