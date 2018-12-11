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
const LOCAL_APPLIANCE_ID = '0';

const windowURLAppliance = window && window.location && window.location.href && new URL(window.location.href);
const initApplianceID = windowURLAppliance.searchParams.get('appliance') || LOCAL_APPLIANCE_ID;

class Appliance {
  constructor(id) {
    assert.equal(typeof id, 'string');
    this.id = id;
    this.regInfo = {}; // registry Information
    if (this.id === LOCAL_APPLIANCE_ID) {
      this.regInfo = {
        uuid: LOCAL_APPLIANCE_ID,
        name: 'Local Appliance',
      };
    }
    this.devices = [];
    this.alerts = [];
    this.rings = {};
    this.users = {};
    this.networkConfig = {};
  }

  get name() {
    return this.regInfo.name ? this.regInfo.name : this.id;
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

function computeAppMode(state) {
  return state.testAppMode === 'automatic' ? state.appMode : state.testAppMode;
}

const initAppliance = new Appliance(initApplianceID);
const state = {
  appMode: 'cloud',
  testAppMode: 'automatic',
  loggedIn: false,
  fakeLogin: false,
  mock: false,
  leftPanelVisible: false,
  appliances: {
    [initApplianceID]: initAppliance,
  },
  currentApplianceID: initApplianceID,
  currentAppliance: initAppliance,
};

const mutations = {
  setAppliances(state, newAppliances) {
    debug('setAppliances, newAppliances', newAppliances);
    assert(Array.isArray(newAppliances));
    if (newAppliances.length === 1 && newAppliances[0].uuid === LOCAL_APPLIANCE_ID) {
      state.appMode = 'appliance';
    } else {
      state.appMode = 'cloud';
    }
    newAppliances.forEach((val) => {
      // Will create as needed
      assert(typeof val === 'object');
      assert(val.name !== undefined);
      assert(val.uuid !== undefined);
      const appliance = getAppliance(state, val.uuid);
      appliance.regInfo = val;
    });
    // If there's only one appliance, default to it.
    if (newAppliances.length === 1) {
      state.currentApplianceID = newAppliances[0].uuid;
      state.currentAppliance = state.appliances[state.currentApplianceID];
    }
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

  setVulnRepair(state, {id, deviceID, vulnID, value}) {
    const app = getAppliance(state, id);
    const vuln = app && app.devicesByUniqID && app.devicesByUniqID[deviceID] &&
      app.devicesByUniqID[deviceID].vulnerabilities &&
      app.devicesByUniqID[deviceID].vulnerabilities[vulnID];
    if (!vuln) {
      debug('failed to get vulnerability', id, deviceID, vulnID);
      return;
    }
    Vue.set(vuln, 'repair', true);
  },

  setTestAppMode(state, newMode) {
    state.testAppMode = newMode;
  },

  setMock(state, newValue) {
    state.mock = newValue;
    debug('setMock', newValue, computeAppMode(state));
    if (state.mock) {
      if (computeAppMode(state) === 'cloud') {
        applianceApi.setMockMode(applianceApi.MOCKMODE_CLOUD);
      } else {
        applianceApi.setMockMode(applianceApi.MOCKMODE_APPLIANCE);
      }
    } else {
      applianceApi.setMockMode(applianceApi.MOCKMODE_NONE);
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
  currentApplianceID: (state) => state.currentApplianceID,
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
    return computeAppMode(state);
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
    return state.appliances; // hack for now
  },
  siteByID: (state) => (id) => {
    return state.appliances[id]; // hack for now
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
  async fetchAppliances(context) {
    debug('Store: fetchAppliances');
    const appliances = await applianceApi.appliancesGet();
    debug('Store: fetchAppliances got', appliances);
    context.commit('setAppliances', appliances);
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

  // Ask the server to repair a vulnerability by setting the appropriate
  // property.
  async repairVuln(context, {deviceID, vulnID}) {
    assert(typeof deviceID === 'string');
    assert(typeof vulnID === 'string');

    debug(`repairVuln: ${deviceID} ${vulnID}`);
    const id = context.state.currentApplianceID;
    context.commit('setVulnRepair', {id: id, deviceID: deviceID, vulnID: vulnID, value: true});
    try {
      await applianceApi.applianceConfigSet(id, `@/clients/${deviceID}/vulnerabilities/${vulnID}/repair`, 'true');
    } catch (err) {
      debug('failed to set repair bit', err);
    } finally {
      context.dispatch('fetchDevices');
    }
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
    context.dispatch('fetchAppliances').then(() => {
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
