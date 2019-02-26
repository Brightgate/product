<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>

table.sensitive {
  display: block;
  margin: 10pt auto;
  border: 2px dashed gray;
  padding: 5pt 0pt 5pt 5pt;
  font-size: 12pt;
  background: #FFFFAA;
}

table.sensitive td.generated {
  font-size: 10pt;
  font-family: "Roboto Mono", monospace;
  padding-left: 1em;
}

div.explain {
  display: block;
  margin-top: 10px;
  margin-bottom: 10px;
  font-size: 12pt;
}
.ios span.explainbutton {
  font-weight: bold;
}

.md span.explainbutton {
  text-transform: uppercase;
  font-weight: bold;
}

div.activation-status {
  margin: 1em 0;
  height: 2em;
  text-align: center;
}

a.narrow-button {
  min-width: 0;
  padding: 0 0;
  border: none; /* for ios; in f7 4.x we can get rid of this */
}

div.sensitive {
  background: #FFFFAA;
  border: 2px dashed gray;
}

</style>
<template>
  <!-- XXX I18N is missing for most of this page -->
  <f7-page @page:beforein="onPageBeforeIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.self_provision.title')" sliding />
    <template v-if="provisioned">
      <f7-card>
        <f7-card-header>Wi-Fi Provisioned</f7-card-header>
        <f7-card-content>
          Your wifi credentials are already provisioned.
          <table class="sensitive">
            <tr>
              <td>User&nbsp;name:</td>
              <td>{{ provisionedUsername }}</td>
            </tr>
            <tr>
              <td>Last&nbsp;provisioned:</td>
              <td>{{ provisionedAt }}</td>
            </tr>
          </table>

          Your password cannot be redisplayed for security reasons.  If you have lost your password, you can click <span class="explainbutton">Reprovision</span>.
        </f7-card-content>
        <f7-card-footer>
          <f7-link back>Back</f7-link>
          <f7-link @click="startReprovision">Reprovision</f7-link>
        </f7-card-footer>
      </f7-card>

    </template>
    <template v-else> <!-- !provisioned -->
      <f7-card v-if="!provisioned">
        <f7-card-header>Your New Wi-Fi Login</f7-card-header>
        <f7-card-content>
          <f7-block>
            <!-- XXX table is a weak way to solve this problem.  This
                 should be reworked with flexbox or something else -->
            <table class="sensitive">
              <tr>
                <td colspan="2">User&nbsp;name:</td>
              </tr>
              <tr>
                <td class="generated">{{ generatedUsername }}</td>
                <!-- n.b. we use the vue :class syntax here to get 'narrow-button' to be the most
               specific class for this element, overriding styling from 'button' -->
                <td width="1%">
                  <f7-button :class="'narrow-button'" small icon-material="content_copy" @click="copyUsername" />
                </td>
              </tr>
              <tr>
                <td colspan="2">Password:</td>
              </tr>
              <tr>
                <td class="generated">{{ generatedPassword }}</td>
                <!-- see note above. -->
                <td width="1%">
                  <f7-button :class="'narrow-button'" small icon-material="content_copy" @click="copyPassword" />
                </td>
              </tr>
            </table>
          </f7-block>
        </f7-card-content>
      </f7-card>

      <f7-swiper :params="{'slidesPerView': 1, 'allowTouchMove': false}">
        <f7-swiper-slide>
          <f7-card id="confirm-password">
            <f7-card-header>Step 1: Confirm Your Password</f7-card-header>
            <f7-card-content>
              <f7-block>
                <div class="explain">
                  <p>
                    It's time to set up your Wi-Fi login.  To start, we've
                    automatically generated a user name and password for you.
                  </p>
                  <p>

                    If you like this password, click <span class="explainbutton">Accept</span> to move
                    to the next step.  If you don't like this password,
                    use <span class="explainbutton">Try Again</span>.
                  </p>
                </div>
              </f7-block>
            </f7-card-content>
            <f7-card-footer>
              <f7-link @click="generatePassword">Try Again</f7-link>
              <f7-link @click="stepToNext">Accept</f7-link>
            </f7-card-footer>
          </f7-card>
        </f7-swiper-slide>

        <f7-swiper-slide>
          <f7-card id="save-password">
            <f7-card-header>Step 2: Save Your Login Information</f7-card-header>
            <f7-card-content>
              <f7-block>
                <div class="explain">
                  <p>Store your Wi-Fi user name and password in a safe
                  location.  You need them whenever you add devices to your
                  organization's Wi-Fi.  We recommend using a password manager to
                  securely store this information.
                  </p>
                  <p>
                    <b>If you lose this password, you will need to repeat this process. Keep your password safe!</b>
                  </p>
                </div>
              </f7-block>
            </f7-card-content>
            <f7-card-footer>
              <f7-link @click="stepToPrev">Back</f7-link>
              <f7-link @click="stepToNext">Yes, I have stored them safely</f7-link>
            </f7-card-footer>
          </f7-card>
        </f7-swiper-slide>

        <f7-swiper-slide>
          <f7-card id="provision-password">
            <f7-card-header>Step 3: Activate Your Password</f7-card-header>
            <f7-card-content>
              <f7-block>
                <div class="explain">
                  We are ready to activate your Wi-Fi user name and password.  After
                  this is done, we'll help you configure your devices.
                </div>
                <f7-block>
                  <f7-button :disabled="activate === ACTIVATE.INPROGRESS" fill @click="doActivate">
                    Activate
                  </f7-button>
                  <div class="activation-status">
                    <f7-preloader v-if="activate === ACTIVATE.INPROGRESS" />
                    <div v-if="activate === ACTIVATE.SUCCESS">
                      Activation Succeeded
                    </div>
                    <div v-if="activate === ACTIVATE.FAILED">
                      Activation Failed
                    </div>
                  </div>
                </f7-block>
                <p />
              </f7-block>
            </f7-card-content>
            <f7-card-footer>
              <f7-link @click="stepToPrev">Back</f7-link>
              <f7-link v-if="activate === ACTIVATE.SUCCESS" @click="stepToNext">Continue</f7-link>
            </f7-card-footer>
          </f7-card>
        </f7-swiper-slide>

        <f7-swiper-slide>
          <f7-card id="connect-devices">
            <f7-card-header>Connect to Wi-Fi</f7-card-header>
            <f7-card-content>
              <f7-block>
                <div class="explain">
                  Consult <i>Brightgate Help</i> to learn how to connect
                  your device to the Wi-Fi.
                </div>
              </f7-block>
            </f7-card-content>
            <f7-card-footer>
              <div /> <!-- placeholder -->
              <f7-link @click="stepToNext">Got It</f7-link>
            </f7-card-footer>
          </f7-card>
        </f7-swiper-slide>

        <f7-swiper-slide>
          <f7-card id="the-end">
            <f7-card-header>All done!</f7-card-header>
            <f7-card-content>
              <f7-block>
                <div class="explain">
                  Your username and password are ready to use.
                  <b>This is the last time we will show them to you.</b>
                  Brightgate does not keep plain-text records of your password.
                </div>
              </f7-block>
            </f7-card-content>
            <f7-card-footer>
              <f7-link @click="stepToPrev">Back</f7-link>
              <f7-link back>Finish</f7-link>
            </f7-card-footer>
          </f7-card>
        </f7-swiper-slide>
      </f7-swiper>
    </template>

  </f7-page>
</template>

<script>
import vuex from 'vuex';
import Debug from 'debug';
import * as clipboard from 'clipboard-polyfill';
import siteApi from '../api/site';
import {format, parseISO} from '../date-fns-wrapper';
const debug = Debug('page:self_provision');

const ACTIVATE = {
  NONE: 0,
  INPROGRESS: 1,
  FAILED: 2,
  SUCCESS: 3,
};

export default {
  data: function() {
    return {
      provisioned: false,
      provisionedAt: null,
      provisionedUsername: '',
      generatedUsername: '',
      generatedPassword: '',
      verifier: '',
      step: 'confirm-password',
      activate: ACTIVATE.NONE,
      ACTIVATE,
    };
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...vuex.mapGetters([
    ]),
  },

  methods: {
    stepToNext: async function() {
      const swiper = this.$f7.swiper.get('.swiper-container');
      swiper.slideNext();
    },

    stepToPrev: async function() {
      const swiper = this.$f7.swiper.get('.swiper-container');
      swiper.slidePrev();
    },

    doActivate: async function() {
      this.activate = ACTIVATE.INPROGRESS;
      try {
        await siteApi.accountSelfProvisionPost(this.generatedUsername, this.generatedPassword, this.verifier);
        this.activate = ACTIVATE.SUCCESS;
      } catch (err) {
        this.activate = ACTIVATE.FAILED;
      }
      await this.$store.dispatch('fetchAccountSelfProvision');
    },

    copyUsername: async function() {
      debug('trying to write username to clipboard');
      await clipboard.writeText(this.generatedUsername);
    },

    copyPassword: async function() {
      debug('trying to write password to clipboard');
      await clipboard.writeText(this.generatedPassword);
    },

    generatePassword: async function() {
      const res = await siteApi.accountGeneratePassword();
      debug('accountGeneratePassword res', res);
      this.generatedUsername = res.username;
      this.generatedPassword = res.password;
      this.verifier = res.verifier;
    },

    startReprovision: async function() {
      this.provisioned = false;
      this.provisionedAt = null;
      this.provisionedUsername = '';
      await this.generatePassword();
    },

    onPageBeforeIn: async function() {
      await this.$store.dispatch('fetchAccountSelfProvision');
      const sp = this.$store.getters.accountSelfProvision;
      debug('accountSelfProvisionGet', sp);
      if (sp.status === 'provisioned') {
        this.provisioned = true;
        this.provisionedAt = format(parseISO(sp.completed), 'PPpp');
        this.provisionedUsername = sp.username;
      } else {
        await this.generatePassword();
      }
    },
  },
};
</script>
