/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

/* eslint camelcase: ['error', {'properties': 'never'}] */

// Ready translated locale messages
export default {
  en: {
    message: {
      test_tools: { // Note: we don't internationalize this group
        testing: 'For Test Purposes',
        mocks_group: 'Mock behaviors',
        appmode_group: 'App Major Mode',
        other_group: 'Other Tools',
        accept_devices: 'Accept Devices',
        accept_success: 'Devices Acceptance succeeded. {devicesChanged} devices were affected.',
        accept_fail: 'There was an error accepting devices: <b>{reason}.</b>',
        enable_mock: 'Mock API Responses',
        enable_fakelogin: 'Simulate being Logged In',
        switch_appliance: 'Switch Appliance',
        auto_mode: 'Automatic Mode',
        local_mode: 'Force Local Site Mode',
        cloud_mode: 'Force Cloud Mode',
        provision_group: 'Employee Self-Provisioning',
        generated_pass: 'Generated Password',
        generate_pass_button: 'Generate Password',
        accept_pass_button: 'Accept and Provision',
      },
      api: { // Messages from the store
        unknown_device: 'Unnamed ({hwAddr})',
        unknown_device_with_genus: '{genus} ({hwAddr})',
        unknown_org: 'Unknown',
        roles: {
          user: 'User',
          admin: 'Administrator',
        },
        strength: {
          str_5: 'Excellent',
          str_4: 'Very Good',
          str_3: 'Good',
          str_2: 'Poor',
          str_1: 'Very Poor',
          str_0: 'Unknown',
        },
        unfinished_operation: 'This site isn\'t responding to cloud commands at the moment. This command has been queued for processing; when the site reconnects to the cloud, the change will occur.',
      },
      home: {
        local_site: 'Local Site',
        local_site_explanation: 'You are administering the Brightgate Wi-fi appliances on your local network (the Local Site). Changes will also be reflected in the Brightgate cloud.',
        select_site: 'Select Site',
        tools: 'Tools',
      },
      org_switch_popup: {
        no_roles: 'No roles granted',
      },
      left_panel: {
        group_organization: 'Organization',
        group_tools: 'Tools',
        group_help: 'Help',
        home: 'Home',
        my_account: 'My Account',
        accounts: 'Accounts',
        admin_guide: 'Admin Guide',
        support: 'Brightgate Support',
      },
      site_admin: {
        admin_title: 'You are administering Brightgate Wi-fi for this site.',
        user_title: 'You are viewing Brightgate Wi-fi for this site.',
      },
      site_list: {
        current: 'Current',
      },
      site_controls: {
        network: 'Networks',
        hardware: 'Hardware',
        compliance_report: 'Compliance Report',
        manage_devices: 'Devices ({active_device_count} active, {inactive_device_count} inactive)',
        enroll_guest: 'Enroll a Guest User',
        users: 'Users',
      },
      alerts: {
        serious_alerts: 'Serious Alerts',
        vulnerability: 'Vulnerability',
        problem_on_device: '{problem} on {device}',
      },
      site_alert: {
        title: 'Site Alert',
        contact_support: 'Contact Support',
        heartbeat: {
          'short': 'Gateway/Cloud Connectivity',
          'title': 'Gateway Can’t Connect to Brightgate Cloud',
          'text': 'Your gateway sends regular messages to the Brightgate cloud. We have not seen these messages in the past 15 minutes (or longer).',
          'check_intro': 'Please check:',
          'checks': {
            '1': 'The gateway is powered on and its lights indicate normal activity',
            '2': 'The gateway’s internet ethernet cable is connected',
            '3': 'Your cable modem or internet box is powered on and working',
            '4': 'Your internet provider does not have an outage',
          },
          'check_final': 'If these things are in order, your gateway may be experiencing a failure and may require service.',
        },
        configQueue: {
          'short': 'Gateway/Cloud Connectivity',
          'title': 'Brightgate cloud management stalled',
          'text': 'Brightgate’s cloud sent configuration commands to your gateway. These have been pending for three or more minutes. This means your gateway can’t connect or requires service.',
          'check_intro': 'Please check:',
          'checks': {
            '1': 'The gateway is powered on and its lights indicate normal activity',
            '2': 'The gateway’s internet ethernet cable is connected',
            '3': 'Your cable modem or internet box is powered on and working',
            '4': 'Your internet provider does not have an outage',
          },
          'check_final': 'If these things are in order, your gateway may be experiencing a failure and may require service.',
        },
      },
      notifications: {
        notifications: 'Notifications',
        update_device: '⚠️  Update device {device}',
        security_notifications: '⚠️ Security Notifications',
        self_provision_title: 'Your Account',
        self_provision_text: 'Your Wi-Fi login needs to be provisioned',
        msg: {
          '0': 'This device is less secure because it is running old software.',
          '1': 'Brightgate can\'t automatically update this device.',
          '2': 'Follow the manufacturer\'s instructions to update its software.',
        },
      },
      general: {
        accept: 'Accept',
        back: 'Back',
        cancel: 'Cancel',
        close: 'Close',
        confirm: 'Confirm',
        done: 'Done',
        details: 'Details',
        login: 'Login',
        logout: 'Logout',
        need_login: 'You must be logged in',
        save: 'Save',
        rings: {
          unenrolled: 'Unenrolled',
          quarantine: 'Quarantine',
          core: 'Core',
          standard: 'Standard',
          devices: 'Devices',
          guest: 'Guest',
          internal: 'Internal',
        },
      },
      account_prefs: {
        title: 'Account Preferences',
        wifi_provision: 'Wi-Fi Provisioning',
        vpn: 'VPN',
        roles_none: 'None',
      },
      account_wg: {
        title: 'VPN',
        configs: 'My VPN Configurations',
        download: 'Download VPN Software',
        download_explain: 'Brightgate uses the secure, open source VPN software called WireGuard®. You can learn more and download WireGuard for your system using the links below. Then load your VPN configuration(s) into WireGuard.',
        plat_other: 'Other Platforms',
        delete_button: 'Delete',
        delete_unfinished: 'The site {site} isn\'t responding to cloud commands at the moment. Your VPN configuration has been queued for deletion; when the site reconnects to the cloud, your VPN configuration will be deleted.',
      },
      account_wg_config: {
        title: 'New VPN Configuration',
        name: 'Configuration Name',
        name_placeholder: 'My Laptop',
        name_info: 'The device where you will install this config',
        name_error: 'Please describe the device where you will install this config',
        site: 'Site',
        site_info: 'The remote site you will connect to',
        create_button: 'Create',
        create_explain: 'Create a new VPN configuration, suitable for loading into a WireGuard® client. After the configuration is generated, you can download it, or scan it using the WireGuard application\'s QR code scanner.',
        success_explain: 'Here is your new VPN configuration. Download or Scan the QR code now; you won\'t be able to retrieve it later.',
        create_failed: 'Failed to create VPN configuration: {msg}',
        download_button: 'Download',
        qr_scan_button: 'Scan QR Code',
        qr_scan_explain: 'Use the WireGuard® application on your mobile device to scan this code. This will import the VPN configuration into your client. If the WireGuard application prompts you for "Tunnel Name", here are some suggested names:',
        enabled_optgroup: 'Sites with VPN Enabled',
        disabled_optgroup: 'Sites with VPN Disabled',
      },
      wifi_provision: {
        title: 'Wi-Fi Self Provisioning',
      },
      network: {
        title: 'Networks',
        names: {
          eap: 'Authorized User Network',
          psk: 'Devices Network',
          guest: 'Guest Network',
          vpn: 'VPN',
        },
        config: 'Configuration',
        config_dns_server: 'DNS Server',
        config_dns_domain: 'Site DNS Domain',
        config_wan_current: 'Current WAN Address',
        config_24GHz: '2.4GHz Channel',
        config_5GHz: '5GHz Channel',
        descriptions: {
          eap: 'Unique ID and password; laptops, tablets, phones',
          psk: 'Shared password; IoT devices, printers, ...',
          guest: 'Wi-Fi guest users; password rotates periodically',
          vpn: 'Remote access to this site using VPN software',
        },
        networks: 'Networks',
      },
      network_vap: {
        title: 'Network Details',
        descriptions: {
          eap: 'Each network user has their own login ID and password, created using the Brightgate App or Cloud Portal. Use it for laptops, tablets, and phones.',
          psk: 'All devices on this network share the same password. Use it for IoT devices, such as printers, security cameras, or "smart" appliances.',
          guest: 'Guests use this network. Administrators periodically rotate this password. To reveal the current password, click the password eye.',
        },
        properties: 'Network Properties',
        key_mgmt: 'Authentication Method',
        default_tg: '(default)',
        passphrase: 'Passphrase',
        ring_config: 'Trust Group Configuration',
      },
      network_vap_editor: {
        title: 'Edit Network Properties',
        ssid: 'SSID (Network Name)',
        passphrase: 'Passphrase',
        warning: 'If your device is presently using the "{ssid}" Wi-Fi network, changing these properties may cause you to become disconnected.',
        warning_title: 'Wi-Fi Properties Change',
        error_update: 'Error while changing network settings: {err}',
        titles: {
          eap: 'Authorized User Network Properties',
          psk: 'Devices Network Properties',
          guest: 'Guest Network Properties',
        },
        valid_ssid: {
          not_set: 'SSID must be set',
          too_long: 'SSID is too long {len}/32',
          invalid: 'Invalid character',
        },
        valid_pp: {
          len: 'Passphrase must be 8 - 63 characters',
          invalid: 'Invalid character',
          hex: '64-character passphrases must be hex strings',
        },
      },
      network_wg: {
        title: 'VPN',
        properties: 'VPN Properties',
        status: 'VPN Status',
        enabled: 'Enabled',
        disabled: 'Disabled',
        address: 'External Address',
        address_none: 'None configured',
        port: 'External Port',
        port_none: 'None',
        public_key: 'Public Key',
      },
      network_wg_editor: {
        title: 'VPN Configuration',
        properties: 'VPN Properties',
        enabled: 'Enable VPN',
        address: 'External Address',
        port: 'External Port',
        warning: 'Changing the VPN server address or port will invalidate existing user VPN configurations!',
        warning_title: 'VPN Server Property Change',
        error_update: 'Failed updating VPN configuration',
      },
      compliance_report: {
        title: 'Compliance Report',
        summary: 'Summary',
        summary_violations: 'There are no policy violations. | There is one policy violation. Correct it to stay compliant. | There are {num} policy violations. Correct these to stay compliant.',
        summary_no_violations: 'This network is currently in compliance with your policy.',
        summary_enrolled: '{num} users are enrolled in this network.',
        summary_phish: '{num} incidents of phishing activity detected.',
        summary_vuln: 'There are {num} active vulnerabilities.',
        population: 'All Devices',
        ring_summary: 'Trust Group Summary',
        ring_ok: 'Devices scanned, no current vulnerabilties',
        ring_not_scanned: 'Devices not yet scanned',
        ring_vulnerable: 'Devices scanned, Vulnerability detected',
        ring_inactive: 'Inactive Devices',
        vulnScan_active: 'Active Devices Vulnerability Scanned',
        active_violations: 'Active Violations',
        no_active_violations: 'No active vulnerabilities.',
        resolved_violations: 'Resolved Violations',
        no_resolved_violations: 'No resolved violations.',
        security_rings: 'Trust Groups',
      },
      devices: {
        title: 'Devices',
        num_alerts: '{count} Alert | {count} Alerts',
        show_recent: 'Show Recent Attempts...',
        hide_recent: 'Hide Recent Attempts...',
        cats: {
          recent: 'Recent Attempted Connections',
          phone: 'Phones & Tablets',
          computer: 'Computers',
          printer: 'Printers/Scanners',
          media: 'Media',
          iot: 'Things',
          unknown: 'Unknown or Unidentifiable',
        },
        rings: {
          unenrolled: 'Unenrolled',
          quarantine: 'Quarantine',
          core: 'Core',
          standard: 'Standard',
          devices: 'Devices',
          guest: 'Guest',
          internal: 'Internal',
        },
      },
      dev_details: {
        title: 'Device Details',
        device: 'Device',
        name: 'Name',
        name_admin: 'Set by an administrator',
        name_auto: 'Default name',
        rename_title: 'Device Name',
        rename_entry: 'Enter new name (example: "Michelle\'s PC"); clear to revert to default name.',
        rename_entry_err: 'Failed to change name for {dev} to {newFriendly}: {err}',
        ipv4_addr: 'IPv4 Address',
        ipv4_addr_none: 'None',
        hw_addr: 'Hardware Address',
        access_control: 'Access Control',
        security_ring: 'Trust Group',
        ring_tip: 'Affects the security protections and network access for this device. See admin guide.',
        vuln_scan: 'Vulnerability Scan',
        vuln_scan_notyet: 'Not Scanned Yet',
        vuln_scan_initial: 'Initial Scan in Progress',
        vuln_scan_rescan: 'Rescan in Progress',
        activity: 'Activity',
        active_true: 'Active',
        active_false: 'Inactive',
        connection: 'Network',
        vuln_more_info: 'More Information ({source})',
        vuln_remediation: 'Remediation: {text}',
        vuln_first_detected: 'Reported: {time}',
        vuln_latest_detected: 'Recently Seen: {time}',
        vuln_repaired: 'Repaired: {time}',
        vuln_details: 'Details:',
        wired_port: 'Wired Network',
        change_ring_err: 'Failed to change trust group for {dev} to {ring}: {err}',
        signal_strength: 'Signal Strength',
        SSID: 'SSID',
        user_name: 'User Name',
        attributes: 'Client Attributes',
        dhcp_id: 'DHCP ID',
        dhcp_id_none: 'None',
        dhcp_id_tip: 'Name presented by the device when it joined the network.',
        dns_name: 'DNS Name',
        dns_name_none: 'Not set',
        dns_name_tip: 'The gateway\'s built-in DNS (Domain Name System) server publishes a record for this client using the name indicated. Clients on the local network can reference this system using this name.',
        devid_tip: 'Based on observation of this device\'s network activity.',
        os_name: 'Operating System',
        os_name_none: 'Unknown',
        model_name: 'Hardware Model',
        model_name_none: 'Unknown',
      },
      enroll_guest: {
        title: 'Enroll Guest',
        header: 'Here are three simple ways to enroll a guest\'s device onto your guest network.',
        direct_subhead: 'Tell the guest the password',
        network_name: 'Network',
        network_passphrase: 'Password',
        sms_subhead: 'Send the guest the password',
        qr_subhead: 'Show the guest this code',
        qr_explain: 'Use the camera app on supporting devices, including modern Android and iOS 11+.',
        phone: 'Guest Phone Number',
        phone_placeholder: '(650) 555-1212',
        send_sms: 'Text Guest',
        sending: 'Sending',
        psk_network: 'PSK Network',
        psk_success: 'Great!  Your guest should receive an SMS momentarily with the network name and password.',
        sms_failure: 'Oops, something went wrong sending the SMS message.',
      },
      login: {
        login: 'Login',
        username: 'User Name',
        password: 'Password',
        sign_in: 'Sign In',
        fail_unauthorized: 'Login failed. Invalid user name or password.',
        fail_other: 'Login failed unexpectedly: {err}.',
        oauth2_google: 'Sign in with Google',
        oauth2_microsoft: 'Sign in with Microsoft',
        oauth2_other: 'Sign in with {provider}',
        down: 'The login system is not working right now. It will be back shortly.',
        no_oauth_rule: 'An account for {email} (via {provider}) could not be created. This account could not be linked to any known Brightgate customers.',
        server_error: 'The server experienced an unexpected error during login.',
        no_roles: 'The account {email} exists, but currently has no roles assigned; it cannot login.',
        unknown_error: 'An unknown error occurred. Please contact service for help.',
      },
      users: {
        title: 'Users',
        cloud_self_provisioned: 'Cloud Accounts with Wi-Fi Access',
        cloud_self_provisioned_explain: 'Wi-Fi self-provisioning is complete for these accounts. They may log in to the {wifi} Wi-Fi network.',
        none_provisioned_yet: 'None so far',
        manage_accounts: 'Manage all accounts',
        site_specific: 'Site-Specific Administrators',
        site_specific_add: 'Add Site-Specific Administrator',
        site_specific_explain: 'These administrators may access both the {wifi} Wi-Fi network and the appliance\'s local web user interface to troubleshoot, if connection to the Brightgate cloud has been lost.',
      },
      accounts: {
        title: 'Accounts',
        none_yet: 'There are no accounts yet',
      },
      account_details: {
        title: 'Account Details',
        delete_text: '{name}\'s account will be removed from {org}\'s organization. Subscription fees associated with this account will no longer be charged.',
        delete_title: 'Delete Account?',
        administration: 'Account Administration',
        wifi_login: 'Wi-Fi Login',
        manage_roles: 'Manage Roles',
        deprovision_button: 'Deprovision',
        delete_account: 'Delete Account',
        delete_button: 'Delete',
        last_provisioned: 'Provisioned: {last}',
        not_provisioned: 'Not Provisioned',
        email_none: 'None',
        phone_none: 'None',
      },
      account_roles: {
        title: 'Manage Account Roles',
        roles_group: 'Roles',
      },
      user_details: {
        title: 'User Details',
        user_name: 'User Name',
        user_type: 'User Type',
        email: 'Email',
        email_placeholder: 'User\'s email address',
        name: 'Name',
        name_placeholder: 'User\'s real name',
        phone: 'Phone',
        phone_placeholder: '+1 650-555-1212',
        role: 'Role',
        roles: {
          user: 'User',
          admin: 'Administrator',
        },
        password: 'Password',
        add_title: 'Add Site-Specific Administrator',
        edit_title: 'Edit Site-Specific Administrator',
        create_ok: 'Created user {name}',
        create_fail: 'Failed to create new user: {err}',
        save_ok: 'Updated user {name}',
        save_fail: 'Failed to update: {err}',
        delete_ok: 'Deleted user {name}',
        delete_fail: 'Failed to delete: {err}',
      },
      nodes: {
        title: 'Hardware',
        gateway_role: 'Role: Gateway',
        satellite_role: 'Role: Satellite',
        unnamed_hw: 'Unnamed ({id})',
      },
      node_lan_port: {
        title: 'LAN Port Details',
        port_label: 'Port Label',
        port_ring: 'Port Trust Group',
        change_ring_err: 'Failed to change trust group for port {nic} to {ring}: {err}',
      },
      node_radio: {
        title: 'Radio Details',
        radio_label: 'Radio ID',
        band_label: 'Frequency Band',
        protocol_label: 'Protocols',
        auto_band: '{band}',
        config_band: '{band} (Manually configured)',
        width_label: 'Width',
        auto_width: '{width}MHz',
        config_width: '{width}MHz (Manually configured)',
        channel_label: 'Channel',
        change_channel_err: 'Failed to change channel for radio {nic} to {channel}: {err}',
        channel_automatic: 'Automatic ({active})',
        channel_automatic_no_channel: 'Automatic',
      },
      node_details: {
        title: 'Hardware Details',
        unnamed_hw: 'Unnamed ({id})',
        name: 'Name',
        role: 'Role',
        gateway: 'Gateway',
        satellite: 'Satellite',
        serial_number: 'Serial Number',
        sn_none: 'None',
        change_name_err: 'Failed to change name to {name}: {err}',
        radios: 'Wi-Fi Radios',
        wifi_radio: 'Wi-Fi Radio {silkscreen}',
        wifi_details: '{band}, Channel: {channel}, Width: {width}MHz',
        ports: 'Ethernet Ports',
        wan_port: 'WAN Port',
        lan_port: 'LAN Port {silkscreen}',
        rename_title: 'Node Name',
        rename_text: 'Enter new name (example: "North Side Satellite")',
      },
    },
  },
  de: {
    message: {
      test_tools: {}, // Note: We don't internationalize this group
      api: { // Messages from the store, XXX1I8N
      },
      home: { // XXXI18N
      },
      site_admin: { // XXXI18N
      },
      site_list: { // XXXI18N
      },
      site_controls: { // XXXI18N
      },
      alerts: {
        serious_alerts: 'Schwerwiegende Warnungen',
        vulnerability: 'Vulnerability', // XXXI18N
        problem_on_device: '{problem} auf {device}', // XXXI18N
      },
      site_alert: { // XXXI18N
      },
      notifications: {
        notifications: 'Benachrichtigungen',
        update_device: '⚠️  {device} aktualisieren',
        self_provision_title: 'Your Account', // XXXI18N
        self_provision_text: 'Your Wi-Fi login needs to be provisioned', // XXXI18N
        security_notifications: '⚠️ Sicherheitshinweis',
        msg: {
          '0': 'Dieses Gerät ist unsicher wegen veralteter Software.',
          '1': 'Brightgate kann dieses Gerät nicht automatisch aktualisieren.',
          '2': 'Folgen Sie den Hinweisen des Herstellers um die Software zu aktualisieren.',
        },
      },
      general: {
        accept: 'Akzeptieren',
        back: 'Zurück',
        cancel: 'Abbrechen',
        close: 'Schließen',
        confirm: 'Fortsetzen',
        details: 'Details',
        login: 'Anmelden',
        logout: 'Abmelden',
        need_login: 'Sie müssen angemeldet sein',
        save: 'Save', // XXXI18N
      },
      account_prefs: { // XXXI18N
      },
      self_provision: { // XXXI18N
      },
      network: { // XXXI18N
      },
      network_vap: { // XXXI18N
      },
      network_vap_editor: { // XXXI18N
      },
      compliance_report: { // XXXI18N
      },
      devices: {
        title: 'Geräte',
        show_recent: 'Neueste Versuche zeigen ...',
        hide_recent: 'Neueste Versuche verbergen ...',
        cats: {
          recent: 'Neueste Verbindungsversuche',
          phone: 'Handies & Tablets',
          computer: 'Computers',
          printer: 'Printers/Scanners', // XXXI18N
          media: 'Media',
          iot: 'Unidentifiziertes Verbindungsobjekt',
          unknown: 'Unknown or Unidentifiable', // XXXI18N
        },
      },
      dev_details: {
        title: 'Device Details', // XXXI18N
        device: 'Gerät',
        name: 'Name',
        ipv4_addr: 'IPv4 Address',       // XXXI18N
        ipv4_addr_none: 'None',          // XXXI18N
        hw_addr: 'Hardware Address',     // XXXI18N
        os_version: 'OS Version',
        os_version_unknown: 'Unknown', // XXXI18N
        access_control: 'Zugangskontrolle',
        security_ring: 'Trust Group', // XXXI18N
        vuln_scan: 'Vulnerability Scan', // XXXI18N
        vuln_scan_notyet: 'Not Scanned Yet',  // XXXI18N
        vuln_scan_initial: 'Initial Scan in Progress',  // XXXI18N
        vuln_scan_rescan: 'Rescan in Progress',  // XXXI18N
        activity: 'Activity',         // XXXI18N
        active_true: 'Active',        // XXXI18N
        active_false: 'Inactive',     // XXXI18N
        conn_vap: 'WiFi Network', // XXXI18N
        vuln_more_info: 'More Information ({source})', // XXXI18N
        vuln_remediation: 'Remediation: {text}', // XXXI18N
        vuln_first_detected: 'Reported: {time}', // XXXI18N
        vuln_latest_detected: 'Recently Seen: {time}', // XXXI18N
        vuln_repaired: 'Repaired: {time}', // XXXI18N
        vuln_details: 'Details:', // XXXI18N
        wired_port: 'Wired Network', // XXXI18N
        change_ring_err: 'Failed to change trust group for {dev} to {ring}: {err}', // XXXI18N
        signal_strength: 'Signal Strength', // XXXI18N
        SSID: 'SSID', // XXXI18N
        user_name: 'User Name', // XXXI18N
        attributes: 'Client Attributes', // XXXI18N
        dhcp_id: 'DHCP ID', // XXXI18N
        dhcp_id_none: 'None', // XXXI18N
        dhcp_id_tip: 'Name presented by the device when it joined the network.', // XXXI18N
        dns_name: 'DNS Name', // XXXI18N
        dns_name_none: 'Not set', // XXXI18N
        dns_name_tip: 'The gateway\'s built-in DNS (Domain Name System) server publishes a record for this client using the name indicated. Clients on the local network can reference this system using this name.', // XXXI18N
        devid_tip: 'Based on observation of this device\'s network activity.', // XXXI18N
        os_name: 'Operating System', // XXXI18N
        os_name_none: 'Unknown', // XXXI18N
        model_name: 'Hardware Model', // XXXI18N
        model_name_none: 'Unknown', // XXXI18N
      },
      enroll_guest: {
        title: 'Gastbenutzer Registrieren',
        header: 'Enroll Guest Users with Brightgate', // XXXI18N
        direct_subhead: 'Manually', // XXXI18N
        sms_subhead: 'Via SMS', // XXXI18N
        qr_subhead: 'Via QR Code', // XXXI18N
        phone: 'Telefonnummer des Gastbenutzers',
        phone_placeholder: 'Telefonnummer',
        send_sms: 'SMS versenden',
        sending: 'Bitte Warten',
        psk_network: 'PSK Network', // XXXI18N
        psk_success: 'Super! Der Gastbenutzer sollte in einem Moment eine SMS mit Netzwerkname und Paßwort erhalten.',
        sms_failure: 'Oop der Versendung von der SMS ist ein Fehler aufgetreten',
      },
      login: {
        login: 'Login',
        username: 'Name',
        password: 'Passwort',
        sign_in: 'Anmelden',
        fail_unauthorized: 'Login failed. Invalid user name or password.', // XXXI18N
        fail_other: 'Login failed unexpectedly: {err}.', // XXXI18N
        oauth2_with_google: 'Login with Google', // XXXI18N
        oauth2_with_microsoft: 'Login with Microsoft', // XXXI18N
        oauth2_with_other: 'Login with {provider}', // XXXI18N
        down: 'The login system is not working right now. It will be back shortly.', // XXXI18N
      },
      users: {
        title: 'Benutzer',
        site_specific: 'Site-Specific Users', // XXXI18N
        add_site_specific: 'Add Site-Specific User', // XXXI18N
        cloud_self_provisioned: 'Cloud-Self-Provisioned Users', // XXXI18N
      },
      user_details: {
        title: 'User Details',  // XXXI18N
        user_name: 'User Name', // XXXI18N
        user_type: 'User Type', // XXXI18N
        uuid: 'UUID',
        role: 'Rolle',
        roles: {
          user: 'User',          // XXXI18N
          admin: 'Administrator', // XXXI18N
        },
        password: 'Passwort',
        edit_title: 'Edit User',                              // XXXI18N
        create_ok: 'Created user {name}',                     // XXXI18N
        create_fail: 'Failed to create new user: {err}',      // XXXI18N
        save_ok: 'Updated user {name}',                       // XXXI18N
        save_fail: 'Failed to update: {err}',                 // XXXI18N
        delete_ok: 'Deleted user {name}',                     // XXXI18N
        delete_fail: 'Failed to delete: {err}',               // XXXI18N
      },
    },
  },
};
