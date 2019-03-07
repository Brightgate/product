/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

import MockAdapter from 'axios-mock-adapter';
import axios from 'axios';
import Debug from 'debug';

import appDefs from '../app_defs';
import mockDevices from './devices_mock';
import mockUsers from './users_mock';

const debug = Debug('site-mock');

// mock response to /api/sites when in APPMODE_LOCAL
const mockLocalSites = [
  {
    uuid: '0',
    name: 'Local Site',
  },
];

// mock response to /api/sites when in APPMODE_CLOUD
const mockCloudSites = [
  {
    uuid: '5182ab0b-39db-4256-86e0-8154171b35ac',
    name: 'Scranton Office (Admin)',
    roles: ['admin'],
  },
  {
    uuid: 'ef9b1046-95fa-41c5-a226-ad88198da9e2',
    name: 'Buffalo Office (User)',
    roles: ['user'],
  },
];

// mock response to /auth/providers when in APPMODE_LOCAL
const mockLocalProviders = {
  mode: appDefs.APPMODE_LOCAL,
  providers: 'password',
};

// mock response to /auth/providers when in APPMODE_CLOUD
const mockCloudProviders = {
  mode: appDefs.APPMODE_CLOUD,
  providers: ['google', 'azureadv2'],
};

const mockRings = {
  'core': {
    'vap': 'eap',
    'subnet': '192.168.131.0/26',
    'leaseDuration': 1440,
  },
  'devices': {
    'vap': 'psk',
    'subnet': '192.168.132.0/26',
    'leaseDuration': 180,
  },
  'guest': {
    'vap': 'guest',
    'subnet': '192.168.133.0/26',
    'leaseDuration': 30,
  },
  'standard': {
    'vap': 'eap',
    'subnet': '192.168.134.0/26',
    'leaseDuration': 1440,
  },
  'quarantine': {
    'vap': 'psk',
    'subnet': '192.168.135.0/26',
    'leaseDuration': 10,
  },
  'unenrolled': {
    'vap': 'psk',
    'subnet': '192.168.136.0/26',
    'leaseDuration': 10,
  },
  'internal': {
    'vap': '',
    'subnet': '192.168.137.0/26',
    'leaseDuration': 1440,
  },
};

const mockVAPInfo = {
  'psk': {
    'ssid': 'coffeeshop-devices',
    'keyMgmt': 'wpa-psk',
    'passphrase': 'I LIKE COCONUTS',
    'defaultRing': 'unenrolled',
    'rings': ['quarantine', 'devices', 'unenrolled'],
  },
  'guest': {
    'ssid': 'coffeeshop-guest',
    'keyMgmt': 'wpa-psk',
    'passphrase': 'I LIKE COFFEE',
    'defaultRing': 'guest',
    'rings': ['guest'],
  },
  'eap': {
    'ssid': 'coffeeshop-users',
    'keyMgmt': 'wpa-eap',
    'defaultRing': 'standard',
    'rings': ['standard', 'core'],
  },
};
const mockVAPNames = Object.keys(mockVAPInfo);

const mockConfig = {
  '@/network/dnsserver': '8.8.8.8',
  '@/network/wan/current/address': '10.1.4.12/26',
  '@/network/2.4GHz/channel': '11',
  '@/network/5GHz/channel': '165',
};

const mockUserid = {
  'username': 'test@example.com',
  'email': 'test@example.com',
  'phoneNumber': '+1 650-555-1212',
  'name': 'Foo Bar',
  'organization': 'example corp',
  'selfProvisioned': true,
};

const mockEnrollGuest = {
  smsDelivered: true,
  smsErrorCode: 0,
  smsError: '',
};

function configHandler(config) {
  const parsedURL = new URL(config.url, 'http://example.com/');
  const search = parsedURL.search.slice(1, Infinity);
  const value = mockConfig[search];
  debug('configHandler:', search, value);

  if (value === undefined) {
    debug(`configHandler: prop ${search} not found`);
    return 500;
  }
  return [200, value];
}

function vapGetHandler(config) {
  const parsedURL = new URL(config.url, 'http://example.com/');
  const splits = parsedURL.pathname.split('/');
  const lastComp = splits[splits.length - 1];
  return [200, mockVAPInfo[lastComp]];
}

const pws = ['correct horse battery staple', 'i like coconuts', 'my voice is my password'];
let pwid = 0;

function passwordgenHandler() {
  const resp = {
    username: 'test@example.com',
    password: pws[pwid % pws.length],
    verifier: 'anything',
  };
  pwid++;
  return [200, resp];
}

function timeout(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function selfProvGetHandler() {
  let resp = {
    status: 'unprovisioned',
  };
  if (localStorage.getItem('debug_provisioned') === 'true') {
    resp = {
      status: 'provisioned',
      username: 'test@example.com',
      completed: '2019-02-01T01:01:01Z',
    };
  }
  return [200, resp];
}

async function selfProvPostHandler() {
  await timeout(3000);
  return [200];
}

function mockAxios(normalAxios, mode) {
  const mockAx = axios.create();
  const mock = new MockAdapter(mockAx);
  debug('mock mode', mode);

  const mockSites = (mode === appDefs.APPMODE_CLOUD) ? mockCloudSites : mockLocalSites;
  debug('mockSites', mockSites);
  const mockProviders = (mode === appDefs.APPMODE_CLOUD) ? mockCloudProviders : mockLocalProviders;
  debug('mockProviders', mockProviders);

  mock
    .onGet('/api/sites').reply(200, mockSites)
    .onGet(/\/api\/sites\/.+\/users/).reply(200, mockUsers)
    .onGet(/\/api\/sites\/.+\/config\?.*/).reply(configHandler)
    .onGet(/\/api\/sites\/.+\/devices/).reply(200, mockDevices)
    .onGet(/\/api\/sites\/.+\/rings/).reply(200, mockRings)
    .onPost(/\/api\/sites\/.+\/enroll_guest$/).reply(200, mockEnrollGuest)
    .onGet(/\/api\/sites\/.+\/network\/vap$/).reply(200, mockVAPNames)
    .onGet(/\/api\/sites\/.+\/network\/vap\.*/).reply(vapGetHandler)
    .onGet('/auth/sites/login').reply(200)
    .onGet('/auth/logout').reply(200)
    .onGet('/auth/userid').reply(200, mockUserid)
    .onGet('/auth/providers').reply(200, mockProviders)
    .onGet(/\/api\/account\/.+\/passwordgen/).reply(passwordgenHandler)
    .onGet(/\/api\/account\/.+\/selfprovision/).reply(selfProvGetHandler)
    .onPost(/\/api\/account\/.+\/selfprovision/).reply(selfProvPostHandler)
    .onAny().reply(500);

  return mockAx;
}

export default mockAxios;
