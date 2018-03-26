/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

import Devices from './pages/devices.vue';
import Details from './pages/details.vue';
import Enroll from './pages/enroll.vue';
import Home from './pages/home.vue';
import MalwareWarn from './pages/malwareWarn.vue';
import Users from './pages/users.vue';
import UserDetails from './pages/user_details.vue';
import UserEditor from './pages/user_editor.vue';

export default [
  {
    path: '/',
    component: Home,
  },
  {
    path: '/devices',
    component: Devices,
  },
  {
    path: '/details/',
    component: Details,
  },
  {
    path: '/enroll/',
    component: Enroll,
  },
  {
    path: '/malwareWarn/',
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
