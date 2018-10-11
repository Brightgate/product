/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */
import assert from 'assert';

import {cloneDeep, defaults, filter, keyBy, pickBy} from 'lodash-es';
import Promise from 'bluebird';
import superagent from 'superagent-bluebird-promise';
import retry from 'bluebird-retry';
import Vue from 'vue';
import Vuex from 'vuex';
import Debug from 'debug';

import mockDevicesRef from './mock_devices';
import mockUsers from './mock_users';

const debug = Debug('store');
// Run mockDevices through our property deriver
const mockDevices = mockDevicesRef.map(computeDeviceProps);

Vue.use(Vuex);

// XXX this needs further rationalization with devices.json
const device_category_all = ['recent', 'phone', 'computer', 'printer', 'media', 'iot', 'unknown'];

const mockNetworkConfig = {
  ssid: 'mockSSID',
  dnsServer: '1.1.1.1:53',
  defaultRingWPAEAP: 'guest',
  defaultRingWPAPSK: 'unenrolled',
};

const mockAppliances = ['0'];

const mockRings = [
  'core',
  'standard',
  'devices',
  'quarantine',
];

const windowURLAppliance = window && window.location && window.location.href && new URL(window.location.href);
const windowApplianceID = windowURLAppliance.searchParams.get('appliance') || '0';

// Determines whether mock devices are enabled or disabled by default
const enableMock = false;

let initUsers = {};
let initNetworkConfig = {};
let initDevlist = [];
if (enableMock) {
  initDevlist = mockDevices;
  initUsers = cloneDeep(mockUsers);
  initNetworkConfig = cloneDeep(mockNetworkConfig);
}

const initDevices = organizeDevices(initDevlist);
// might take more than just devices in the future
const initAlerts = makeAlerts(initDevices);

const STD_TIMEOUT = {
  response: 5000,
  deadline: 10000,
};
const RETRY_DELAY = 1000;

const state = {
  loggedIn: false,
  fakeLogin: false,
  currentApplianceID: windowApplianceID,
  applianceIDs: [],
  devices: cloneDeep(initDevices),
  alerts: initAlerts,
  rings: [],
  users: cloneDeep(initUsers),
  networkConfig: cloneDeep(initNetworkConfig),
  enableMock: enableMock,
};

const mutations = {
  setApplianceIDs(state, newIDs) {
    state.applianceIDs = newIDs;
    state.applianceIDs.push('Other Appliance');
    if (state.applianceIDs.length === 1) {
      state.currentApplianceID = state.applianceIDs[0];
    }
  },

  setDevices(state, newDevices) {
    state.devices = newDevices;
    state.alerts = makeAlerts(newDevices);
  },

  setRings(state, newRings) {
    state.rings = newRings;
  },

  setNetworkConfig(state, newConfig) {
    state.networkConfig = newConfig;
  },

  setUsers(state, newUsers) {
    state.users = newUsers;
  },

  updateUser(state, user) {
    assert(user.UUID);
    state.users[user.UUID] = user;
  },

  setLoggedIn(state, newLoggedIn) {
    debug(`setLoggedIn: now ${newLoggedIn}`);
    state.loggedIn = newLoggedIn;
  },

  toggleMock(state) {
    state.enableMock = !state.enableMock;
  },

  toggleFakeLogin(state) {
    state.fakeLogin = !state.fakeLogin;
  },
};

const getters = {
  Is_Logged_In: (state) => {
    return state.loggedIn || state.fakeLogin;
  },

  Fake_Login: (state) => {return state.fakeLogin;},

  ApplianceIDs: (state) => {return state.applianceIDs;},

  CurrentApplianceID: (state) => {return state.currentApplianceID;},

  RealAppliance: (state) => {
    return (state.applianceIDs.length === 1 && state.applianceIDs[0] === '0');
  },

  Device_By_UniqID: (state) => (uniqid) => {
    return state.devices.by_uniqid[uniqid];
  },

  All_Devices: (state) => {
    return state.devices.all_devices;
  },
  Device_Count: (state) => (devices) => {
    assert(Array.isArray(devices), 'expected devices to be array');
    return devices.length;
  },

  // Return an array of devices for the category, sorted by network_name.
  Devices_By_Category: (state) => (category) => {
    return state.devices.by_category[category];
  },

  Devices_By_Ring: (state) => (ring) => {
    if (state.devices.by_ring[ring] === undefined) {
      return [];
    }
    return state.devices.by_ring[ring];
  },

  Device_Active: (state) => (devices) => {
    return filter(devices, {active: true});
  },

  Device_VulnScanned: (state) => (devices) => {
    return filter(devices, 'scans.vulnerability.finish');
  },

  Device_Vulnerable: (state) => (devices) => {
    return filter(devices, 'activeVulnCount');
  },

  Device_NotVulnerable: (state) => (devices) => {
    return filter(devices, {activeVulnCount: 0});
  },

  All_Alerts: (state) => {return state.alerts;},

  Alert_Count: (state) => (alerts) => {
    assert(typeof(alerts) === 'object' && !Array.isArray(alerts), 'expected alerts to be object');
    return Object.keys(alerts).length;
  },

  Alert_Active: (state) => (alerts) => {
    return pickBy(alerts, {vulninfo: {active: true}});
  },

  Alert_Inactive: (state) => (alerts) => {
    return pickBy(alerts, {vulninfo: {active: false}});
  },

  Alert_By_Ring: (state) => (ring, alerts) => {
    return pickBy(alerts, {device: {ring: ring}});
  },

  Rings: (state) => {return state.rings;},

  All_Users: (state) => {return state.users;},
  User_Count: (state) => (users) => {
    assert(typeof(users) === 'object' && !Array.isArray(users), 'expected users to be object');
    return Object.keys(users).length;
  },

  User_By_UUID: (state) => (uuid) => {return state.users[uuid];},

  Network_Config: (state) => {return state.networkConfig;},

  Mock: (state) => {return state.enableMock;},
};

// Get a property's value
function fetchPropP(context, property, default_value) {
  const u = `/api/appliances/${context.state.currentApplianceID}/config`;
  debug(`fetchPropP(${property}, ${default_value}) -> ${u}`);
  return superagent.get(u
  ).query(property
  ).timeout(STD_TIMEOUT
  ).then((res) => {
    debug(`fetchPropP(${property}): returning ${res.body}`);
    return res.body;
  }).catch((err) => {
    if (default_value !== undefined) {
      debug(`fetchPropP(${property}): defaulting`);
      return Promise.resolve(default_value);
    }
    throw err;
  });
}

// Make up to maxcount attempts to see if property has changed to an
// expected value.
function checkPropChangeP(context, property, value, maxcount, count) {
  assert.equal(typeof property, 'string');
  assert.equal(typeof maxcount, 'number');
  count = count === undefined ? maxcount : count;
  assert.equal(typeof count, 'number');
  const u = `/api/appliances/${context.state.currentApplianceID}/config`;
  const attempt_str = `#${(maxcount - count) + 1}`;
  debug(`checkPropChangeP: waiting for ${property} == ${value}, try ${attempt_str} ${u}`);
  return superagent.get(u
  ).query(property
  ).timeout(STD_TIMEOUT
  ).then((res) => {
    if (res.body !== value) {
      throw new Error(`Tried ${maxcount} times but did not see property change`);
    }
    debug(`checkPropChangeP: I saw ${property} -> ${value}`);
    return value;
  }).catch((err) => {
    if (count === 0) {
      throw new Error(`Couldn't see property change after ${maxcount} tries.  Last error was: ${err}`);
    }
    debug(`checkPropChangeP: failed ${attempt_str}, will retry.  ${err}`);
    return Promise.delay(RETRY_DELAY
    ).then(() => {
      return checkPropChangeP(context, property, value, maxcount, count - 1);
    });
  });
}

// Make up to maxcount attempts to load the devices configuration object
function devicesGetP(context) {
  const u = `/api/appliances/${context.state.currentApplianceID}/devices`;
  debug(`devicesGetP: GET ${u}`);
  return superagent.get(u
  ).timeout(STD_TIMEOUT
  ).then((res) => {
    debug('devicesGetP: got response');
    if (res.body === null ||
        (typeof res.body !== 'object') ||
        !('Devices' in res.body)) {
      throw new Error('Saw incomplete or bad GET /devices response.');
    } else {
      return res.body;
    }
  });
}

function organizeDevices(all_devices) {
  assert(Array.isArray(all_devices));
  const devices = {
    all_devices: all_devices,
    by_uniqid: {},
    by_category: {},
    by_ring: {},
  };

  // First, organize by unique id.
  devices.by_uniqid = keyBy(devices.all_devices, 'uniqid');

  // Next, Reorganize the data into:
  // { 'phone': [list of phones...], 'computer': [...] ... }
  //
  // Make sure all categories are present.
  devices.by_category = {};
  for (const c of device_category_all) {
    devices.by_category[c] = [];
  }

  devices.all_devices.reduce((result, value) => {
    assert(value.category in devices.by_category, `category ${value.category} is missing`);
    result[value.category].push(value);
    return result;
  }, devices.by_category);

  // Index by ring
  devices.all_devices.reduce((result, value) => {
    if (result[value.ring] === undefined) {
      result[value.ring] = [];
    }
    result[value.ring].push(value);
    return result;
  }, devices.by_ring);

  // Tabulate vulnerability counts for each device
  devices.all_devices.forEach((device) => {
    const actives = pickBy(device.vulnerabilities, {active: true});
    device.activeVulnCount = Object.keys(actives).length;
  });

  debug(devices);
  return devices;
}

// Today all of the alerts we make are derived from the devices
// list.  In the future, that could change.
function makeAlerts(devices) {
  const alerts = [];

  for (const [, device] of Object.entries(devices.by_uniqid)) {
    for (const [vulnid, vulninfo] of Object.entries(device.vulnerabilities)) {
      alerts.push({
        'device': device,
        'vulnid': vulnid,
        'vulninfo': vulninfo,
      });
    }
  }
  return alerts;
}

function computeDeviceProps(device) {
  const k2c = {
    'android': 'phone',
    'ios': 'phone',
    'computer': 'computer',
    'iot': 'iot',
    'unknown': 'unknown',
    'media': 'media',
    'printer': 'printer',
  };
  const k2m = {
    'android': 'mobile-phone-1',
    'ios': 'mobile-phone-1',
    'computer': 'laptop-1',
    'iot': 'webcam-1',
    'unknown': 'interface-question-mark',
    'media': 'television',
    'printer': 'tablet', // XXX for now
  };
  assert(typeof(device.confidence) === 'number');
  // derived from logic in configctl
  if (device.confidence < 0.5) {
    device.category = 'unknown';
    device.media = k2m['unknown'];
    device.certainty = 'low';
  } else {
    device.certainty = device.confidence < 0.87 ? 'medium' : 'high';
    device.category = device.kind in k2c ? k2c[device.kind] : k2c['unknown'];
    device.media = device.kind in k2m ? k2m[device.kind] : k2m['unknown'];
  }
  return device;
}

let fetchDevicesPromise = Promise.resolve();
let fetchPeriodicTimeout = null;

const actions = {
  // Load the list of appliances from the server.
  fetchApplianceIDs(context) {
    debug('fetchApplianceIDs: GET /api/appliances');
    return superagent.get('/api/appliances'
    ).then((res) => {
      debug('fetchApplianceIDs: Succeeded: ', res.body);
      assert(typeof res.body === 'object');
      context.commit('setApplianceIDs', res.body);
    }).catch((err) => {
      if (context.state.enableMock) {
        debug('fetchApplianceIDs: Using mocked appliances');
        context.commit('setApplianceIDs', mockAppliances);
        return;
      }
      debug(`fetchAppliances: Error ${err}`);
      throw err;
    });
  },

  // Load the list of devices from the server.  merge it with mock devices
  // defined locally (if using).
  fetchDevices(context) {
    debug('Store: fetchDevices');
    // Join callers so that only one fetch is ongoing
    if (fetchDevicesPromise.isPending()) {
      debug('Store: fetchDevices (pending)');
      return fetchDevicesPromise;
    }
    let all_devices = [];
    if (context.state.enableMock) {
      debug(`Store: fetchDevices: enableMock = ${context.state.enableMock}`);
      all_devices = mockDevices;
    }
    const p = retry(devicesGetP, {interval: RETRY_DELAY, max_tries: 10, args: [context]}
    ).then((res_json) => {
      const mapped_devices = res_json.Devices.map((dev) => {
        return computeDeviceProps({
          manufacturer: dev.Manufacturer,
          model: dev.Model,
          kind: dev.Kind,
          confidence: dev.Confidence,
          network_name: dev.HumanName ? dev.HumanName : `Unknown (${dev.HwAddr})`, // XXX this has issues, including i18n)
          ipv4_addr: dev.IPv4Addr,
          os_version: dev.OSVersion,
          activated: '',
          uniqid: dev.HwAddr,
          hwaddr: dev.HwAddr,
          ring: dev.Ring,
          active: dev.Active,
          scans: dev.Scans,
          vulnerabilities: dev.Vulnerabilities,
        });
      });

      all_devices = all_devices.concat(mapped_devices);
    }).finally(() => {
      const devices = organizeDevices(all_devices);
      context.commit('setDevices', devices);
      debug('Store: fetchDevices finished');
    });
    // make sure promise is a bluebird promise, so we can call isPending
    fetchDevicesPromise = Promise.resolve(p);
    return fetchDevicesPromise;
  },

  // Start a timer-driven periodic fetch of devices
  fetchPeriodic(context) {
    // if not logged in, just come back later
    if (!context.getters.Is_Logged_In) {
      debug('fetchPeriodic: not logged in, later');
      fetchPeriodicTimeout = setTimeout(() => {
        context.dispatch('fetchPeriodic');
      }, 10000);
      return;
    }

    debug('fetchPeriodic: dispatching fetchDevices');
    context.dispatch('fetchDevices'
    ).then(() => {
      fetchPeriodicTimeout = setTimeout(() => {
        context.dispatch('fetchPeriodic');
      }, 10000);
    }, () => {
      debug('fetchPeriodic: failed, back in 30');
      fetchPeriodicTimeout = setTimeout(() => {
        context.dispatch('fetchPeriodic');
      }, 30000);
    });
    return;
  },

  fetchPeriodicStop(context) {
    clearTimeout(fetchPeriodicTimeout);
    fetchPeriodicTimeout = null;
  },

  // Load the list of rings from the server.
  fetchRings(context) {
    const u = `/api/appliances/${context.state.currentApplianceID}/rings`;
    debug(`fetchRings: GET ${u}`);
    return superagent.get(u
    ).then((res) => {
      debug('fetchRings: Succeeded: ', res.body);
      assert(typeof res.body === 'object');
      context.commit('setRings', res.body);
    }).catch((err) => {
      if (context.state.enableMock) {
        debug('fetchRings: Using mocked rings');
        context.commit('setRings', mockRings);
        return;
      }
      debug(`fetchRings: Error ${err}`);
      throw err;
    });
  },

  // Load the various aspects of the network configuration from the server.
  fetchNetworkConfig(context, appliance) {
    debug(`fetchNetworkConfig`);
    return Promise.props({
      ssid: fetchPropP(context, '@/network/ssid'),
      dnsServer: fetchPropP(context, '@/network/dnsserver', ''),
      defaultRingWPAEAP: fetchPropP(context, '@/network/default_ring/wpa-eap', ''),
      defaultRingWPAPSK: fetchPropP(context, '@/network/default_ring/wpa-psk', ''),
    }).catch((err) => {
      if (context.state.enableMock) {
        debug('fetchNetworkConfig: Using mocked networkConfig');
        return mockNetworkConfig;
      } else {
        debug(`fetchNetworkConfig: Error ${err}`);
        throw err;
      }
    }).then((netConfig) => {
      context.commit('setNetworkConfig', netConfig);
    });
  },

  // Set a simple property to the specified value.
  setConfigProp(context, {property, value}) {
    assert.equal(typeof property, 'string');
    const u = `/api/appliances/${context.state.currentApplianceID}/config`;
    debug(`setConfigProp: POST ${u} ${property}=${value}`);
    return superagent.post(u
    ).type('form'
    ).send({[property]: value}
    ).then(() => {
      debug(`setConfigProp: finished setting ${property} = ${value}.`);
    }).catch((err) => {
      debug(`setConfigProp: Error ${err}`);
      throw err;
    });
  },

  enrollGuest(context, {type, phone, email}) {
    const args = {type, phone, email};
    const u = `/api/appliances/${context.state.currentApplianceID}/enroll_guest`;
    debug(`enrollGuest ${u}`, args);
    return superagent.post(u
    ).type('form'
    ).send(args
    ).set('Accept', 'application/json');
  },

  // Ask the server to change the ring property for a device, then
  // attempt to wait for that change to propagate.  In practice this
  // seems to take several seconds, during which time the server may
  // become unreachable; thus we use retrys to make things work properly.
  changeRing(context, {deviceUniqID, newRing}) {
    assert.equal(typeof deviceUniqID, 'string');
    assert.equal(typeof newRing, 'string');
    debug(`changeRing: ${deviceUniqID} -> ${newRing}`);
    const propname = `@/clients/${deviceUniqID}/ring`;
    return context.dispatch('setConfigProp', {
      property: propname,
      value: newRing,
    }).then(() => {
      return checkPropChangeP(context, propname, newRing, 10);
    }).finally(() => {
      // let this run async?
      context.dispatch('fetchDevices');
    });
  },

  // Load the list of users from the server.
  fetchUsers(context) {
    const user_result = {};
    if (context.state.enableMock) {
      debug('fetchUsers: Adding mock users');
      defaults(user_result, mockUsers);
    }
    const u = `/api/appliances/${context.state.currentApplianceID}/users`;
    debug(`fetchUsers: GET ${u}`);
    return superagent.get(u
    ).then((res) => {
      debug('fetchUsers: Succeeded: ', res);
      if (res && res.body && res.body.Users) {
        assert(typeof res.body.Users === 'object');
        defaults(user_result, res.body.Users);
      } else {
        debug('fetchUsers: res.body had unexpected contents');
      }
    }).finally(() => {
      context.commit('setUsers', user_result);
    }).catch((err) => {
      debug(`fetchUsers: Error ${err}`);
      throw err;
    });
  },

  // Create or Update a user
  saveUser(context, {user, newUser}) {
    assert(typeof user === 'object');
    assert(typeof newUser === 'boolean');
    const action = newUser ? 'creating' : 'updating';
    const u = `/api/appliances/${context.state.currentApplianceID}/users/${newUser ? 'NEW' : user.UUID}`;
    debug(`store.js saveUsers: ${action} ${user.UUID} ${u}`, user);
    if (newUser) {
      // Backend is strict about UUID
      delete user.UUID;
    }

    return superagent.post(u
    ).type('json'
    ).send(user
    ).then((res) => {
      debug('saveUser: Succeeded: ', res.body);
      assert(typeof res.body === 'object');
      context.commit('updateUser', res.body);
    }).catch((err) => {
      debug('err is', err);
      if (err.res && err.res.text) {
        throw new Error(`Failed to save user: ${err.res.text}`);
      } else {
        throw err;
      }
    });
  },

  deleteUser(context, {user, newUser}) {
    assert(typeof user === 'object');
    const u = `/api/appliances/${context.state.currentApplianceID}/users/${user.UUID}`;
    debug(`deleteUser: ${user.UUID} ${u}`, user);
    return superagent.delete(u
    ).then(() => {
      context.dispatch('fetchUsers');
    });
  },

  checkLogin(context) {
    return superagent.get('/auth/userid'
    ).then(() => {
      debug('checkLogin: logged in');
      context.commit('setLoggedIn', true);
    }).catch((err) => {
      debug('checkLogin: not logged in');
      throw err;
    });
  },

  login(context, {uid, userPassword}) {
    assert.equal(typeof uid, 'string');
    assert.equal(typeof userPassword, 'string');
    debug('login: /auth/appliance/login');
    return superagent.post('/auth/appliance/login'
    ).type('form'
    ).send({uid, userPassword}
    ).then(() => {
      debug(`login: Logged in as ${uid}.`);
      context.commit('setLoggedIn', true);
      // Let these run async
      context.dispatch('fetchDevices');
      context.dispatch('fetchRings');
      context.dispatch('fetchUsers');
      context.dispatch('fetchPeriodic');
    }).catch((err) => {
      debug(`login: Error ${err}`);
      throw err;
    });
  },

  logout(context) {
    debug('logout: /auth/logout');
    return superagent.get('/auth/logout'
    ).then(() => {
      debug('logout: Completed');
      context.commit('setLoggedIn', false);
      context.dispatch('fetchPeriodicStop');
    }).catch((err) => {
      debug(`logout: Error ${err}`);
      throw err;
    });
  },
};

export default new Vuex.Store({
  strict: true, // XXX: for debugging only, expensive, see manual
  actions,
  state,
  getters,
  mutations,
});
