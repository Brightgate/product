/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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

import Promise from 'bluebird';
import retry from 'bluebird-retry';
import Debug from 'debug';
import appDefs from '../app_defs';
import makeAxiosMock from './site_mock';

class UnfinishedOperationError extends Error {
  constructor(message) {
    super(message);
    this.name = 'UnfinishedOperationError';
  }
}

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

async function commonAppliancePost(siteID, suffix, postData) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof suffix, 'string');
  assert.equal(typeof postData, 'object');

  // XXX See also T470 and site.go; this value is intentionally long because
  // the server doesn't handle it properly.
  const timeout = 20000;
  const u = buildUrl(`/api/sites/${siteID}/${suffix}`);
  debug(`POST ${u}`, postData);
  const res = await axios({
    method: 'POST',
    url: u,
    timeout: timeout,
    headers: {'X-Timeout': `${timeout}`},
    data: postData,
  });
  debug(`POST ${u} result`, res);
  // HTTP 202 accepted means "The request has been accepted for processing, but
  // the processing has not been completed.  The request might or might not
  // eventually be acted upon, as it might be disallowed when processing
  // actually takes place."
  if (res.status === 202) {
    throw new UnfinishedOperationError(u);
  }
  return res;
}

// Load the list of devices from the server.
async function siteDevicesGet(siteID) {
  const res = await commonApplianceGet(siteID, 'devices');
  assert(Array.isArray(res));
  return res;
}

async function siteHealthGet(siteID) {
  return await commonApplianceGet(siteID, 'health');
}

async function siteFeaturesGet(siteID) {
  return await commonApplianceGet(siteID, 'features');
}

// Load the list of rings from the server.
async function siteRingsGet(siteID) {
  return await commonApplianceGet(siteID, 'rings');
}

// Ask the server to change the ring property for a device, then
// attempt to wait for that change to propagate.  In practice this
// seems to take several seconds, during which time the server may
// become unreachable; thus we use retrys to make things work properly.
// XXX NEED TO WORK ON THIS MORE-- also, still true?
async function siteClientsRingSet(siteID, deviceID, newRing) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof deviceID, 'string');
  assert.equal(typeof newRing, 'string');

  await commonAppliancePost(siteID, `devices/${deviceID}`, {ring: newRing});

  // XXX needs some more looking at.
  const propName = `@/clients/${deviceID}/ring`;
  debug(`siteClientsRingSet: ${propName} -> ${newRing}`);
  await siteConfigWaitProp(siteID, propName, newRing);
}

async function siteClientsFriendlySet(siteID, deviceID, newFriendly) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof deviceID, 'string');
  assert.equal(typeof newFriendly, 'string');

  await commonAppliancePost(siteID, `devices/${deviceID}`, {friendlyName: newFriendly});
}

// Load the DNS config from the server.
async function siteDNSConfigGet(siteID) {
  return await commonApplianceGet(siteID, 'network/dns');
}

// Load the list of VAPs from the server.
async function siteVAPsGet(siteID) {
  const vapNames = await commonApplianceGet(siteID, 'network/vap');
  debug('vapNames', vapNames);
  const vapMap = {};
  for (const n of vapNames) {
    vapMap[n] = commonApplianceGet(siteID, `network/vap/${n}`);
  }
  const res = await Promise.props(vapMap);
  debug('vap result is', res);
  return res;
}

// Post configuration changes for a vap
async function siteVAPPost(siteID, vapName, vapConfig) {
  assert.equal(typeof siteID, 'string', 'siteid');
  assert.equal(typeof vapName, 'string', 'vapname');
  assert.equal(typeof vapConfig, 'object', 'config');

  await commonAppliancePost(siteID, `network/vap/${vapName}`, vapConfig);
}

// Load the WAN information from the server.
async function siteWanGet(siteID) {
  return await commonApplianceGet(siteID, 'network/wan');
}

// Load the list of users from the server.
async function siteUsersGet(siteID) {
  const res = await commonApplianceGet(siteID, 'users');
  assert(typeof res === 'object');
  return res;
}

// Load the map of nodes from the server.
async function siteNodesGet(siteID) {
  return await commonApplianceGet(siteID, 'nodes');
}

// Post changes to a node
async function siteNodePost(siteID, nodeID, nodeConfig) {
  assert.equal(typeof siteID, 'string', 'siteID');
  assert.equal(typeof nodeID, 'string', 'nodeID');
  assert.equal(typeof nodeConfig, 'object', 'nodeConfig');

  await commonAppliancePost(siteID, `nodes/${nodeID}`, nodeConfig);
}

// Post changes to a node's port
async function siteNodePortPost(siteID, nodeID, portID, portConfig) {
  assert.equal(typeof siteID, 'string', 'siteID');
  assert.equal(typeof nodeID, 'string', 'nodeID');
  assert.equal(typeof portID, 'string', 'portID');
  assert.equal(typeof portConfig, 'object', 'portConfig');

  await commonAppliancePost(siteID, `nodes/${nodeID}/ports/${portID}`, portConfig);
}

// Update or create user on server
async function siteUsersPost(siteID, userInfo, newUser) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof userInfo, 'object');
  assert.equal(typeof newUser, 'boolean');

  const uid = newUser ? 'NEW' : userInfo.UUID;
  const res = await commonAppliancePost(siteID, `users/${uid}`, userInfo);
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

async function siteEnrollGuest(siteID, {kind, phoneNumber, email}) {
  assert.equal(typeof siteID, 'string');
  assert.equal(typeof kind, 'string');
  assert.equal(typeof phoneNumber, 'string');
  assert(email === undefined || typeof email === 'string');

  const args = {kind, phoneNumber, email};
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

// AuthUserIDError is designed to absorb the special error returned by the
// /auth/userid;
class AuthUserIDError {
  constructor(lerr) {
    this.reason = appDefs.LOGIN_REASON.UNKNOWN_ERROR;
    if (!lerr.response || !lerr.response.data) {
      debug('Saw unexpected error constructing AuthUserIDError', lerr);
      return;
    }
    if (typeof lerr.response.data === 'object') {
      if (lerr.response.data.reason) {
        // Should override reason, and set other related fields
        Object.assign(this, lerr.response.data);
        return;
      } else {
        debug('Saw unexpected response object constructing AuthUserIDError', lerr.response.data);
        return;
      }
    }
  }
}

async function authProviders() {
  const u = buildUrl('/auth/providers');
  try {
    const resp = await axios.get(u);
    return resp.data;
  } catch (err) {
    debug('authProviders: failed', err);
    // UI can indicate that is not working
    return {
      mode: appDefs.APPMODE_FAILURE,
      error: err,
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

async function authApplianceLogout() {
  const u = buildUrl('/auth/logout');
  try {
    await axios.get(u);
  } catch (err) {
    debug('authApplianceLogout: failed', err);
    throw err;
  }
}

async function authUserID() {
  const u = buildUrl('/auth/userid');
  try {
    const res = await axios.get(u);
    return res.data;
  } catch (err) {
    debug('authUserID: failed', err);
    debug('authUserID: failed', typeof(err.response.data), err.response.data);
    throw new AuthUserIDError(err);
  }
}

async function accountDeprovisionPost(accountUUID) {
  assert.equal(typeof accountUUID, 'string');
  const u = buildUrl(`/api/account/${accountUUID}/deprovision`);
  try {
    const res = await axios({
      timeout: 20000,
      method: 'POST',
      headers: {'content-type': 'application/x-www-form-urlencoded'},
      url: u,
    });
    debug('accountDeprovisionPost: succeeded');
    return res.data;
  } catch (err) {
    debug('accountDeprovisionPost: failed', err);
    throw err;
  }
}

async function accountGeneratePassword() {
  const u = buildUrl('/api/account/passwordgen');
  try {
    const res = await axios.get(u);
    return res.data;
  } catch (err) {
    debug('accountGeneratePassword: failed', err);
    throw err;
  }
}

async function accountSelfProvisionGet(accountUUID) {
  assert.equal(typeof accountUUID, 'string');
  const u = buildUrl(`/api/account/${accountUUID}/selfprovision`);
  try {
    const res = await axios.get(u);
    return res.data;
  } catch (err) {
    debug('accountSelfProvisionGet: failed', err);
    throw err;
  }
}

async function accountSelfProvisionPost(accountUUID, username, password, verifier) {
  const u = buildUrl(`/api/account/${accountUUID}/selfprovision`);
  try {
    const res = await axios({
      timeout: 20000,
      method: 'POST',
      headers: {'content-type': 'application/x-www-form-urlencoded'},
      data: qs.stringify({username, password, verifier}),
      url: u,
    });
    debug('accountSelfProvisionPost: succeeded');
    return res.data;
  } catch (err) {
    debug('accountSelfProvisionPost: failed', err);
    throw err;
  }
}

async function accountRolesGet(accountUUID) {
  assert.equal(typeof accountUUID, 'string');
  const u = buildUrl(`/api/account/${accountUUID}/roles`);
  try {
    const res = await axios.get(u);
    return res.data;
  } catch (err) {
    debug('accountRolesGet: failed', err);
    throw err;
  }
}

async function accountRolesPost(accountUUID, tgtOrgUUID, role, value) {
  assert.equal(typeof accountUUID, 'string');
  const u = buildUrl(`/api/account/${accountUUID}/roles/${tgtOrgUUID}/${role}`);
  try {
    const res = await axios({
      method: 'POST',
      headers: {'content-type': 'application/x-www-form-urlencoded'},
      data: qs.stringify({value: value}),
      url: u,
    });
    return res.data;
  } catch (err) {
    debug('accountRolesPost: failed', err);
    throw err;
  }
}

async function accountDelete(accountUUID) {
  assert.equal(typeof accountUUID, 'string');
  const u = buildUrl(`/api/account/${accountUUID}`);
  try {
    await axios({
      timeout: 20000,
      method: 'DELETE',
      url: u,
    });
    debug('accountDelete: succeeded');
  } catch (err) {
    debug('accountDelete: failed', err);
    throw err;
  }
}

async function orgsGet() {
  const u = buildUrl(`/api/org`);
  try {
    const res = await axios.get(u);
    return res.data;
  } catch (err) {
    debug('orgsGet: failed', err);
    throw err;
  }
}

async function orgAccountsGet(orgUUID) {
  const u = buildUrl(`/api/org/${orgUUID}/accounts`);
  try {
    const res = await axios.get(u);
    return res.data;
  } catch (err) {
    debug('orgAccountsGet: failed', err);
    throw err;
  }
}

export default {
  siteConfigGet,
  siteConfigSet,
  siteConfigWaitProp,
  sitesGet,
  siteDevicesGet,
  siteHealthGet,
  siteFeaturesGet,
  siteRingsGet,
  siteClientsRingSet,
  siteClientsFriendlySet,
  siteDNSConfigGet,
  siteVAPsGet,
  siteVAPPost,
  siteWanGet,
  siteNodesGet,
  siteNodePost,
  siteNodePortPost,
  siteUsersGet,
  siteUsersPost,
  siteUsersDelete,
  siteEnrollGuest,
  AuthUserIDError,
  authProviders,
  authApplianceLogin,
  authApplianceLogout,
  authUserID,
  accountDelete,
  accountDeprovisionPost,
  accountGeneratePassword,
  accountSelfProvisionGet,
  accountSelfProvisionPost,
  accountRolesGet,
  accountRolesPost,
  orgsGet,
  orgAccountsGet,
  setMockMode,
  UnfinishedOperationError,
};
