/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Application-wide constants

export default {
  // Primordial mode: used at startup, and to disable mocking
  APPMODE_NONE: null,
  // Local mode: app is talking directly to appliance, or indicates mock
  // appliance mode
  APPMODE_LOCAL: 'local',
  // Cloud mode: app is talking to cloud, or indicates mock cloud mode
  APPMODE_CLOUD: 'cloud',
  // Failure: app cannot determine in what mode it is supposed to run
  APPMODE_FAILURE: 'failure',

  // Well-known vap names
  VAP_EAP: 'eap',
  VAP_PSK: 'psk',
  VAP_GUEST: 'guest',
};
