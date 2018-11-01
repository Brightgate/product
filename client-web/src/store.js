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
const DEVICE_CATEGORY_ALL = ['recent', 'phone', 'computer', 'printer', 'media', 'iot', 'unknown'];
const RETRY_DELAY = 1000;

const windowURLAppliance = window && window.location && window.location.href && new URL(window.location.href);
const initApplianceID = windowURLAppliance.searchParams.get('appliance') || '0';

class Appliance {
  constructor(id) {
    assert.equal(typeof id, 'string');
    this.id = id;
    this.devices = []; // formerly allDevices
    this.alerts = [];
    this.rings = {};
    this.users = {};
    this.networkConfig = {};
  }

  get devices() {
    return this._devices;
  }

  // Setting devices sets off a cascade of updates.
  set devices(val) {
    debug('set devices', val);
    assert(Array.isArray(val));
    this._devices = val;

    // First, organize by unique id.
    Vue.set(this, 'devicesByUniqID', keyBy(this._devices, 'uniqid'));

    // Next, Reorganize the data into:
    // { 'phone': [list of phones...], 'computer': [...] ... }
    //
    // Make sure all categories are present.
    const byCat = {};
    for (const c of DEVICE_CATEGORY_ALL) {
      byCat[c] = [];
    }

    this._devices.reduce((result, value) => {
      assert(value.category in byCat, `category ${value.category} is missing`);
      result[value.category].push(value);
      return result;
    }, byCat);
    Vue.set(this, 'devicesByCategory', byCat);

    // Index by ring
    const byRing = {};
    this._devices.reduce((result, value) => {
      if (result[value.ring] === undefined) {
        result[value.ring] = [];
      }
      result[value.ring].push(value);
      return result;
    }, byRing);
    Vue.set(this, 'devicesByRing', byRing);

    // Tabulate vulnerability counts and create vulnerability alerts for each
    // device
    const alerts = [];
    this._devices.forEach((device) => {
      const actives = pickBy(device.vulnerabilities, {active: true});
      Vue.set(device, 'activeVulnCount', Object.keys(actives).length);

      // Today all of the alerts we make are derived from the devices
      // list.  In the future, that could change.
      if (device.vulnerabilities) {
        for (const [vulnid, vulninfo] of Object.entries(device.vulnerabilities)) {
          alerts.push({
            'deviceID': device.uniqid,
            'vulnid': vulnid,
            'vulninfo': vulninfo,
          });
        }
      }
    });
    Vue.set(this, 'alerts', alerts);
    debug('set devices completed');
  }
}

function getAppliance(state, applianceID) {
  if (state.appliances[applianceID] === undefined) {
    // Using Vue.set here is super important because we're adding the
    // appliance as a new property of state.appliances, and we need
    // it to be reactive.
    Vue.set(state.appliances, applianceID, new Appliance(applianceID));
  }
  return state.appliances[applianceID];
}

const initAppliance = new Appliance(initApplianceID);
const state = {
  appMode: 'cloud',
  testAppMode: 'automatic',
  loggedIn: false,
  fakeLogin: false,
  mock: false,
  leftPanelVisible: false,
  localAppliance: true,
  applianceIDs: [],
  sites: {},
  appliances: {
    [initApplianceID]: initAppliance,
  },
  currentApplianceID: initApplianceID,
  currentAppliance: initAppliance,
};

const mutations = {
  setApplianceIDs(state, newIDs) {
    debug('setApplianceIDs, newIDs', newIDs);
    if (newIDs.length === 1 && newIDs[0] === '0') {
      state.appMode = 'appliance';
    } else {
      state.appMode = 'cloud';
    }
    if (state.testAppMode === 'cloud') {
      // need to make a copy, as when mocking we get back a singleton
      newIDs = newIDs.concat(['OtherAppliance']);
    }
    state.applianceIDs = newIDs;
    state.localAppliance = state.applianceIDs.length === 1 && state.applianceIDs[0] === '0';
    state.applianceIDs.forEach((applianceID) => {
      // Will create as needed
      getAppliance(state, applianceID);
    });
    if (state.applianceIDs.length === 1) {
      state.currentApplianceID = state.applianceIDs[0];
      state.currentAppliance = state.appliances[state.currentApplianceID];
    }
    const newSites = {};
    for (const id of newIDs) {
      debug(`adding ${id}`);
      newSites[id] = {
        uniqid: id,
        name: id === '0' ? 'Local Appliance' : id,
      };
    }
    debug(`newSites is now this: ${newSites}`, newSites);
    state.sites = newSites;
  },

  setCurrentApplianceID(state, newID) {
    // getAppliance will create as needed; maybe in the future this code
    // should instead check for the appliance existing, and fail if not?
    getAppliance(state, newID);
    state.currentApplianceID = newID;
    state.currentAppliance = state.appliances[state.currentApplianceID];
  },

  setApplianceDevices(state, {id, devices}) {
    getAppliance(state, id).devices = devices;
  },

  setApplianceRings(state, {id, rings}) {
    getAppliance(state, id).rings = rings;
  },

  setApplianceNetworkConfig(state, {id, networkConfig}) {
    getAppliance(state, id).networkConfig = networkConfig;
  },

  setApplianceUsers(state, {id, users}) {
    assert(users);
    debug('setApplianceUsers', id, users);
    Vue.set(getAppliance(state, id), 'users', users);
  },

  setApplianceUser(state, {id, user}) {
    assert(user.UUID);
    getAppliance(state, id).users[user.UUID] = user;
  },

  setLoggedIn(state, newValue) {
    state.loggedIn = newValue;
  },

  setTestAppMode(state, newMode) {
    state.testAppMode = newMode;
  },

  setMock(state, newValue) {
    state.mock = newValue;
    if (state.mock) {
      applianceApi.enableMock();
    } else {
      applianceApi.disableMock();
    }
  },

  setFakeLogin(state, newValue) {
    state.fakeLogin = newValue;
  },

  setLeftPanelVisible(state, newValue) {
    state.leftPanelVisible = newValue;
  },
};

const getters = {
  loggedIn: (state) => state.loggedIn || state.fakeLogin,
  fakeLogin: (state) => state.fakeLogin,
  mock: (state) => state.mock,
  localAppliance: (state) => state.localAppliance,
  currentApplianceID: (state) => state.currentApplianceID,
  applianceIDs: (state) => state.applianceIDs,
  leftPanelVisible: (state) => state.leftPanelVisible,

  applianceAlerts: (state) => (applianceID) => {
    return getAppliance(state, applianceID).alerts;
  },
  alerts: (state) => {
    return state.currentAppliance.alerts;
  },

  applianceDevices: (state) => (applianceID) => {
    return getAppliance(state, applianceID).devices;
  },
  devices: (state) => {
    return state.currentAppliance.devices;
  },

  applianceDeviceByUniqID: (state) => (applianceID, uniqid) => {
    return getAppliance(state, applianceID).devicesByUniqID[uniqid];
  },
  deviceByUniqID: (state) => (uniqid) => {
    return state.currentAppliance.devicesByUniqID[uniqid];
  },

  appMode: (state) => {
    if (state.testAppMode !== 'automatic') {
      return state.testAppMode;
    }
    return state.appMode;
  },

  testAppMode: (state) => {
    return state.testAppMode;
  },

  applianceDevicesByCategory: (state) => (applianceID, category) => {
    return getAppliance(state, applianceID).devicesByCategory[category];
  },
  devicesByCategory: (state) => (category) => {
    return state.currentAppliance.devicesByCategory[category];
  },

  applianceDevicesByRing: (state) => (applianceID, ring) => {
    return getAppliance(state, applianceID).devicesByRing[ring] || [];
  },
  devicesByRing: (state) => (ring) => {
    return state.currentAppliance.devicesByRing[ring] || [];
  },

  applianceNetworkConfig: (state) => (applianceID) => {
    return getAppliance(state, applianceID).networkConfig;
  },
  networkConfig: (state) => {
    return state.currentAppliance.networkConfig;
  },

  applianceRings: (state) => (applianceID) => {
    return getAppliance(state, applianceID).rings;
  },
  rings: (state) => {
    return state.currentAppliance.rings;
  },

  applianceUsers: (state) => (applianceID) => {
    return getAppliance(state, applianceID).users;
  },
  users: (state) => {
    return state.currentAppliance.users;
  },

  applianceUserByUUID: (state) => (applianceID, uuid) => {
    return getAppliance(state, applianceID).users[uuid];
  },
  userByUUID: (state) => (uuid) => {
    return state.currentAppliance.users[uuid];
  },

  sites: (state) => {
    return state.sites;
  },
  siteByUniqID: (state) => (uniqid) => {
    return state.sites[uniqid];
  },

  // device utility functions
  // XXX since these don't reference state explicitly, they should move to
  // a library, probably.
  deviceCount: (state) => (devices) => {
    assert(Array.isArray(devices), 'expected devices to be array');
    return devices.length;
  },
  deviceActive: (state) => (devices) => {
    return filter(devices, {active: true});
  },
  deviceVulnScanned: (state) => (devices) => {
    return filter(devices, 'scans.vulnerability.finish');
  },
  deviceVulnerable: (state) => (devices) => {
    return filter(devices, 'activeVulnCount');
  },
  deviceNotVulnerable: (state) => (devices) => {
    return filter(devices, {activeVulnCount: 0});
  },

  // alert utility functions
  alertCount: (state) => (alerts) => {
    assert(typeof(alerts) === 'object' && !Array.isArray(alerts), 'expected alerts to be object');
    return Object.keys(alerts).length;
  },
  alertActive: (state) => (alerts) => {
    return pickBy(alerts, {vulninfo: {active: true}});
  },
  alertInactive: (state) => (alerts) => {
    return pickBy(alerts, {vulninfo: {active: false}});
  },
  alertByRing: (state) => (ring, alerts) => {
    return pickBy(alerts, {device: {ring: ring}});
  },

  // user utility functions
  userCount: (state) => (users) => {
    assert(typeof(users) === 'object' && !Array.isArray(users), 'expected users to be object');
    return Object.keys(users).length;
  },
};

// Take an API device and transform it for local use.
// Much of this is legacy and could be fixed.
function computeDeviceProps(apiDevice) {
  const device = {
    manufacturer: apiDevice.Manufacturer,
    model: apiDevice.Model,
    kind: apiDevice.Kind,
    confidence: apiDevice.Confidence,
    networkName: apiDevice.HumanName ? apiDevice.HumanName : `Unknown (${apiDevice.HwAddr})`, // XXX this has issues, including i18n)
    ipv4Addr: apiDevice.IPv4Addr,
    osVersion: apiDevice.OSVersion,
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
    debug('Store: fetchApplianceIDs');
    const ids = await applianceApi.appliancesGet();
    debug('Store: fetchApplianceIDs got', ids);
    context.commit('setApplianceIDs', ids);
  },

  async setCurrentApplianceID(context, {id}) {
    context.commit('setCurrentApplianceID', id);
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
    const id = context.state.currentApplianceID;
    fetchDevicesPromise = retry(applianceApi.applianceDevicesGet, {
      interval: RETRY_DELAY,
      max_tries: 5, // eslint-disable-line camelcase
      args: [id],
    }).then((apiDevices) => {
      devices = apiDevices.map(computeDeviceProps);
      context.commit('setApplianceDevices', {id: id, devices: devices});
    }).tapCatch((err) => {
      debug('Store: fetchDevices failed', err);
    });
    return fetchDevicesPromise;
  },

  // Start a timer-driven periodic fetch of devices
  fetchPeriodic(context) {
    if (fetchPeriodicTimeout !== null) {
      clearTimeout(fetchPeriodicTimeout);
      fetchPeriodicTimeout = null;
    }
    // if not logged in, just come back later
    if (!context.getters.loggedIn) {
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
    context.commit('setApplianceRings', {id: id, rings: rings});
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
    context.commit('setApplianceNetworkConfig', {id: id, networkConfig: nc});
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
    context.commit('setApplianceUsers', {id: id, users: users});
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
      context.commit('setApplianceUser', {id: id, user: postUser});
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
    context.dispatch('fetchApplianceIDs').then(() => {
      context.dispatch('fetchDevices');
      context.dispatch('fetchRings');
      context.dispatch('fetchUsers');
      context.dispatch('fetchPeriodic');
    });
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
