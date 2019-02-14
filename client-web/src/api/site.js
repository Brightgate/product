/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
import appDefs from '../app_defs';
import makeAxiosMock from './site_mock';

const normalAxios = axiosMod.create({
  timeout: 5000,
});

let axios = normalAxios;
let mockMode = null;

const RETRY_DELAY = 1000;

const debug = Debug('api/site');

function setMockMode(mode) {
  assert([appDefs.APPMODE_NONE, appDefs.APPMODE_CLOUD, appDefs.APPMODE_LOCAL].includes(mode));
  if (mockMode === mode) {
    return;
  }
  if (mode === null) {
    axios = normalAxios;
  } else {
    axios = makeAxiosMock(normalAxios, mode);
  }
  mockMode = mode;
}

const urlPrefix = '';
function buildUrl(u) {
  return urlPrefix + u;
}

// Get a property's value
async function siteConfigGet(siteID, property, defaultValue) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof property, 'string');
  assert(defaultValue === undefined || typeof defaultValue === 'string');

  const u = buildUrl(`/api/sites/${siteID}/config?${property}`);
  debug(`siteConfigGet(${siteID}, ${property}, ${defaultValue})`);
  let val = null;
  try {
    const resp = await axios.get(u);
    val = resp.data;
  } catch (err) {
    if (defaultValue === undefined) {
      throw err;
    } else {
      debug(`siteConfigGet(${siteID}, ${property}): defaulting to ${defaultValue}`);
      val = defaultValue;
    }
  }
  debug(`siteConfigGet(${siteID}, ${property}): returning`, val);
  return val;
}

// Set a simple property to the specified value.
async function siteConfigSet(siteID, property, value) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof property, 'string');
  assert.equal(typeof value, 'string');

  const u = buildUrl(`/api/sites/${siteID}/config`);
  debug(`siteConfigSet: POST ${u} ${property}=${value}`);
  const data = {[property]: value};
  try {
    await axios({
      method: 'POST',
      headers: {'content-type': 'application/x-www-form-urlencoded'},
      data: qs.stringify(data),
      url: u,
    });
    debug(`siteConfigSet: set ${property} = ${value}.`);
  } catch (err) {
    debug(`siteConfigSet: Error ${err}`);
    throw err;
  }
  return;
}

async function siteConfigMustEqual(siteID, property, expected) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof property, 'string');
  assert.equal(typeof expected, 'string');

  const val = await siteConfigGet(siteID, property);
  if (val !== expected) {
    throw new Error(`siteConfigMustEqual(${siteID}, ${property}, ${expected}) != ${val}`);
  }
  return true;
}

// Make repeated attempts to see if property has changed to an expected value.
async function siteConfigWaitProp(siteID, property, expected) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof property, 'string');
  assert.equal(typeof expected, 'string');

  const maxTries = 10;
  try {
    await retry(siteConfigMustEqual, {
      interval: RETRY_DELAY,
      max_tries: maxTries, // eslint-disable-line camelcase
      throw_original: true, // eslint-disable-line camelcase
      args: [siteID, property, expected],
    });
    debug(`siteConfigWaitProp: saw ${property} become ${expected}`);
  } catch (err) {
    throw new Error(`Did not see property change.  Last error was: ${err}`);
  }
}

async function sitesGet() {
  const u = buildUrl('/api/sites');
  const res = await axios.get(u);
  if (res.data === undefined || res.data === null || typeof res.data !== 'object') {
    throw new Error(`Saw incomplete or bad GET ${u} response.`);
  }
  return res.data;
}

async function commonApplianceGet(siteID, suffix) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof suffix, 'string');

  const u = buildUrl(`/api/sites/${siteID}/${suffix}`);
  const res = await axios.get(u);
  const data = res.data;
  if (data === undefined || data === null || typeof data !== 'object') {
    throw new Error(`Saw incomplete or bad GET ${u} response.`);
  } else {
    return data;
  }
}

// Load the list of devices from the server.
async function siteDevicesGet(siteID) {
  assert.equal(typeof siteID, 'string');

  const res = await commonApplianceGet(siteID, 'devices');
  if (res.Devices === null) {
    return [];
  }
  assert.equal(typeof res.Devices, 'object');
  return res.Devices;
}

// Load the list of rings from the server.
async function siteRingsGet(siteID) {
  assert.equal(typeof siteID, 'string');

  return await commonApplianceGet(siteID, 'rings');
}

// Ask the server to change the ring property for a device, then
// attempt to wait for that change to propagate.  In practice this
// seems to take several seconds, during which time the server may
// become unreachable; thus we use retrys to make things work properly.
async function siteClientsRingSet(siteID, deviceID, newRing) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof deviceID, 'string');
  assert.equal(typeof newRing, 'string');

  const propName = `@/clients/${deviceID}/ring`;
  debug(`siteClientsRingSet: ${propName} -> ${newRing}`);
  await siteConfigSet(siteID, propName, newRing);
  await siteConfigWaitProp(siteID, propName, newRing);
}

// Load the list of users from the server.
async function siteUsersGet(siteID) {
  assert.equal(typeof siteID, 'string');

  const res = await commonApplianceGet(siteID, 'users');
  assert(res.Users && typeof res.Users === 'object');
  return res.Users;
}

// Update or create user on server
async function siteUsersPost(siteID, userInfo, newUser) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof userInfo, 'object');
  assert.equal(typeof newUser, 'boolean');

  const uid = newUser ? 'NEW' : userInfo.UUID;
  const u = buildUrl(`/api/sites/${siteID}/users/${uid}`);
  debug(`siteUsersPost ${u}`, userInfo);
  const res = await axios.post(u, userInfo);
  assert(typeof res.data === 'object');
  return res.data;
}

async function siteUsersDelete(siteID, userID) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof userID, 'string');

  const u = buildUrl(`/api/sites/${siteID}/users/${userID}`);
  debug(`siteUsersDelete ${u}`, userID);
  await axios.delete(u);
  return;
}

async function siteEnrollGuest(siteID, {type, phone, email}) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof type, 'string');
  assert.equal(typeof phone, 'string');
  assert(email === undefined || typeof email === 'string');

  const args = {type, phone, email};
  const u = buildUrl(`/api/sites/${siteID}/enroll_guest`);
  debug(`siteEnrollGuest ${u}`, args);
  const res = await axios({
    method: 'POST',
    headers: {'content-type': 'application/x-www-form-urlencoded'},
    data: qs.stringify(args),
    url: u,
  });
  debug('enroll res', res.data);
  return res.data;
}

async function authProviders() {
  const u = buildUrl('/auth/providers');
  try {
    const resp = await axios.get(u);
    return resp.data;
  } catch (err) {
    debug('authProviders failed', err);
    // UI can indicate that is not working
    return {
      mode: appDefs.APPMODE_FAILURE,
      providers: '',
    };
  }
}

async function authApplianceLogin(uid, userPassword) {
  assert.equal(typeof uid, 'string');
  assert.equal(typeof userPassword, 'string');

  const u = buildUrl('/auth/site/login');
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

async function siteSupreme(siteID) {
  assert.equal(typeof siteID, 'string');

  const u = buildUrl(`/api/sites/${siteID}/supreme`);
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
    const res = await axios.get(u);
    return res.data;
  } catch (err) {
    debug('authUserid failed', err);
    throw err;
  }
}

async function accountGeneratePassword() {
  const u = buildUrl('/api/account/0/passwordgen');
  try {
    const res = await axios.get(u);
    return res.data;
  } catch (err) {
    debug('passwordGen failed', err);
    throw err;
  }
}

async function accountSelfProvision(username, password, verifier) {
  const u = buildUrl('/api/account/0/selfprovision');
  try {
    const res = await axios({
      timeout: 20000,
      method: 'POST',
      headers: {'content-type': 'application/x-www-form-urlencoded'},
      data: qs.stringify({username, password, verifier}),
      url: u,
    });
    debug(`accountSelfProvision: Succeeded.`);
    return res.data;
  } catch (err) {
    debug(`accountSelfProvision: Failed`, err);
    throw err;
  }
}

export default {
  siteConfigGet,
  siteConfigSet,
  siteConfigWaitProp,
  sitesGet,
  siteDevicesGet,
  siteRingsGet,
  siteClientsRingSet,
  siteUsersGet,
  siteUsersPost,
  siteUsersDelete,
  siteEnrollGuest,
  siteSupreme,
  authProviders,
  authApplianceLogin,
  authApplianceLogout,
  authUserid,
  accountGeneratePassword,
  accountSelfProvision,
  setMockMode,
};
