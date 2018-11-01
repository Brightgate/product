/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

import LeftPanel from './pages/left_panel.vue';
import SiteStatus from './pages/site_status.vue';
import ComplianceReport from './pages/compliance_report.vue';
import DevDetails from './pages/dev_details.vue';
import Devices from './pages/devices.vue';
import EnrollGuest from './pages/enroll_guest.vue';
import Home from './pages/home.vue';
import MalwareWarn from './pages/malware_warn.vue';
import SiteAdmin from './pages/site_admin.vue';
import TestTools from './pages/test_tools.vue';
import UserDetails from './pages/user_details.vue';
import UserEditor from './pages/user_editor.vue';
import Users from './pages/users.vue';

export default [
  {
    path: '/',
    component: Home,
  },
  {
    path: '/malware_warn/',
    component: MalwareWarn,
  },
  {
    path: '/left-panel/',
    component: LeftPanel,
  },
  {
    path: '/sites/:SiteID/compliance_report/',
    component: ComplianceReport,
  },
  {
    path: '/sites/:SiteID/devices/',
    component: Devices,
  },
  {
    path: '/sites/:SiteID/devices/:UniqID/',
    component: DevDetails,
  },
  {
    path: '/sites/:SiteID/enroll_guest/',
    component: EnrollGuest,
  },
  {
    path: '/sites/:SiteID/site_status/',
    component: SiteStatus,
  },
  {
    path: '/sites/:SiteID/',
    component: SiteAdmin,
  },
  {
    path: '/sites/:SiteID/users/',
    component: Users,
  },
  {
    path: '/sites/:SiteID/users/:UUID/',
    component: UserDetails,
  },
  {
    path: '/sites/:SiteID/users/:UUID/editor/',
    component: UserEditor,
  },
  {
    path: '/test_tools/',
    component: TestTools,
  },
];
