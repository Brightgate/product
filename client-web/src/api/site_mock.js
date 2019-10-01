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
import {mockUsers, mockAccounts, mockUserID} from './users_mock';

const debug = Debug('site-mock');

// mock response to /api/sites when in APPMODE_LOCAL
const mockLocalSites = [
  {
    UUID: '0',
    name: 'Local Site',
    organizationUUID: '0',
  },
];

// mock response to /api/sites when in APPMODE_CLOUD
const mockCloudSites = [
  {
    UUID: '5182ab0b-39db-4256-86e0-8154171b35ac',
    name: 'Scranton Office',
    organizationUUID: 'd91864cd-434a-4b52-8236-d3b95afde170',
  },
  {
    UUID: 'ef9b1046-95fa-41c5-a226-ad88198da9e2',
    name: 'Buffalo Office',
    organizationUUID: 'd91864cd-434a-4b52-8236-d3b95afde170',
  },
  {
    UUID: '2ffcf157-d083-469c-a607-9af4bd9ec4d5',
    name: 'WUPHF HQ',
    organizationUUID: '9f56108e-2916-409d-9b43-c964115fde61',
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

const mockNodes = [
  {
    'id': '001-201901BB-000001',
    'name': 'gateway',
    'role': 'gateway',
    'serialNumber': '001-201901BB-000001',
    'hwModel': 'model100',
    'bootTime': '2019-05-02T04:19:57Z',
    'alive': '2019-09-30T20:56:02Z',
    'addr': '10.1.3.49',
    'nics': [
      {
        'name': 'wan',
        'macaddr': '60:90:84:a0:00:02',
        'kind': 'wired:uplink',
        'ring': 'wan',
        'silkscreen': 'wan',
      },
      {
        'name': 'lan3',
        'macaddr': '60:90:84:a0:00:03',
        'kind': 'wired:lan',
        'ring': 'internal',
        'silkscreen': '4',
      },
      {
        'name': 'lan1',
        'macaddr': '60:90:84:a0:00:03',
        'kind': 'wired:lan',
        'ring': 'standard',
        'silkscreen': '2',
      },
      {
        'name': 'lan2',
        'macaddr': '60:90:84:a0:00:03',
        'kind': 'wired:lan',
        'ring': 'standard',
        'silkscreen': '3',
      },
      {
        'name': 'wlan0',
        'macaddr': '06:f0:21:4b:77:3c',
        'kind': 'wireless',
        'ring': '',
        'silkscreen': '1',
      },
      {
        'name': 'wlan1',
        'macaddr': '06:f0:21:4b:77:4c',
        'kind': 'wireless',
        'ring': '',
        'silkscreen': '2',
      },
      {
        'name': 'lan0',
        'macaddr': '60:90:84:a0:00:03',
        'kind': 'wired:lan',
        'ring': 'devices',
        'silkscreen': '1',
      },
    ],
  },
  {
    'id': '001-201901BB-000002',
    'name': 'conference room',
    'role': 'satellite',
    'serialNumber': '001-201901BB-000002',
    'hwModel': 'model100',
    'bootTime': '2019-05-02T04:19:57Z',
    'alive': '2019-09-30T20:56:02Z',
    'addr': '10.1.3.49',
    'nics': [
      {
        'name': 'wan',
        'macaddr': '60:90:84:a0:00:02',
        'kind': 'wired:wan',
        'ring': 'wan',
        'silkscreen': 'wan',
      },
      {
        'name': 'lan3',
        'macaddr': '60:90:84:a0:00:03',
        'kind': 'wired:lan',
        'ring': 'internal',
        'silkscreen': '4',
      },
      {
        'name': 'lan1',
        'macaddr': '60:90:84:a0:00:03',
        'kind': 'wired:lan',
        'ring': 'standard',
        'silkscreen': '2',
      },
      {
        'name': 'lan2',
        'macaddr': '60:90:84:a0:00:03',
        'kind': 'wired:lan',
        'ring': 'standard',
        'silkscreen': '3',
      },
      {
        'name': 'wlan0',
        'macaddr': '06:f0:21:4b:77:3c',
        'kind': 'wireless',
        'ring': '',
        'silkscreen': '1',
      },
      {
        'name': 'wlan1',
        'macaddr': '06:f0:21:4b:77:4c',
        'kind': 'wireless',
        'ring': '',
        'silkscreen': '2',
      },
      {
        'name': 'lan0',
        'macaddr': '60:90:84:a0:00:03',
        'kind': 'wired:lan',
        'ring': 'devices',
        'silkscreen': '1',
      },
    ],
  },
];

const mockVAPInfo = {
  'psk': {
    'ssid': 'dunder-device',
    'keyMgmt': 'wpa-psk',
    'passphrase': 'I LIKE COCONUTS',
    'defaultRing': 'unenrolled',
    'rings': ['quarantine', 'devices', 'unenrolled'],
  },
  'guest': {
    'ssid': 'dunder-guest',
    'keyMgmt': 'wpa-psk',
    'passphrase': 'I LIKE COFFEE',
    'defaultRing': 'guest',
    'rings': ['guest'],
  },
  'eap': {
    'ssid': 'dunder-user',
    'keyMgmt': 'wpa-eap',
    'defaultRing': 'standard',
    'rings': ['standard', 'core'],
  },
};
const mockVAPNames = Object.keys(mockVAPInfo);

const mockWan = {
  'currentAddress': '10.1.4.38/26',
  'dhcpAddress': '10.1.4.38/26',
  'dhcpStart': '2019-03-14T21:05:35Z',
  'dhcpDuration': 86400,
  'dhcpRoute': '10.1.4.1',
};

const mockHealth = {
  'heartbeatProblem': true,
  'configProblem': true,
};

const mockConfig = {
  '@/network/base_address': '10.1.4.12/26',
};

const mockDNSConfig = {
  'domain': '99999.brightgate.net',
  'servers': ['8.8.8.8'],
};

const mockLocalOrgs = [
  {
    name: 'Local Org',
    organizationUUID: '0',
    relationship: 'self',
  },
];

const mockCloudOrgs = [
  {
    name: 'WUPHF.com, A Ryan Howard Project',
    organizationUUID: '9f56108e-2916-409d-9b43-c964115fde61',
    relationship: 'msp',
  },
  {
    name: 'Dunder Mifflin',
    organizationUUID: 'd91864cd-434a-4b52-8236-d3b95afde170',
    relationship: 'self',
  },
];

export const mockAccountRoles = [
  {
    'targetOrganization': '9f56108e-2916-409d-9b43-c964115fde61',
    'relationship': 'msp',
    'limitRoles': ['admin', 'user'],
    'roles': ['admin'],
  },
  {
    'targetOrganization': 'd91864cd-434a-4b52-8236-d3b95afde170',
    'relationship': 'self',
    'limitRoles': ['admin', 'user'],
    'roles': ['admin', 'user'],
  },
];

const mockEnrollGuest = {
  smsDelivered: true,
  smsErrorCode: 0,
  smsError: '',
};

function configGetHandler(config) {
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

async function configPostHandler(config) {
  await timeout(3000);
  return [200];
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
    username: 'pam@dundermifflin.com',
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
      username: 'pam@dundermifflin.com',
      completed: (new Date()).toISOString(),
    };
  }
  return [200, resp];
}

async function selfProvPostHandler() {
  await timeout(3000);
  return [200];
}

function getAccountsByOrgID(orgID) {
  return mockAccounts[orgID];
}

async function accountDelHandler(config) {
  debug('accountDel', config);
  const parsedURL = new URL(config.url, 'http://example.com/');
  debug('parsedURL', parsedURL);
  const p = parsedURL.pathname.split('/');
  const last = p[p.length - 1];
  const orgID = p[p.length - 3];
  const accts = getAccountsByOrgID(orgID);
  const idx = accts.findIndex((elem) => elem.accountUUID === last);
  if (idx >= 0) {
    accts.splice(idx, 1);
  }
  debug('mockAccounts is now', mockAccounts);
  return [200];
}

function mockAxios(normalAxios, mode) {
  const mockAx = axios.create();
  const mock = new MockAdapter(mockAx);
  debug('mock mode', mode);

  const mockOrgs = (mode === appDefs.APPMODE_CLOUD) ? mockCloudOrgs : mockLocalOrgs;
  debug('mockOrgs', mockOrgs);
  const mockSites = (mode === appDefs.APPMODE_CLOUD) ? mockCloudSites : mockLocalSites;
  debug('mockSites', mockSites);
  const mockProviders = (mode === appDefs.APPMODE_CLOUD) ? mockCloudProviders : mockLocalProviders;
  debug('mockProviders', mockProviders);

  mock
    .onGet('/api/sites').reply(200, mockSites)
    .onGet(/\/api\/sites\/.+\/users/).reply(200, mockUsers)
    .onGet(/\/api\/sites\/.+\/health/).reply(200, mockHealth)
    .onGet(/\/api\/sites\/.+\/config\?.*/).reply(configGetHandler)
    .onPost(/\/api\/sites\/.+\/config/).reply(configPostHandler)
    .onGet(/\/api\/sites\/.+\/devices/).reply(200, mockDevices)
    .onGet(/\/api\/sites\/.+\/rings/).reply(200, mockRings)
    .onGet(/\/api\/sites\/.+\/nodes/).reply(200, mockNodes)
    .onPost(/\/api\/sites\/.+\/enroll_guest$/).reply(200, mockEnrollGuest)
    .onGet(/\/api\/sites\/.+\/network\/dns$/).reply(200, mockDNSConfig)
    .onGet(/\/api\/sites\/.+\/network\/vap$/).reply(200, mockVAPNames)
    .onGet(/\/api\/sites\/.+\/network\/vap\.*/).reply(vapGetHandler)
    .onGet(/\/api\/sites\/.+\/network\/wan$/).reply(200, mockWan)
    .onGet('/auth/sites/login').reply(200)
    .onGet('/auth/logout').reply(200)
    .onGet('/auth/userid').reply(200, mockUserID)
    .onGet('/auth/providers').reply(200, mockProviders)
    .onDelete(/\/api\/account\/.+/).reply(accountDelHandler)
    .onGet(/\/api\/account\/passwordgen/).reply(passwordgenHandler)
    .onGet(/\/api\/account\/.+\/selfprovision/).reply(selfProvGetHandler)
    .onPost(/\/api\/account\/.+\/selfprovision/).reply(selfProvPostHandler)
    .onPost(/\/api\/account\/.+\/deprovision/).reply(200)
    .onGet(/\/api\/account\/.+\/roles/).reply(200, mockAccountRoles)
    .onPost(/\/api\/account\/.+\/roles\/.+\/.+/).reply(200)
    .onGet('/api/org').reply(200, mockOrgs)
    .onGet(/\/api\/org\/9f56108e-2916-409d-9b43-c964115fde61\/accounts/).reply(200, getAccountsByOrgID('9f56108e-2916-409d-9b43-c964115fde61'))
    .onGet(/\/api\/org\/d91864cd-434a-4b52-8236-d3b95afde170\/accounts/).reply(200, getAccountsByOrgID('d91864cd-434a-4b52-8236-d3b95afde170'))
    .onAny().reply(500);

  return mockAx;
}

export default mockAxios;
