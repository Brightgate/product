/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// Application-wide constants

const ROLE_ADMIN= 'admin';
const ROLE_USER= 'user';

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

  ROLE_ADMIN,
  ROLE_USER,
  ALL_ROLES: [ROLE_ADMIN, ROLE_USER],

  LOGIN_REASON: {
    UNKNOWN_ERROR: -1,
    SERVER_ERROR: 1,
    NO_OAUTH_RULE_MATCH: 2,
    NO_ROLES: 3,
    NO_SESSION: 4,
  },

  // These also map to the indices in the metrics array
  METRIC_INTERVALS: {
    SECOND: 0,
    MINUTE: 1,
    HOUR: 2,
    DAY: 3,
  },

  WIREGUARD_PORT: 51820,
};

