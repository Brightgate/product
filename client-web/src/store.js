/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */
import Vue from 'vue'
import Vuex from 'vuex'

import assert from "assert"

import { mockDevices } from "./mock_devices.js"
import { mockUsers } from "./mock_users.js"
import _ from "lodash"
import util from "util"
import Promise from "bluebird"
import superagent from "superagent-bluebird-promise"
import retry from "bluebird-retry"

Vue.use(Vuex)

// XXX this needs to get replaced with constants from devices.json
const device_category_all = ['recent', 'phone', 'computer', 'media', 'iot']

// Determines whether mock devices are enabled or disabled by default
const enable_mock = false

var initDevices
var initUsers
if (enable_mock) {
  initDevices = {
     by_uniqid: _.keyBy(mockDevices, 'uniqid'),
     categories: {}
  }
  initUsers = _.cloneDeep(mockUsers)
} else {
  initDevices = {
     by_uniqid: {},
     categories: {}
  }
  initUsers = {}
}
makeDeviceCategories(initDevices)

const STD_TIMEOUT = {
  response: 500,
  deadline: 5000
}
const RETRY_DELAY = 1000

const state = {
  loggedIn: false,
  devices: _.cloneDeep(initDevices),
  deviceCount: Object.keys(initDevices.by_uniqid).length,
  rings: [],
  users: _.cloneDeep(initUsers),
  userCount: Object.keys(initUsers).length,
  enable_mock: enable_mock
};

const mutations = {
  setDevices (state, newDevices) {
    state.devices = newDevices;
    state.deviceCount = Object.keys(newDevices.by_uniqid).length
  },

  setRings (state, newRings) {
    state.rings = newRings;
  },

  setUsers (state, newUsers) {
    state.users = newUsers;
    state.userCount = Object.keys(newUsers).length
  },

  updateUser (state, user) {
    assert(user.UUID)
    state.users[user.UUID] = user
  },

  setLoggedIn (state, newLoggedIn) {
    console.log(`setLoggedIn: now ${newLoggedIn}`);
    state.loggedIn = newLoggedIn;
  },

  toggleMock (state) {
    state.enable_mock = !state.enable_mock
  },
}

const getters = {
  Is_Logged_In: (state) => {
    return state.loggedIn
  },

  Device_By_UniqID: (state) => (uniqid) => {
    return state.devices.by_uniqid[uniqid]
  },

  All_Devices: (state) => {
    return state.devices.by_uniqid
  },

  Devices_By_Category: (state) => (category) => {
    const d = state.devices
    if (!(category in d.categories)) {
      return []
    }
    var x = _.map(d.categories[category], (uniqid) => {
      return d.by_uniqid[uniqid]
    })
    return x
  },

  NumUniqIDs_By_Category: (state) => (category) => {
    const d = state.devices
    if (!(category in d.categories)) {
      return 0
    }
    return d.categories[category].length
  },

  Device_Count: (state) => {
    return state.deviceCount
  },

  Rings: (state) => {
      return state.rings
  },

  All_Users: (state) => {
    return state.users
  },

  User_By_UUID: (state) => (uuid) => {
    return state.users[uuid]
  },

  Mock: (state) => {
      return state.enable_mock
  },
}

// Make up to maxcount attempts to see if property has changed to an
// expected value.
function checkPropChangeP(property, value, maxcount, count) {
  assert.equal(typeof property, "string")
  assert.equal(typeof maxcount, "number")
  count = count == undefined ? maxcount : count;
  assert.equal(typeof count, "number")

  const attempt_str = `#${(maxcount - count) + 1}`
  console.log(`checkPropChangeP: waiting for ${property} to become ${value}, try ${attempt_str}`)
  return superagent.get('/apid/config'
  ).query(property
  ).timeout(STD_TIMEOUT
  ).then((res) => {
    if (res.body !== value) {
      throw new Error(`Tried ${maxcount} times but did not see property change`)
    }
    console.log(`checkPropChangeP: I saw ${property} -> ${value}`)
    return value
  }).catch((err) => {
    if (count === 0) {
      throw new Error(`Couldn't see property change after ${maxcount} tries.  Last error was: ${err}`)
    }
    console.log(`checkPropChangeP: failed ${attempt_str}, will retry.  ${err}`)
    return Promise.delay(RETRY_DELAY
    ).then(() => {
      return checkPropChangeP(property, value, maxcount, count - 1)
    })
  })
}

// Make up to maxcount attempts to load the devices configuration object
function devicesGetP() {
  console.log(`devicesGetP: GET /devices`)
  return superagent.get('/apid/devices'
  ).timeout(STD_TIMEOUT
  ).then((res) => {
    console.log("devicesGetP: got response")
    if (res.body === null ||
        (typeof res.body !== "object") ||
        !("Devices" in res.body)) {
      throw new Error("Saw incomplete or bad GET /devices response.")
    } else {
      return res.body
    }
  })
}

function makeDeviceCategories(devices) {
    // Reorganize the data into:
    // { 'phone': [list of phones...], 'computer': [...] ... }
    //
    // Make sure all categories are present.
    devices.categories = {}
    for (var c of device_category_all) {
      devices.categories[c] = []
    }

    _.reduce(devices.by_uniqid, ((result, value) => {
      assert(value.category in devices.categories, `category ${value.category} is missing`)
      result[value.category].push(value.uniqid);
      return result;
    }), devices.categories)
    return devices
}

const actions = {
  // Load the list of devices from the server.  merge it with mock devices
  // defined locally (if using).
  fetchDevices (context) {
    console.log("Store: fetchDevices")
    var devices = {
        categories: {},
        by_uniqid: {},
    }
    if (context.state.enable_mock) {
      console.log(`Store: fetchDevices: enable_mock = ${context.state.enable_mock}`)
      _.defaults(devices.by_uniqid,  _.keyBy(mockDevices, 'uniqid'))
    }
    return retry(devicesGetP, { interval: RETRY_DELAY, max_tries: 10 }
    ).then((res_json) => {
      var mapped_devices = _.map(res_json.Devices, (dev) => {
        return {
          category: 'phone',
          device: dev.Manufacturer + " " + dev.Model,
          network_name: dev.HumanName, // XXX
          os_version: dev.OSVersion,
          owner: dev.OwnerName,
          owner_phone: dev.OwnerPhone,
          owner_email: '',
          activated: '',
          media: 'mobile-phone-1',
          uniqid: dev.HwAddr,
          hwaddr: dev.HwAddr,
          ring: dev.Ring,
        }
      })

      _.defaults(devices.by_uniqid, _.keyBy(mapped_devices, 'uniqid'))
    }).finally(() => {
      makeDeviceCategories(devices)
      context.commit('setDevices', devices)
    })
  },

  // Load the list of rings from the server.
  fetchRings (context) {
    console.log("fetchRings: GET /apid/rings")
    return superagent.get('/apid/rings'
    ).then((res) => {
      console.log("fetchRings: Succeeded: ", res.body);
      assert(typeof res.body === "object")
      context.commit('setRings', res.body)
    }).catch((err) => {
      console.log(`fetchRings: Error ${err}`)
      throw err
    })
  },

  // Set a simple property to the specified value.
  setConfigProp (context, { property, value }) {
    assert.equal(typeof property, "string")
    console.log(`setConfigProp: POST /apid/config ${property}=${value}`)
    return superagent.post('/apid/config'
    ).type('form'
    ).send({ [property]: value }
    ).then(() => {
      console.log(`setConfigProp: finished setting ${property} = ${value}.`)
    }).catch((err) => {
      console.log(`setConfigProp: Error ${err}`)
      throw err
    })
  },

  enrollSMS (context, { phone }) {
    console.log(`enrollSMS: phone:${phone}`)
    return superagent.post('/apid/enroll'
    ).type('form'
    ).send({ phone: phone })
  },

  // Ask the server to change the ring property for a device, then
  // attempt to wait for that change to propagate.  In practice this
  // seems to take several seconds, during which time the server may
  // become unreachable; thus we use retrys to make things work properly.
  changeRing (context, { deviceUniqID , newRing }) {
    assert.equal(typeof deviceUniqID, "string")
    assert.equal(typeof newRing, "string")
    console.log(`changeRing: ${deviceUniqID} -> ${newRing}`)
    const propname = `@/clients/${deviceUniqID}/ring`
    return context.dispatch('setConfigProp', {
        property: propname,
        value: newRing
    }).then(() => {
      return checkPropChangeP(propname, newRing, 10)
    }).finally(() => {
      // let this run async?
      context.dispatch('fetchDevices')
    })
  },

  // Load the list of users from the server.
  fetchUsers (context) {
    var user_result = {}
    if (context.state.enable_mock) {
      _.defaults(user_result, mockUsers)
    }
    console.log("fetchUsers: GET /apid/users")
    return superagent.get('/apid/users'
    ).then((res) => {
      console.log("fetchUsers: Succeeded: ", res.body);
      assert(typeof res.body === "object")
      assert(typeof res.body.Users === "object")
      _.defaults(user_result, res.body.Users)
    }).finally(() => {
      context.commit('setUsers', user_result)
    }).catch((err) => {
      console.log(`fetchUsers: Error ${err}`)
      throw err
    })
  },

  // Load the list of rings from the server.
  saveUser (context, { user }) {
    console.log("store.js saveUsers: stub implementation, updating: ", user.UID, user.UUID)
    return Promise.delay(1500).then(() => {
      context.commit('updateUser', user)
    })
  },

  login (context, { uid, userPassword }) {
    assert.equal(typeof uid, "string")
    assert.equal(typeof userPassword, "string")
    return superagent.post('/apid/login'
    ).type('form'
    ).send({ uid: uid, userPassword: userPassword }
    ).then(() => {
      console.log(`login: Logged in as ${uid}.`)
      context.commit("setLoggedIn", true)
    }).catch((err) => {
      console.log(`login: Error ${err}`)
      throw err
    })
  },

  logout (context) {
    console.log(`logout: `)
    return superagent.get('/apid/logout'
    ).then(() => {
      console.log(`logout: Completed`)
      context.commit("setLoggedIn", false)
    }).catch((err) => {
      console.log(`logout: Error ${err}`)
      throw err
    })
  },

}

export default new Vuex.Store({
  strict: true, // for debugging only, expensive, see manual
  actions,
  state,
  getters,
  mutations,
})
