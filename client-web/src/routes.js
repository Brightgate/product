/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

import DevDetails from './pages/dev_details.vue';
import Devices from './pages/devices.vue';
import EnrollGuest from './pages/enroll_guest.vue';
import Home from './pages/home.vue';
import MalwareWarn from './pages/malware_warn.vue';
import UserDetails from './pages/user_details.vue';
import UserEditor from './pages/user_editor.vue';
import Users from './pages/users.vue';

export default [
  {
    path: '/',
    component: Home,
  },
  {
    path: '/devices/',
    component: Devices,
  },
  {
    path: '/devices/:UniqID/',
    component: DevDetails,
  },
  {
    path: '/enroll_guest/',
    component: EnrollGuest,
  },
  {
    path: '/malware_warn/',
    component: MalwareWarn,
  },
  {
    path: '/users/',
    component: Users,
  },
  {
    path: '/users/:UUID/',
    component: UserDetails,
  },
  {
    path: '/users/:UUID/editor/',
    component: UserEditor,
  },
];
