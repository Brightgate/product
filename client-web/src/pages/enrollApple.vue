<template>
  <f7-page name="enrollApple">
    <center><h2>Getting Online with Brightgate</h2></center>
    <center><h4>Intro: Install Secure Network Profile</h4></center>
    <f7-grid >
      <!-- NB using widths with %s here is non-standard and won't result
           in using F7 f7-col-<NN> classes to layout. -->
      <f7-col width="30%"></f7-col>
      <f7-col width="10%">
        <f7-button round tab-link="#step1" active><b>1</b></f7-button>
      </f7-col>
      <f7-col width="10%">
        <f7-button round tab-link="#step2"><b>2</b></f7-button>
      </f7-col>
      <f7-col width="10%">
        <f7-button round tab-link="#step3"><b>3</b></f7-button>
      </f7-col>
      <f7-col width="30%"></f7-col>
    </f7-grid >

    <f7-tabs swipeable>

      <f7-tab id="step1" active>
        <f7-card>
          <f7-card-header>Step 1: Install the profile</f7-card-header>
          <f7-card-content>
            <div v-if="showHelp" class="help">
            <p>Once you begin the connection process, you'll be 
            asked if you want to install a profile. 
            Installing this profile will automatically store the 
            network name and password on your device.</p>

            <p>In the first step, the following screen will come up.
            Tap "Install" in the upper right to install the profile.</p>
            </div><!-- help -->
            <center><img src="img/step1.png" width=202 height=424></center>
          </f7-card-content>
          <f7-card-footer>
            <!-- This aligns properly due to flexbox styling with default
                 justify-content: space-between -->
            <f7-link href="/appleConnect" external>Skip intro and connect</f7-link>
            <f7-link v-on:click="showHelp = false" v-if="showHelp">Hide Help</f7-link>
            <f7-link v-on:click="showHelp = true" v-if="!showHelp">Show Help</f7-link>
          </f7-card-footer>
        </f7-card>
      </f7-tab>

      <f7-tab id="step2">
        <f7-card>
          <f7-card-header>Step 2: Enter your device's passcode</f7-card-header>
          <f7-card-content>
            <div v-if="showHelp" class="help">
            <p>After you tap "Install", your device needs your
            permission to store the profile in your
            Profiles. It will ask you for your device passcode.
            This is the same passcode you use to activate your
            device when you power it on.</p>
            </div><!-- help -->
            <center><img src="img/step2.png" width=202 height=424></center>
          </f7-card-content>
          <f7-card-footer>
            <f7-link href="/appleConnect" external>Skip intro and connect</f7-link>
            <f7-link v-on:click="showHelp = false" v-if="showHelp">Hide Help</f7-link>
            <f7-link v-on:click="showHelp = true" v-if="!showHelp">Show Help</f7-link>
          </f7-card-footer>
        </f7-card>
      </f7-tab>

      <f7-tab id="step3">
        <f7-card>
          <f7-card-header>Step 3: Agree to the rules</f7-card-header>
          <f7-card-content>
            <div v-if="showHelp" class="help">
            <p>The owner of the network you're connecting to 
            may ask you to consent to rules about how you're
            going to use the network. Read the message and 
            tap Install to complete the process.</p>
            </div><!-- help -->
            <center><img src="img/step3.png" width=202 height=297></center>
<span></span>
            <f7-link href="/appleConnect" external>
              <center><img src="img/connect-button.png" width=200></center>
            </f7-link>
          </f7-card-content>
          <f7-card-footer>
            <f7-link href="/appleConnect" external>Connect</f7-link>
            <f7-link v-on:click="showHelp = false" v-if="showHelp">Hide Help</f7-link>
            <f7-link v-on:click="showHelp = true" v-if="!showHelp">Show Help</f7-link>
            </f7-grid>
          </f7-card-footer>
        </f7-card>
      </f7-tab>
    </f7-tabs>
  </f7-page>

</template>

<script>
export default {
  data: function() {
    return { showHelp: false }
  }
}
</script>
