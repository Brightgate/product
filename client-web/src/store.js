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

import {filter, keyBy, pickBy} from 'lodash-es';
import Promise from 'bluebird';
import retry from 'bluebird-retry';
import Vue from 'vue';
import Vuex from 'vuex';
import Debug from 'debug';

import applianceApi from './api/appliance';

const debug = Debug('store');

Vue.use(Vuex);

// XXX this needs further rationalization with devices.json
const device_category_all = ['recent', 'phone', 'computer', 'printer', 'media', 'iot', 'unknown'];

const windowURLAppliance = window && window.location && window.location.href && new URL(window.location.href);
const initApplianceID = windowURLAppliance.searchParams.get('appliance') || '0';

const RETRY_DELAY = 1000;

const state = {
  loggedIn: false,
  fakeLogin: false,
  enableMock: false,
  currentApplianceID: initApplianceID,
  applianceIDs: [],
  devices: organizeDevices([]),
  alerts: [],
  rings: {},
  users: {},
  networkConfig: {},
};

const mutations = {
  setApplianceIDs(state, newIDs) {
    state.applianceIDs = newIDs;
    if (state.applianceIDs.length === 1) {
      state.currentApplianceID = state.applianceIDs[0];
      state.applianceIDs.push('Other Appliance');
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
    if (state.enableMock) {
      applianceApi.enableMock();
    } else {
      applianceApi.disableMock();
    }
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

  debug('organizeDevices returning', devices);
  return devices;
}

// Today all of the alerts we make are derived from the devices
// list.  In the future, that could change.
function makeAlerts(devices) {
  const alerts = [];

  if (!devices || !devices.by_uniqid) {
    return alerts;
  }
  for (const [, device] of Object.entries(devices.by_uniqid)) {
    if (!device.vulnerabilities) {
      continue;
    }
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

// Take an API device and transform it for local use.
// Much of this is legacy and could be fixed.
function computeDeviceProps(apiDevice) {
  const device = {
    manufacturer: apiDevice.Manufacturer,
    model: apiDevice.Model,
    kind: apiDevice.Kind,
    confidence: apiDevice.Confidence,
    network_name: apiDevice.HumanName ? apiDevice.HumanName : `Unknown (${apiDevice.HwAddr})`, // XXX this has issues, including i18n)
    ipv4_addr: apiDevice.IPv4Addr,
    os_version: apiDevice.OSVersion,
    activated: '',
    uniqid: apiDevice.HwAddr,
    hwaddr: apiDevice.HwAddr,
    ring: apiDevice.Ring,
    active: apiDevice.Active,
    connAuthType: apiDevice.ConnAuthType,
    connMode: apiDevice.ConnMode,
    connNode: apiDevice.ConnNode,
    scans: apiDevice.Scans,
    vulnerabilities: apiDevice.Vulnerabilities,
  };

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
  async fetchApplianceIDs(context) {
    context.commit('setApplianceIDs', await applianceApi.appliancesGet());
  },

  // Load the list of devices from the server.
  fetchDevices(context) {
    debug('Store: fetchDevices');

    // Join callers so that only one fetch is ongoing
    if (fetchDevicesPromise.isPending()) {
      debug('Store: fetchDevices (pending)');
      return fetchDevicesPromise;
    }

    let devices = [];
    const applianceID = context.state.currentApplianceID;
    const p = retry(applianceApi.applianceDevicesGet,
      {interval: RETRY_DELAY, max_tries: 5, args: [applianceID]}
    ).then((apiDevices) => {
      devices = apiDevices.map(computeDeviceProps);
    }).finally(() => {
      const organized_devices = organizeDevices(devices);
      context.commit('setDevices', organized_devices);
      debug('Store: fetchDevices finished');
    });
    // make sure promise is a bluebird promise, so we can call isPending
    fetchDevicesPromise = Promise.resolve(p);
    return fetchDevicesPromise;
  },

  // Start a timer-driven periodic fetch of devices
  fetchPeriodic(context) {
    // if not logged in, just come back later
    if (fetchPeriodicTimeout !== null) {
      clearTimeout(fetchPeriodicTimeout);
      fetchPeriodicTimeout = null;
    }
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
  async fetchRings(context) {
    const id = context.state.currentApplianceID;
    const rings = await applianceApi.applianceRingsGet(id);
    context.commit('setRings', rings);
  },

  // Load the various aspects of the network configuration from the server.
  async fetchNetworkConfig(context) {
    debug(`fetchNetworkConfig`);
    const id = context.state.currentApplianceID;
    const nc = await Promise.props({
      ssid: applianceApi.applianceConfigGet(id, '@/network/ssid'),
      dnsServer: applianceApi.applianceConfigGet(id, '@/network/dnsserver', ''),
      defaultRingWPAEAP: applianceApi.applianceConfigGet(id, '@/network/default_ring/wpa-eap', ''),
      defaultRingWPAPSK: applianceApi.applianceConfigGet(id, '@/network/default_ring/wpa-psk', ''),
    });
    debug('fetchNetworkConfig committing', nc);
    context.commit('setNetworkConfig', nc);
    return nc;
  },

  async enrollGuest(context, {type, phone, email}) {
    const id = context.state.currentApplianceID;
    return await applianceApi.applianceEnrollGuest(id, {type, phone, email});
  },

  // Ask the server to change the ring property for a device, then
  // attempt to wait for that change to propagate.  In practice this
  // seems to take several seconds, during which time the server may
  // become unreachable; thus we use retrys to make things work properly.
  async changeRing(context, {deviceUniqID, newRing}) {
    const id = context.state.currentApplianceID;
    await applianceApi.applianceClientsRingSet(id, deviceUniqID, newRing);
    context.dispatch('fetchDevices');
  },

  async fetchUsers(context) {
    const id = context.state.currentApplianceID;
    const users = await applianceApi.applianceUsersGet(id);
    context.commit('setUsers', users);
  },

  // Create or Update a user
  async saveUser(context, {user, newUser}) {
    assert(typeof user === 'object');
    assert(typeof newUser === 'boolean');
    const id = context.state.currentApplianceID;
    const action = newUser ? 'creating' : 'updating';
    debug(`saveUser: ${action} ${user.UUID}`, user);
    if (newUser) {
      delete user.UUID; // Backend is strict about UUID
    }
    try {
      const postUser = await applianceApi.applianceUsersPost(id, user, newUser);
      context.commit('updateUser', postUser);
    } catch (err) {
      debug('saveUser failed', err);
      if (err.res && err.res.text) {
        throw new Error(`Failed to save user: ${err.res.text}`);
      } else {
        throw err;
      }
    }
  },

  async deleteUser(context, {user, newUser}) {
    assert(typeof user === 'object');
    const id = context.state.currentApplianceID;
    await applianceApi.applianceUsersDelete(id, user.UUID);
    context.dispatch('fetchUsers');
  },

  async checkLogin(context) {
    let loggedin = false;
    try {
      await applianceApi.authUserid();
      loggedin = true;
    } catch (err) {
      loggedin = false;
    }
    context.commit('setLoggedIn', loggedin);
    debug(`checkLogin: ${loggedin}`);
    return loggedin;
  },

  async supreme(context) {
    const id = context.state.currentApplianceID;
    return await applianceApi.applianceSupreme(id);
  },

  async login(context, {uid, userPassword}) {
    assert.equal(typeof uid, 'string');
    assert.equal(typeof userPassword, 'string');
    await applianceApi.authApplianceLogin(uid, userPassword);
    context.commit('setLoggedIn', true);
    // Let these run async
    context.dispatch('fetchDevices');
    context.dispatch('fetchRings');
    context.dispatch('fetchUsers');
    context.dispatch('fetchPeriodic');
  },

  logout(context) {
    debug('logout');
    applianceApi.authApplianceLogout();
    debug('logout: Completed');
    context.commit('setLoggedIn', false);
    context.dispatch('fetchPeriodicStop');
  },
};

export default new Vuex.Store({
  strict: true, // XXX: for debugging only, expensive, see manual
  actions,
  state,
  getters,
  mutations,
});
