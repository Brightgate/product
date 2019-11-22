<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page>

    <f7-navbar>
      <!-- f7-nav-title doesn't seem to center properly without also
         including left and right. -->
      <f7-nav-left v-if="!leftPanelVisible">
        <f7-link panel-open="left" icon-ios="f7:menu" icon-md="material:menu" />
      </f7-nav-left>

      <f7-nav-title v-if="!leftPanelVisible">
        <img v-if="this.$f7router.app.theme === 'ios'"
             alt="Brightgate"
             style="padding-top:4px"
             src="img/bglogo_navbar_ios.png"
             srcset="img/bglogo_navbar_ios.png,
                  img/bglogo_navbar_ios@2x.png 2x">
        <img v-else
             alt="Brightgate"
             style="padding-top:4px"
             src="img/bglogo_navbar_md.png"
             srcset="img/bglogo_navbar_md.png,
                  img/bglogo_navbar_md@2x.png 2x">
      </f7-nav-title>

      <f7-nav-right v-if="!leftPanelVisible">
        &nbsp;
      </f7-nav-right>
    </f7-navbar>

    <div>
      <f7-block-title>{{ $t("message.test_tools.testing") }}</f7-block-title>
      <f7-list>
        <f7-list-group>
          <f7-list-item :title="$t('message.test_tools.mocks_group')" group-title />
          <f7-list-item :title="$t('message.test_tools.enable_mock')">
            <f7-toggle slot="after" :checked="mock" @change="toggleMock" />
          </f7-list-item>
          <f7-list-item :title="$t('message.test_tools.enable_fakelogin')">
            <f7-toggle slot="after" :checked="fakeLogin" @change="toggleFakeLogin" />
          </f7-list-item>
        </f7-list-group>
        <f7-list-group>
          <f7-list-item :title="$t('message.test_tools.appmode_group')" group-title />
          <f7-list-item
            :checked="testAppMode === appDefs.APPMODE_NONE"
            :title="$t('message.test_tools.auto_mode')"
            radio
            name="mode-radio"
            @change="setTestAppMode(appDefs.APPMODE_NONE)"
          />
          <f7-list-item
            :checked="testAppMode === appDefs.APPMODE_CLOUD"
            :title="$t('message.test_tools.cloud_mode')"
            radio
            name="mode-radio"
            @change="setTestAppMode(appDefs.APPMODE_CLOUD)"
          />
          <f7-list-item
            :checked="testAppMode === appDefs.APPMODE_LOCAL"
            :title="$t('message.test_tools.local_mode')"
            radio
            name="mode-radio"
            @change="setTestAppMode(appDefs.APPMODE_LOCAL)"
          />
        </f7-list-group>
      </f7-list>
    </div>
    <f7-block-title>Actions</f7-block-title>
    <f7-block>
      <f7-button fill @click="$f7.loginScreen.open('#bgLoginScreen')">Open Login Screen</f7-button>
    </f7-block>
    <f7-block>
      <f7-button fill popup-open=".color-popup">Colors</f7-button>
    </f7-block>

    <f7-popup class="color-popup">
      <f7-page>
        <f7-block>
          <f7-button fill popup-close=".color-popup">Close</f7-button>
        </f7-block>
        <f7-block-title>Theme Colors</f7-block-title>
        <f7-block>
          <f7-row v-for="theme in ['red', 'blue', 'green', 'yellow', 'orange']" :key="theme" :class="'color-' + theme">
            <f7-col>
              <span style="color: var(--f7-theme-color)">{{ theme }}</span>
            </f7-col>
            <f7-col style="background: var(--f7-theme-color)">
              <span style="color: white">{{ theme }}</span>
            </f7-col>
            <f7-col style="background: var(--f7-theme-color)">
              <span style="color: black">{{ theme }}</span>
            </f7-col>
          </f7-row>
        </f7-block>
        <hr>
        <f7-block-title>CSS Rules</f7-block-title>
        <f7-block>
          <div v-for="tuple in colorRules" :key="tuple[0]">
            {{ tuple[0] }} {{ tuple[1] }}
            <div>
              <tt>
                {{ tuple[2] }}
              </tt>
            </div>
          </div>
        </f7-block>
        <hr>
        <f7-block-title>CSS Colors</f7-block-title>
        <f7-block>
          <f7-row v-for="color in colors" :key="color[0]">
            <f7-col>
              <span :style="{'color': color[1]}">{{ color[0] }}</span>
            </f7-col>
            <f7-col :style="{'background': color[1]}">
              <span style="color: white">{{ color[0] }}</span>
            </f7-col>
            <f7-col :style="{'background': color[1]}">
              <span style="color: black">{{ color[0] }}</span>
            </f7-col>
          </f7-row>
        </f7-block>
      </f7-page>
    </f7-popup>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Debug from 'debug';
import appDefs from '../app_defs';

const debug = Debug('page:test_tools');

// From https://stackoverflow.com/questions/324486/how-do-you-read-css-rule-values-with-javascript
function iterateCSS(f) {
  for (const styleSheet of window.document.styleSheets) {
    const classes = styleSheet.rules || styleSheet.cssRules;
    if (!classes) {
      continue;
    }

    for (const cssRule of classes) {
      if (cssRule.type !== 1 || !cssRule.style) {
        continue;
      }
      const selector = cssRule.selectorText;
      const style = cssRule.style;
      if (!selector || !style.cssText) {
        continue;
      }
      for (let i=0; i<style.length; i++) {
        const propertyName=style.item(i);
        if (f(selector, propertyName, style.getPropertyValue(propertyName), style.getPropertyPriority(propertyName), cssRule)===false) {
          return;
        }
      }
    }
  }
}

export default {
  components: {
  },
  data: function() {
    return {
      appDefs: appDefs,
      generatedUsername: 'none',
      generatedPassword: 'none',
      verifier: '',
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
      'appMode',
      'currentSiteID',
      'fakeLogin',
      'leftPanelVisible',
      'loggedIn',
      'mock',
      'testAppMode',
    ]),

    colors: function() {
      const colors = [];
      const seen = {};
      iterateCSS( (selector, propertyName, propertyValue, propertyPriority, cssRule) => {
        if (propertyName.startsWith('--bg-color') && !propertyName.endsWith('rgb')) {
          if (!seen[propertyName]) {
            colors.push([propertyName, propertyValue]);
            seen[propertyName] = true;
          }
        } else if (propertyName.startsWith('--f7-color') &&
            !propertyName.endsWith('rgb') &&
            !propertyName.startsWith('--f7-color-picker')) {
          if (!seen[propertyName]) {
            colors.push([propertyName, propertyValue]);
            seen[propertyName] = true;
          }
        }
      });
      return colors;
    },

    colorRules: function() {
      // F7 includes a utility to compute the extended colors needed
      // for tint and shade.  Keep these here to aid maintenance and
      // changes to color theme.
      const tuples = [
        ['blue70', '#448cd8', ''],
        ['lightblue', '#9cb6d3', ''],
        ['green60', '#40a641', ''],
        ['red50', '#d90e00', ''],
        ['yellow100', '#ffb819', ''],
        ['orange100', '#fb9100', ''],
      ];
      for (const t of tuples) {
        t[2] = this.$utils.colorThemeCSSProperties(t[1]);
      }
      return tuples;
    },

  },

  methods: {
    ...vuex.mapActions([
      'logout',
    ]),

    toggleMock: function(evt) {
      debug('toggleMock', evt);
      this.$store.commit('setMock', evt.target.checked);
      this.$store.dispatch('fetchProviders').catch(() => {});
      this.$store.dispatch('checkLogin').catch(() => {});
      this.$store.dispatch('fetchPostLogin').catch(() => {});
    },

    toggleFakeLogin: function(evt) {
      debug('toggleFakeLogin', evt);
      this.$store.commit('setFakeLogin', evt.target.checked);
      this.$store.dispatch('checkLogin').catch(() => {});
    },

    setTestAppMode: function(mode) {
      debug('setTestAppMode', mode);
      this.$store.commit('setTestAppMode', mode);
      // Force the mock to update too
      this.$store.commit('setMock', this.$store.getters.mock);
      this.$store.dispatch('fetchProviders').catch(() => {});
      this.$store.dispatch('checkLogin').catch(() => {});
      this.$store.dispatch('fetchPostLogin').catch(() => {});
    },
  },
};
</script>
