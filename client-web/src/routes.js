/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

import AccountPrefs from './pages/account_prefs.vue';
import ComplianceReport from './pages/compliance_report.vue';
import DevDetails from './pages/dev_details.vue';
import Devices from './pages/devices.vue';
import EnrollGuest from './pages/enroll_guest.vue';
import Home from './pages/home.vue';
import LeftPanel from './pages/left_panel.vue';
import MalwareWarn from './pages/malware_warn.vue';
import Network from './pages/network.vue';
import NetworkVAP from './pages/network_vap.vue';
import SelfProvision from './pages/self_provision.vue';
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
    path: '/left_panel/',
    component: LeftPanel,
  },
  {
    path: '/account_prefs/',
    component: AccountPrefs,
  },
  {
    path: '/account_prefs/self_provision',
    component: SelfProvision,
  },
  {
    path: '/sites/:siteID/compliance_report/',
    component: ComplianceReport,
  },
  {
    path: '/sites/:siteID/devices/',
    component: Devices,
  },
  {
    path: '/sites/:siteID/devices/:UniqID/',
    component: DevDetails,
  },
  {
    path: '/sites/:siteID/enroll_guest/',
    component: EnrollGuest,
  },
  {
    path: '/sites/:siteID/',
    component: SiteAdmin,
  },
  {
    path: '/sites/:siteID/users/',
    component: Users,
  },
  {
    path: '/sites/:siteID/users/:UUID/',
    component: UserDetails,
  },
  {
    path: '/sites/:siteID/users/:UUID/editor/',
    component: UserEditor,
  },
  {
    path: '/sites/:siteID/network/',
    component: Network,
  },
  {
    path: '/sites/:siteID/network/vap/:vapName',
    component: NetworkVAP,
  },
  {
    path: '/test_tools/',
    component: TestTools,
  },
];
