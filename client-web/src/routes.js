/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


import Accounts from './pages/accounts.vue';
import AccountDetails from './pages/account_details.vue';
import AccountPrefs from './pages/account_prefs.vue';
import AccountRoles from './pages/account_roles.vue';
import AccountWG from './pages/account_wg.vue';
import AccountWGConfig from './pages/account_wg_config.vue';
import ComplianceReport from './pages/compliance_report.vue';
import DevDetails from './pages/dev_details.vue';
import Devices from './pages/devices.vue';
import EnrollGuest from './pages/enroll_guest.vue';
import Nodes from './pages/nodes.vue';
import NodeDetails from './pages/node_details.vue';
import NodeLanPort from './pages/node_lan_port.vue';
import NodeRadio from './pages/node_radio.vue';
import Help from './pages/help.vue';
import Home from './pages/home.vue';
import LeftPanel from './pages/left_panel.vue';
import MalwareWarn from './pages/malware_warn.vue';
import Network from './pages/network.vue';
import NetworkVAP from './pages/network_vap.vue';
import NetworkVAPEditor from './pages/network_vap_editor.vue';
import NetworkWG from './pages/network_wg.vue';
import NetworkWGEditor from './pages/network_wg_editor.vue';
import WifiProvision from './pages/wifi_provision.vue';
import SiteAdmin from './pages/site_admin.vue';
import SiteAlert from './pages/site_alert.vue';
import Support from './pages/support.vue';
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
    path: '/account_prefs/wifi_provision/',
    component: WifiProvision,
  },
  {
    path: '/account_prefs/wg/',
    component: AccountWG,
  },
  {
    path: '/account_prefs/wg/:id/',
    component: AccountWGConfig,
  },
  {
    path: '/accounts/',
    component: Accounts,
  },
  {
    path: '/accounts/:accountID/',
    component: AccountDetails,
  },
  {
    path: '/accounts/:accountID/roles/',
    component: AccountRoles,
  },
  {
    path: '/sites/:siteID/alerts/:alertID/',
    component: SiteAlert,
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
    path: '/sites/:siteID/nodes/',
    component: Nodes,
  },
  {
    path: '/sites/:siteID/nodes/:nodeID/',
    component: NodeDetails,
  },
  {
    path: '/sites/:siteID/nodes/:nodeID/lanports/:portID/',
    component: NodeLanPort,
  },
  {
    path: '/sites/:siteID/nodes/:nodeID/radios/:portID/',
    component: NodeRadio,
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
    path: '/sites/:siteID/network/vap/:vapName/editor',
    component: NetworkVAPEditor,
  },
  {
    path: '/sites/:siteID/network/wg',
    component: NetworkWG,
  },
  {
    path: '/sites/:siteID/network/wg/editor',
    component: NetworkWGEditor,
  },
  {
    path: '/support/',
    component: Support,
  },
  {
    path: '/test_tools/',
    component: TestTools,
  },
  {
    path: '/help/:helpTopic',
    component: Help,
  },
  {
    path: '/help/:helpTopic/:anchor',
    component: Help,
  },
];

