import Vue from 'vue'
import Vuex from 'vuex'

import superagent from 'superagent'
import assert from "assert"

import { mockDevices } from "./mock_devices.js"
import _ from "lodash"
import util from "util"
import Promise from "bluebird"

Vue.use(Vuex)

// XXX this needs to get replaced with constants from devices.json
const device_category_all = ['recent', 'phone', 'computer', 'media', 'iot']

// Developer switch, for now; this would be better as a part of the store itself
const enable_mock = true

var initDevices
if (enable_mock) {
  initDevices = {
     by_uniqid: _.keyBy(mockDevices, 'uniqid'),
     categories: {}
  }
} else {
  initDevices = {
     by_uniqid: {},
     categories: {}
  }
}
makeDeviceCategories(initDevices)

const STD_TIMEOUT = {
  response: 500,
  deadline: 5000
}
const RETRY_DELAY = 1000

const state = {
  devices: _.cloneDeep(initDevices),
  deviceCount: Object.keys(initDevices.by_uniqid).length,
  rings: []
};

const mutations = {
  setDevices (state, newDevices) {
    state.devices = newDevices;
    state.deviceCount = Object.keys(newDevices.by_uniqid).length
  },

  setRings (state, newRings) {
    state.rings = newRings;
  },
}

const getters = {
  Device_By_UniqID: (state) => (uniqid) => {
    return state.devices.by_uniqid[uniqid]
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
function devicesGetP(maxcount, count) {
  assert.equal(typeof maxcount, "number")
  count = count === undefined ? maxcount : count;
  assert.equal(typeof count, "number")

  const attempt = (maxcount - count) + 1
  const attempt_str = `#${attempt}/${maxcount}`
  console.log(`devicesGetP: GET /devices try ${attempt_str}`)

  return superagent.get('/apid/devices'
  ).timeout(STD_TIMEOUT
  ).then((res) => {
    console.log("devicesGetP: got response")
    if (res.body === null ||
        (typeof res.body !== "object") ||
        !("Devices" in res.body) ||
        res.body["Devices"] === null) {
      // throw down to our catch handler below, to cause retry or give up.
      throw new Error("Saw incomplete or bad GET /devices response.")
    } else {
      return res.body
    }
  }).catch((err) => {
    if (count === 0) {
      throw new Error(`devicesGetP: failed ${maxcount} times.  Final error was: ${err}`)
    }
    console.log(`devicesGetP: failed ${attempt_str}, will retry.  ${err}`)
    return Promise.delay(RETRY_DELAY
    ).then(() => { return devicesGetP(maxcount, count - 1) })
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
    return devicesGetP(10).then((res_json) => {

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

      devices.by_uniqid = _.keyBy(mapped_devices, 'uniqid')
    }).finally(() => {
      if (enable_mock) {
        _.defaults(devices.by_uniqid,  _.keyBy(mockDevices, 'uniqid'))
      }
    }).then(() => {
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
    console.log(`changeRing: ${deviceUniqID} -> ${newRing}`)
    const propname = `@/clients/${deviceUniqID}/ring`
    return context.dispatch('setConfigProp', {
        property: propname,
	value: newRing
    }).then(() => {
      return checkPropChangeP(propname, newRing, 10)
    }).then(() => {
      return context.dispatch('fetchDevices')
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
