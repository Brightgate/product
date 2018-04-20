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

import _ from 'lodash';
import Promise from 'bluebird';
import superagent from 'superagent-bluebird-promise';
import retry from 'bluebird-retry';
import Vue from 'vue';
import Vuex from 'vuex';

import mockDevicesRef from './mock_devices';
import mockUsers from './mock_users';

// Run mockDevices through our property deriver
const mockDevices = _.map(mockDevicesRef, computeDeviceProps);

Vue.use(Vuex);

// XXX this needs further rationalization with devices.json
const device_category_all = ['recent', 'phone', 'computer', 'printer', 'media', 'iot', 'unknown'];

const mockNetworkConfig = {
  ssid: 'mockSSID',
  dnsServer: '1.1.1.1:53',
  defaultRing: 'standard',
};

const mockRings = [
  'core',
  'standard',
  'devices',
  'quarantine',
];

// Determines whether mock devices are enabled or disabled by default
const enableMock = false;

let initUsers = {};
let initNetworkConfig = {};
let initDevlist = [];
if (enableMock) {
  initDevlist = mockDevices;
  initUsers = _.cloneDeep(mockUsers);
  initNetworkConfig = _.cloneDeep(mockNetworkConfig);
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
  devices: _.cloneDeep(initDevices),
  alerts: initAlerts,
  rings: [],
  users: _.cloneDeep(initUsers),
  networkConfig: _.cloneDeep(initNetworkConfig),
  enableMock: enableMock,
};

const mutations = {
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
    console.log(`setLoggedIn: now ${newLoggedIn}`);
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

  Device_By_UniqID: (state) => (uniqid) => {
    return state.devices.by_uniqid[uniqid];
  },

  All_Devices: (state) => {
    return state.devices.all_devices;
  },
  Device_Count: (state) => (devices) => {
    return _.size(devices);
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
    return _.filter(devices, {active: true});
  },

  Device_VulnScanned: (state) => (devices) => {
    return _.filter(devices, 'scans.vulnerability.finish');
  },

  Device_Vulnerable: (state) => (devices) => {
    return _.filter(devices, 'activeVulnCount');
  },

  Device_NotVulnerable: (state) => (devices) => {
    return _.filter(devices, {activeVulnCount: 0});
  },

  All_Alerts: (state) => {return state.alerts;},

  Alert_Count: (state) => (alerts) => {return _.size(alerts);},

  Alert_Active: (state) => (alerts) => {
    return _.pickBy(alerts, {vulninfo: {active: true}});
  },

  Alert_Inactive: (state) => (alerts) => {
    return _.pickBy(alerts, {vulninfo: {active: false}});
  },

  Alert_By_Ring: (state) => (ring, alerts) => {
    return _.pickBy(alerts, {device: {ring: ring}});
  },

  Rings: (state) => {return state.rings;},

  All_Users: (state) => {return state.users;},
  User_Count: (state) => (users) => {return _.size(users);},

  User_By_UUID: (state) => (uuid) => {return state.users[uuid];},

  Network_Config: (state) => {return state.networkConfig;},

  Mock: (state) => {return state.enableMock;},
};

// Get a property's value
function fetchPropP(property, default_value) {
  console.log(`fetchPropP(${property}, ${default_value})`);
  return superagent.get('/apid/config'
  ).query(property
  ).timeout(STD_TIMEOUT
  ).then((res) => {
    console.log(`fetchPropP(${property}): returning ${res.body}`);
    return res.body;
  }).catch((err) => {
    if (default_value !== undefined) {
      console.log(`fetchPropP(${property}): defaulting`);
      return Promise.resolve(default_value);
    }
    throw err;
  });
}

// Make up to maxcount attempts to see if property has changed to an
// expected value.
function checkPropChangeP(property, value, maxcount, count) {
  assert.equal(typeof property, 'string');
  assert.equal(typeof maxcount, 'number');
  count = count === undefined ? maxcount : count;
  assert.equal(typeof count, 'number');

  const attempt_str = `#${(maxcount - count) + 1}`;
  console.log(`checkPropChangeP: waiting for ${property} to become ${value}, try ${attempt_str}`);
  return superagent.get('/apid/config'
  ).query(property
  ).timeout(STD_TIMEOUT
  ).then((res) => {
    if (res.body !== value) {
      throw new Error(`Tried ${maxcount} times but did not see property change`);
    }
    console.log(`checkPropChangeP: I saw ${property} -> ${value}`);
    return value;
  }).catch((err) => {
    if (count === 0) {
      throw new Error(`Couldn't see property change after ${maxcount} tries.  Last error was: ${err}`);
    }
    console.log(`checkPropChangeP: failed ${attempt_str}, will retry.  ${err}`);
    return Promise.delay(RETRY_DELAY
    ).then(() => {
      return checkPropChangeP(property, value, maxcount, count - 1);
    });
  });
}

// Make up to maxcount attempts to load the devices configuration object
function devicesGetP() {
  console.log('devicesGetP: GET /devices');
  return superagent.get('/apid/devices'
  ).timeout(STD_TIMEOUT
  ).then((res) => {
    console.log('devicesGetP: got response');
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
  assert(_.isArray(all_devices));
  const devices = {
    all_devices: all_devices,
    by_uniqid: {},
    by_category: {},
    by_ring: {},
  };

  // First, organize by unique id.
  devices.by_uniqid = _.keyBy(devices.all_devices, 'uniqid');

  // Next, Reorganize the data into:
  // { 'phone': [list of phones...], 'computer': [...] ... }
  //
  // Make sure all categories are present.
  devices.by_category = {};
  for (const c of device_category_all) {
    devices.by_category[c] = [];
  }

  _.reduce(devices.all_devices, (result, value) => {
    assert(value.category in devices.by_category, `category ${value.category} is missing`);
    result[value.category].push(value);
    return result;
  }, devices.by_category);

  // Index by ring
  _.reduce(devices.all_devices, (result, value) => {
    if (result[value.ring] === undefined) {
      result[value.ring] = [];
    }
    result[value.ring].push(value);
    return result;
  }, devices.by_ring);

  // Tabulate vulnerability counts for each device
  _.forEach(devices.all_devices, (device) => {
    device.activeVulnCount = _.size(_.pickBy(device.vulnerabilities, {active: true}));
  });

  console.log(devices);
  return devices;
}

// Today all of the alerts we make are derived from the devices
// list.  In the future, that could change.
function makeAlerts(devices) {
  const alerts = [];

  _.forEach(devices.by_uniqid, (device, uniqid) => {
    _.forEach(device.vulnerabilities, (vulninfo, vulnid) => {
      alerts.push({
        'device': device,
        'vulnid': vulnid,
        'vulninfo': vulninfo,
      });
    });
  });
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
  // Load the list of devices from the server.  merge it with mock devices
  // defined locally (if using).
  fetchDevices(context) {
    console.log('Store: fetchDevices');
    // Join callers so that only one fetch is ongoing
    if (fetchDevicesPromise.isPending()) {
      console.log('Store: fetchDevices (pending)');
      return fetchDevicesPromise;
    }
    let all_devices = [];
    if (context.state.enableMock) {
      console.log(`Store: fetchDevices: enableMock = ${context.state.enableMock}`);
      all_devices = mockDevices;
    }
    const p = retry(devicesGetP, {interval: RETRY_DELAY, max_tries: 10}
    ).then((res_json) => {
      const mapped_devices = _.map(res_json.Devices, (dev) => {
        return computeDeviceProps({
          manufacturer: dev.Manufacturer,
          model: dev.Model,
          kind: dev.Kind,
          confidence: dev.Confidence,
          network_name: dev.HumanName, // XXX
          ipv4_addr: dev.IPv4Addr,
          os_version: dev.OSVersion,
          owner: dev.OwnerName,
          owner_phone: dev.OwnerPhone,
          owner_email: '',
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
      console.log('Store: fetchDevices finished');
    });
    // make sure promise is a bluebird promise, so we can call isPending
    fetchDevicesPromise = Promise.resolve(p);
    return fetchDevicesPromise;
  },

  // Start a timer-driven periodic fetch of devices
  fetchPeriodic(context) {
    // if not logged in, just come back later
    if (!context.getters.Is_Logged_In) {
      console.log('fetchPeriodic: not logged in, later');
      fetchPeriodicTimeout = setTimeout(() => {
        context.dispatch('fetchPeriodic');
      }, 10000);
      return;
    }

    console.log('fetchPeriodic: dispatching fetchDevices');
    context.dispatch('fetchDevices'
    ).then(() => {
      fetchPeriodicTimeout = setTimeout(() => {
        context.dispatch('fetchPeriodic');
      }, 10000);
    }, () => {
      console.log('fetchPeriodic: failed, back in 30');
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
    console.log('fetchRings: GET /apid/rings');
    return superagent.get('/apid/rings'
    ).then((res) => {
      console.log('fetchRings: Succeeded: ', res.body);
      assert(typeof res.body === 'object');
      context.commit('setRings', res.body);
    }).catch((err) => {
      if (context.state.enableMock) {
        console.log('fetchRings: Using mocked rings');
        context.commit('setRings', mockRings);
        return;
      }
      console.log(`fetchRings: Error ${err}`);
      throw err;
    });
  },

  // Load the list of rings from the server.
  fetchNetworkConfig(context) {
    console.log('fetchNetworkConfig: GET /apid/config');
    return Promise.props({
      ssid: fetchPropP('@/network/ssid'),
      dnsServer: fetchPropP('@/network/dnsserver', ''),
      defaultRing: fetchPropP('@/network/default_ring', ''),
    }).catch((err) => {
      if (context.state.enableMock) {
        console.log('fetchNetworkConfig: Using mocked networkConfig');
        return mockNetworkConfig;
      } else {
        console.log(`fetchNetworkConfig: Error ${err}`);
        throw err;
      }
    }).then((netConfig) => {
      context.commit('setNetworkConfig', netConfig);
    });
  },

  // Set a simple property to the specified value.
  setConfigProp(context, {property, value}) {
    assert.equal(typeof property, 'string');
    console.log(`setConfigProp: POST /apid/config ${property}=${value}`);
    return superagent.post('/apid/config'
    ).type('form'
    ).send({[property]: value}
    ).then(() => {
      console.log(`setConfigProp: finished setting ${property} = ${value}.`);
    }).catch((err) => {
      console.log(`setConfigProp: Error ${err}`);
      throw err;
    });
  },

  enrollGuest(context, {type, phone, email}) {
    const args = {type, phone, email};
    console.log('enrollGuest', args);
    return superagent.post('/apid/enroll_guest'
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
    console.log(`changeRing: ${deviceUniqID} -> ${newRing}`);
    const propname = `@/clients/${deviceUniqID}/ring`;
    return context.dispatch('setConfigProp', {
      property: propname,
      value: newRing,
    }).then(() => {
      return checkPropChangeP(propname, newRing, 10);
    }).finally(() => {
      // let this run async?
      context.dispatch('fetchDevices');
    });
  },

  // Load the list of users from the server.
  fetchUsers(context) {
    const user_result = {};
    if (context.state.enableMock) {
      _.defaults(user_result, mockUsers);
    }
    console.log('fetchUsers: GET /apid/users');
    if (context.state.enableMock) {
      _.defaults(user_result, mockUsers);
    }
    return superagent.get('/apid/users'
    ).then((res) => {
      console.log('fetchUsers: Succeeded: ', res.body);
      assert(typeof res.body === 'object');
      assert(typeof res.body.Users === 'object');
      _.defaults(user_result, res.body.Users);
    }).finally(() => {
      context.commit('setUsers', user_result);
    }).catch((err) => {
      console.log(`fetchUsers: Error ${err}`);
      throw err;
    });
  },

  // Create or Update a user
  saveUser(context, {user, newUser}) {
    assert(typeof user === 'object');
    assert(typeof newUser === 'boolean');
    const action = newUser ? 'creating' : 'updating';
    console.log(`store.js saveUsers: ${action} ${user.UUID}`, user);
    const path = newUser ? '/apid/users/NEW' : `/apid/users/${user.UUID}`;
    if (newUser) {
      // Backend is strict about UUID
      delete user.UUID;
    }

    return superagent.post(path
    ).type('json'
    ).send(user
    ).then((res) => {
      console.log('saveUser: Succeeded: ', res.body);
      assert(typeof res.body === 'object');
      context.commit('updateUser', res.body);
    }).catch((err) => {
      console.log('err is', err);
      if (err.res && err.res.text) {
        throw new Error(`Failed to save user: ${err.res.text}`);
      } else {
        throw err;
      }
    });
  },

  deleteUser(context, {user, newUser}) {
    assert(typeof user === 'object');
    console.log(`store.js deleteUser: ${user.UUID}`, user);

    return superagent.delete(`/apid/users/${user.UUID}`
    ).then(() => {
      context.dispatch('fetchUsers');
    });
  },

  login(context, {uid, userPassword}) {
    assert.equal(typeof uid, 'string');
    assert.equal(typeof userPassword, 'string');
    return superagent.post('/apid/login'
    ).type('form'
    ).send({uid, userPassword}
    ).then(() => {
      console.log(`login: Logged in as ${uid}.`);
      context.commit('setLoggedIn', true);
      // Let these run async
      context.dispatch('fetchDevices');
      context.dispatch('fetchRings');
      context.dispatch('fetchUsers');
      context.dispatch('fetchPeriodic');
    }).catch((err) => {
      console.log(`login: Error ${err}`);
      throw err;
    });
  },

  logout(context) {
    console.log('logout: /apid/logout');
    return superagent.get('/apid/logout'
    ).then(() => {
      console.log('logout: Completed');
      context.commit('setLoggedIn', false);
      context.dispatch('fetchPeriodicStop');
    }).catch((err) => {
      console.log(`logout: Error ${err}`);
      throw err;
    });
  },
};

export default new Vuex.Store({
  strict: true, // for debugging only, expensive, see manual
  actions,
  state,
  getters,
  mutations,
});
