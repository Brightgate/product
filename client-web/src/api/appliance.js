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

import axiosMod from 'axios';
import 'axios-debug-log'; // For side-effect
import qs from 'qs';

import retry from 'bluebird-retry';
import Debug from 'debug';
import makeAxiosMock from './appliance_mock';

const normalAxios = axiosMod.create({
  timeout: 5000,
});
let mockAxios = null;

let axios = normalAxios;
// Get a property's value
function enableMock() {
  if (mockAxios === null) {
    mockAxios = makeAxiosMock(normalAxios);
  }
  axios = mockAxios;
}

function disableMock() {
  axios = normalAxios;
}

const debug = Debug('api/appliance');

const RETRY_DELAY = 1000;

const urlPrefix = '';
function buildUrl(u) {
  return urlPrefix + u;
}

// Get a property's value
async function applianceConfigGet(applianceID, property, default_value) {
  assert.equal(typeof applianceID, 'string');
  assert.equal(typeof property, 'string');
  assert(default_value === undefined || typeof default_value === 'string');

  const u = buildUrl(`/api/appliances/${applianceID}/config?${property}`);
  debug(`applianceConfigGet(${applianceID}, ${property}, ${default_value})`);
  let val = null;
  try {
    const resp = await axios.get(u);
    val = resp.data;
  } catch (err) {
    if (default_value === undefined) {
      throw err;
    } else {
      debug(`applianceConfigGet(${applianceID}, ${property}): defaulting to ${default_value}`);
      val = default_value;
    }
  }
  debug(`applianceConfigGet(${applianceID}, ${property}): returning`, val);
  return val;
}

// Set a simple property to the specified value.
async function applianceConfigSet(applianceID, property, value) {
  assert.equal(typeof applianceID, 'string');
  assert.equal(typeof property, 'string');
  assert.equal(typeof value, 'string');

  const u = buildUrl(`/api/appliances/${applianceID}/config`);
  debug(`applianceConfigSet: POST ${u} ${property}=${value}`);
  const data = {[property]: value};
  try {
    await axios({
      method: 'POST',
      headers: {'content-type': 'application/x-www-form-urlencoded'},
      data: qs.stringify(data),
      url: u,
    });
    debug(`applianceConfigSet: set ${property} = ${value}.`);
  } catch (err) {
    debug(`applianceConfigSet: Error ${err}`);
    throw err;
  }
  return;
}

async function applianceConfigMustEqual(applianceID, property, expected) {
  assert.equal(typeof applianceID, 'string');
  assert.equal(typeof property, 'string');
  assert.equal(typeof expected, 'string');

  const val = await applianceConfigGet(applianceID, property);
  if (val !== expected) {
    throw new Error(`applianceConfigMustEqual(${applianceID}, ${property}, ${expected}) != ${val}`);
  }
  return true;
}

// Make repeated attempts to see if property has changed to an expected value.
async function applianceConfigWaitProp(applianceID, property, expected) {
  assert.equal(typeof applianceID, 'string');
  assert.equal(typeof property, 'string');
  assert.equal(typeof expected, 'string');

  const maxTries = 10;
  try {
    await retry(applianceConfigMustEqual, {
      interval: RETRY_DELAY,
      max_tries: maxTries,
      throw_original: true,
      args: [applianceID, property, expected],
    });
    debug(`applianceConfigWaitProp: saw ${property} become ${expected}`);
  } catch (err) {
    throw new Error(`Did not see property change.  Last error was: ${err}`);
  }
}

async function appliancesGet() {
  const u = buildUrl('/api/appliances');
  const res = await axios.get(u);
  if (res.data === undefined || res.data === null || typeof res.data !== 'object') {
    throw new Error(`Saw incomplete or bad GET ${u} response.`);
  }
  return res.data;
}

async function commonApplianceGet(applianceID, suffix) {
  assert.equal(typeof applianceID, 'string');
  assert.equal(typeof suffix, 'string');

  const u = buildUrl(`/api/appliances/${applianceID}/${suffix}`);
  const res = await axios.get(u);
  const data = res.data;
  if (data === undefined || data === null || typeof data !== 'object') {
    throw new Error(`Saw incomplete or bad GET ${u} response.`);
  } else {
    return data;
  }
}

// Load the list of devices from the server.
async function applianceDevicesGet(applianceID) {
  assert.equal(typeof applianceID, 'string');

  const res = await commonApplianceGet(applianceID, 'devices');
  if (res.Devices === null) {
    return [];
  }
  assert.equal(typeof res.Devices, 'object');
  return res.Devices;
}

// Load the list of rings from the server.
async function applianceRingsGet(applianceID) {
  assert.equal(typeof applianceID, 'string');

  return await commonApplianceGet(applianceID, 'rings');
}

// Ask the server to change the ring property for a device, then
// attempt to wait for that change to propagate.  In practice this
// seems to take several seconds, during which time the server may
// become unreachable; thus we use retrys to make things work properly.
async function applianceClientsRingSet(applianceID, deviceID, newRing) {
  assert.equal(typeof applianceID, 'string');
  assert.equal(typeof deviceID, 'string');
  assert.equal(typeof newRing, 'string');

  const propName = `@/clients/${deviceID}/ring`;
  debug(`applianceClientsRingSet: ${propName} -> ${newRing}`);
  await applianceConfigSet(applianceID, propName, newRing);
  await applianceConfigWaitProp(applianceID, propName, newRing);
}

// Load the list of users from the server.
async function applianceUsersGet(applianceID) {
  assert.equal(typeof applianceID, 'string');

  const res = await commonApplianceGet(applianceID, 'users');
  assert(res.Users && typeof res.Users === 'object');
  return res.Users;
}

// Update or create user on server
async function applianceUsersPost(applianceID, userInfo, newUser) {
  assert.equal(typeof applianceID, 'string');
  assert.equal(typeof userInfo, 'object');
  assert.equal(typeof newUser, 'boolean');

  const uid = newUser ? 'NEW' : userInfo.UUID;
  const u = buildUrl(`/api/appliances/${applianceID}/users/${uid}`);
  debug(`applianceUsersPost ${u}`, userInfo);
  const res = await axios.post(u, userInfo);
  assert(typeof res.data === 'object');
  return res.data;
}

async function applianceUsersDelete(applianceID, userID) {
  assert.equal(typeof applianceID, 'string');
  assert.equal(typeof userID, 'string');

  const u = buildUrl(`/api/appliances/${applianceID}/users/${userID}`);
  debug(`applianceUsersDelete ${u}`, userID);
  await axios.delete(u);
  return;
}

async function applianceEnrollGuest(applianceID, {type, phone, email}) {
  assert.equal(typeof applianceID, 'string');
  assert.equal(typeof type, 'string');
  assert.equal(typeof phone, 'string');
  assert(email === undefined || typeof email === 'string');

  const args = {type, phone, email};
  const u = buildUrl(`/api/appliances/${applianceID}/enroll_guest`);
  debug(`applianceEnrollGuest ${u}`, args);
  const res = await axios({
    method: 'POST',
    headers: {'content-type': 'application/x-www-form-urlencoded'},
    data: qs.stringify(args),
    url: u,
  });
  debug('enroll res', res.data);
  return res.data;
}

async function authApplianceLogin(uid, userPassword) {
  assert.equal(typeof uid, 'string');
  assert.equal(typeof userPassword, 'string');

  const u = buildUrl('/auth/appliance/login');
  const data = {uid, userPassword};
  try {
    await axios({
      method: 'POST',
      headers: {'content-type': 'application/x-www-form-urlencoded'},
      data: qs.stringify(data),
      url: u,
    });
    debug(`authApplianceLogin: Logged in as ${uid}.`);
  } catch (err) {
    debug(`authApplianceLogin: Failed login as ${uid}.`, err);
    throw err;
  }
}

async function applianceSupreme(applianceID) {
  assert.equal(typeof applianceID, 'string');

  const u = buildUrl(`/api/appliances/${applianceID}/supreme`);
  const res = await axios.get(u);
  return res.data;
}

async function authApplianceLogout() {
  const u = buildUrl('/auth/logout');
  try {
    await axios.get(u);
  } catch (err) {
    debug('authApplianceLogout failed', err);
    throw err;
  }
}

async function authUserid() {
  const u = buildUrl('/auth/userid');
  try {
    await axios.get(u);
  } catch (err) {
    debug('authUserid failed', err);
    throw err;
  }
}

export default {
  applianceConfigGet,
  applianceConfigSet,
  applianceConfigWaitProp,
  appliancesGet,
  applianceDevicesGet,
  applianceRingsGet,
  applianceClientsRingSet,
  applianceUsersGet,
  applianceUsersPost,
  applianceUsersDelete,
  applianceEnrollGuest,
  applianceSupreme,
  authApplianceLogin,
  authApplianceLogout,
  authUserid,
  enableMock,
  disableMock,
};
