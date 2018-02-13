<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-login-screen>
    <f7-view>
      <f7-pages>
        <f7-page login-screen>
          <f7-navbar :back-link="$t('message.general.back')" sliding>
            <f7-nav-center><img src="img/bglogo.png"/></f7-nav-center>
            <f7-nav-right>&nbsp;</f7-nav-right>
          </f7-navbar>
          <f7-login-screen-title>{{ $t('message.login.login')}}</f7-login-screen-title>
          <f7-list form>
            <f7-list-item>
              <f7-label>{{ $t('message.login.username')}}</f7-label>
              <f7-input name="username" type="text" :placeholder="$t('message.login.username')"></f7-input>
            </f7-list-item>
            <f7-list-item>
              <f7-label>{{ $t('message.login.password')}}</f7-label>
              <f7-input name="password" type="password" :placeholder="$t('message.login.password')"></f7-input>
            </f7-list-item>
          </f7-list>
          <f7-list>
            <f7-list-button :title="$t('message.login.sign_in')" close-login-screen></f7-list-button>
          </f7-list>
        </f7-page>
      </f7-pages>
    </f7-view>
  </f7-login-screen>
</template>