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

import mockDevices from './devices_mock';
import mockUsers from './users_mock';

const debug = Debug('site-mock');

const mockLocalSites = [
  {
    uuid: '0',
    name: 'Local Site',
  },
];

const mockCloudSites = [
  {
    uuid: '5182ab0b-39db-4256-86e0-8154171b35ac',
    name: 'Apple-Mock-Site',
  },
  {
    uuid: 'ef9b1046-95fa-41c5-a226-ad88198da9e2',
    name: 'Banana-Mock-Site',
  },
];


const mockRings = {
  'core': {
    'auth': 'wpa-eap',
    'subnet': '192.168.133.0/26',
    'leaseDuration': 1440,
  },
  'devices': {
    'auth': 'wpa-psk',
    'subnet': '192.168.135.0/26',
    'leaseDuration': 180,
  },
  'guest': {
    'auth': 'wpa-eap',
    'subnet': '192.168.136.0/26',
    'leaseDuration': 30,
  },
  'standard': {
    'auth': 'wpa-psk',
    'subnet': '192.168.134.0/26',
    'leaseDuration': 1440,
  },
};

const mockConfig = {
  '@/network/ssid': 'mock-ssid',
  '@/network/dnsserver': '1.2.3.4',
  '@/network/default_ring/wpa-eap': 'standard',
  '@/network/default_ring/wpa-psk': 'guest',
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

function mockAxios(normalAxios, mode) {
  const mockAx = axios.create();
  const mock = new MockAdapter(mockAx);

  const mockSites = (mode === 'cloud') ? mockCloudSites : mockLocalSites;
  debug('mockSites is!', mockSites, mode);

  mock
    .onGet('/api/sites').reply(200, mockSites)
    .onGet(/\/api\/sites\/.+\/users/).reply(200, mockUsers)
    .onGet(/\/api\/sites\/.+\/config\?.*/).reply(configHandler)
    .onGet(/\/api\/sites\/.+\/devices/).reply(200, mockDevices)
    .onGet(/\/api\/sites\/.+\/rings/).reply(200, mockRings)
    .onGet('/auth/sites/login').reply(200)
    .onGet('/auth/logout').reply(200)
    .onGet('/auth/userid').reply(200)
    .onAny().reply(500);

  return mockAx;
}

export default mockAxios;
