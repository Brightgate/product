/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */
// vim :set ts=2 sw=2 sts=2 et :
export const mockAccounts = [
  {
    'accountUUID': '4f58eafe-c946-4b8b-b4a1-f4482e9a9f6e',
    'email': 'pam@dundermifflin.com',
    'phoneNumber': '570-555-1212',
    'name': 'Pam Beesly',
    'primaryEmail': 'pam@dundermifflin.com',
  },
  {
    'accountUUID': '22d83012-c62b-4568-baf8-29269c617562',
    'email': 'jim@dundermifflin.com',
    'phoneNumber': '570-555-1212',
    'name': 'Jim Halpert',
    'primaryEmail': 'jim@dundermifflin.com',
  },
];

const pam = mockAccounts[0];
const jim = mockAccounts[1];

export const mockUsers = {
  '5880b539-e65f-4a0a-944c-38b9672aa638': {
    'UID': 'admin',
    'UUID': '5880b539-e65f-4a0a-944c-38b9672aa638',
    'Role': '',
    'DisplayName': 'Admin',
    'Email': 'admin@dundermifflin.com',
    'TelephoneNumber': '+1 650-555-1212',
    'HasPassword': true,
    'SetPassword': null,
    'SelfProvisioning': false,
  },
  'dd8d12dc-30b0-4e8e-a7a2-0e3cbc26034f': {
    'UID': 'michael',
    'UUID': 'dd8d12dc-30b0-4e8e-a7a2-0e3cbc26034f',
    'Role': '',
    'DisplayName': 'Michael Scott',
    'Email': 'michaelscott@dundermifflin.com',
    'TelephoneNumber': '+1 650-555-1212',
    'HasPassword': true,
    'SetPassword': null,
    'SelfProvisioning': false,
  },
  [pam.accountUUID]: {
    'UID': pam.email,
    'UUID': pam.accountUUID,
    'Role': '',
    'DisplayName': pam.name,
    'Email': pam.email,
    'TelephoneNumber': pam.phoneNumber,
    'HasPassword': true,
    'SetPassword': null,
    'SelfProvisioning': true,
  },
  [jim.accountUUID]: {
    'UID': jim.email,
    'UUID': jim.accountUUID,
    'Role': '',
    'DisplayName': jim.name,
    'Email': jim.email,
    'TelephoneNumber': jim.phoneNumber,
    'HasPassword': true,
    'SetPassword': null,
    'SelfProvisioning': true,
  },
};

export const mockUserID = {
  'username': pam.email,
  'email': pam.emal,
  'phoneNumber': pam.phoneNumber,
  'name': pam.name,
  'organization': 'Dunder Mifflin, Inc.',
  'accountUUID': pam.accountUUID,
  'selfProvisioned': true,
};
