<!--
  COPYRIGHT 2019 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<style scoped>

div.sensitive-grid {
  min-width: 300px;
  margin: 8px auto;
  padding: 12px 20px;
  font-size: 12pt;
  display: grid;
  width: fit-content;
  grid-template-columns: auto auto;
  grid-column-gap: 4px;
  border-radius: 4px;
  /* border: 2px dashed var(--bg-color-yellow-100); */
  background: #f0f0f0;
}

div.sensitive-grid.good {
  /* border: 2px solid var(--bg-color-green-60); */
  background: rgba(var(--bg-color-green-60-rgb), 0.1);
}

div.sensitive-grid div.sg-label--2 {
  grid-column: 1 / span 2;
  font-size: 12px;
}

div.sensitive-grid div.sg-note {
  margin-top: 6px;
}

div.sensitive-grid div.sg-button {
  justify-self: end;
}

div.sensitive-grid div.sg-content {
  padding-left: 1em;
  /*
   * Force height so that when no button is present, things look good.
   * Assumes button has no border.
   */
  height: var(--f7-button-height);
}


/*
 * This set of clauses helps to suppress problematic behavior when users
 * triple-click on HTML elements to select them.  Inside table, the
 * browser tends to append a tab character (tsv, anyone?).  In the case
 * of a div, the browser tends to append a newline.  By setting user-select:none
 * on the parent, and user-select:all on the child, the correct effect is
 * achieved.
 */
.select-none {
  user-select: none;
}

/*
 * Reading the spec makes one think that the right value here would be
 * 'text' (really 'contain' but that is IE specific); but in our testing only
 * 'all'-- which forces the content to be selected atomically-- did the right
 * thing.
 */
.select-all {
  user-select: all;
}

.generated {
  font-size: 10pt;
  /* https://systemfontstack.com/ */
  font-family: Menlo, Consolas, Monaco, "Liberation Mono", "Lucida Console", monospace;
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

div.activation-error {
  display: flex;
  justify-content: center;
  margin: 2em 0 1em 0;
  font-size: 12pt;
  align-items: center;
}

div.activation-error div.ae-inner {
  padding: 4px;
}

span.warning {
  color: var(--bg-color-red-50);
}
.status-good {
  color: var(--bg-color-green-70);
}

</style>
<template>
  <!-- XXX I18N is missing for most of this page -->
  <f7-page @page:beforein="onPageBeforeIn">
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.wifi_provision.title')" sliding />
    <template v-if="sp && sp.status === 'provisioned' && activate !== ACTIVATE.SUCCESS">
      <f7-card>
        <f7-card-header>Wi-Fi Provisioned</f7-card-header>
        <f7-card-content>
          <div class="sensitive-grid">
            <!-- username section -->
            <div class="sg-label--2">User&nbsp;name:</div>
            <div class="sg-content"><span class="generated">{{ sp.username }}</span></div>
            <!-- password section -->
            <div class="sg-label--2">Password:</div>
            <div class="sg-content select-none">
              <span class="select-all generated">******************</span>
            </div>
            <!-- org section, if applicable -->
            <template v-if="orgsCount > 1">
              <div class="sg-label--2">Valid For:</div>
              <div class="sg-content select-none">All {{ currentOrg.name }} sites</div>
            </template>
            <!-- Status section -->
            <div class="sg-label--2">Status:</div>
            <div class="sg-content status-good">Active since {{ formatTime(sp.completed) }}</div>
          </div>
          Your wifi credentials are already provisioned.  Your password cannot be redisplayed for security reasons.  If you have lost your password, you can click <span class="explainbutton">Reprovision</span>.
        </f7-card-content>
        <f7-card-footer>
          <f7-link back>Back</f7-link>
          <f7-link @click="startReprovision">Reprovision</f7-link>
        </f7-card-footer>
      </f7-card>

    </template>
    <template v-else> <!-- !provisioned -->
      <f7-card>
        <f7-card-header>Your New Wi-Fi Login</f7-card-header>
        <f7-card-content>
          <div :class="{ good: activate === ACTIVATE.SUCCESS }" class="sensitive-grid">
            <!-- row 1 -->
            <div class="sg-label--2">User&nbsp;name:</div>
            <!-- row 2 -->
            <div class="sg-content select-none">
              <span class="select-all generated">{{ generatedUsername }}</span>
            </div>
            <div class="sg-button">
              <f7-button small text="Copy" @click="copyUsername" />
            </div>
            <!-- row 3 -->
            <div class="sg-label--2">Password:</div>
            <!-- row 4 -->
            <div class="sg-content select-none">
              <span class="select-all generated">{{ generatedPassword }}</span>
            </div>
            <div class="sg-button">
              <f7-button small text="Copy" @click="copyPassword" />
            </div>
            <template v-if="orgsCount > 1">
              <!-- row 5 -->
              <div class="sg-label--2">Valid For:</div>
              <!-- row 6 -->
              <div class="sg-content select-none">All {{ currentOrg.name }} sites</div>
            </template>
            <!-- Status section -->
            <div class="sg-label--2">Status:</div>
            <div
              v-if="activate !== ACTIVATE.SUCCESS"
              class="sg-content text-color-red">
              Not Active Yet
            </div>
            <div v-else class="sg-content status-good">
              This password is now activated
            </div>
          </div>
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
              <div v-if="activate === ACTIVATE.FAILED" class="activation-error">
                <div class="ae-inner">
                  <f7-icon md="material:error" ios="f7:alert_fill" color="red" />
                </div>
                <div class="ae-inner">
                  Activation failed.  Contact your service representative for help.
                </div>
              </div>
            </f7-card-content>
            <f7-card-footer>
              <f7-link @click="stepToPrev">Back</f7-link>
              <f7-link @click="doActivate">I stored it safely, Activate!</f7-link>
            </f7-card-footer>
          </f7-card>
        </f7-swiper-slide>

        <f7-swiper-slide>
          <f7-card id="connect-devices">
            <f7-card-header>Step 3: Connect to Wi-Fi</f7-card-header>
            <f7-card-content>
              <f7-block>
                <div class="explain">
                  Your user name and password are ready to use.
                  <b>This is the last time we will show them to you.</b>
                  Brightgate does not keep plain-text records of your password.
                </div>
                <div class="explain">
                  Consult the <f7-link href="/help/end_customer_guide">Admin
                  Guide</f7-link> to learn how to connect your device to the Wi-Fi.
                  <ul>
                    <li>
                      <f7-link href="/help/end_customer_guide/add-iphone-network">Connect an iPhone</f7-link>
                    </li>
                    <li>
                      <f7-link href="/help/end_customer_guide/add-android-phone-network">Connect an Android Phone</f7-link>
                    </li>
                    <li>
                      <f7-link href="/help/end_customer_guide/add-windows-network">Connect a Windows Computer</f7-link>
                    </li>
                    <li>
                      <f7-link href="/help/end_customer_guide/add-mac-network">Connect a Mac</f7-link>
                    </li>
                  </ul>
                </div>
              </f7-block>
            </f7-card-content>
            <f7-card-footer>
              <div /> <!-- placeholder -->
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
const debug = Debug('page:wifi_provision');

const ACTIVATE = {
  NONE: 0,
  INPROGRESS: 1,
  FAILED: 2,
  SUCCESS: 3,
};

const DEFAULT_SP = {
  status: 'unprovisioned',
  completed: null,
  username: null,
};

export default {
  data: function() {
    return {
      sp: DEFAULT_SP,
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
      'currentOrg',
      'myAccountUUID',
      'myAccount',
      'orgsCount',
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
      this.$f7.preloader.show();
      try {
        await siteApi.accountSelfProvisionPost(this.myAccountUUID,
          this.generatedUsername, this.generatedPassword, this.verifier);
        this.activate = ACTIVATE.SUCCESS;
      } catch (err) {
        this.activate = ACTIVATE.FAILED;
      }
      this.$f7.preloader.hide();
      await this.$store.dispatch('fetchAccountSelfProvision', this.myAccountUUID);
      // Update local SP to version from store
      if (this.activate === ACTIVATE.SUCCESS) {
        await this.stepToNext();
      }
      this.sp = this.myAccount.selfProvision;
    },

    copyUsername: async function() {
      debug('trying to write user name to clipboard');
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
      this.sp = DEFAULT_SP;
      await this.generatePassword();
    },

    onPageBeforeIn: async function() {
      await this.$store.dispatch('fetchAccountSelfProvision', this.myAccountUUID);
      this.sp = this.myAccount.selfProvision;
      debug('this.sp', this.sp);
      if (this.sp.status !== 'provisioned') {
        await this.generatePassword();
      }
    },

    formatTime: function(t) {
      return format(parseISO(t), 'PPp');
    },
  },
};
</script>
