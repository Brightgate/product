/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
      home: {
        local_site: 'Local Site',
        local_site_explanation: 'You are administering the Brightgate Wi-fi appliances on your local network (the Local Site).  Changes will also be reflected in the Brightgate cloud.',
        select_site: 'Select Site',
        tools: 'Tools',
      },
      site_list: {
        current: 'Current',
      },
      site_controls: {
        site_status: 'Site Status',
        network: 'Network Configuration',
        compliance_report: 'Compliance Report',
        manage_devices: 'Manage Devices ({device_count})',
        enroll_guest: 'Enroll a Guest User',
        users: 'Users',
      },
      alerts: {
        serious_alerts: 'Serious Alerts',
        problem_on_device: '{problem} on {device}',
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
        details: 'Details',
        login: 'Login',
        logout: 'Logout',
        need_login: 'You must be logged in',
      },
      account_prefs: {
        title: 'Account Preferences',
        self_provision: 'Wi-Fi Self Provisioning',
      },
      self_provision: {
        title: 'Wi-Fi Self Provisioning',
      },
      network: {
        title: 'Networks',
        names: {
          eap: 'Authorized User Network',
          psk: 'Devices Network',
          guest: 'Guest Network',
        },
        config: 'Configuration',
        config_dns_server: 'DNS Server',
        config_wan_current: 'Current WAN Address',
        config_24GHz: '2.4GHz Channel',
        config_5GHz: '5GHz Channel',
        descriptions: {
          eap: 'Unique ID and password; laptops, tablets, phones',
          psk: 'Shared password; IoT devices, printers, ...',
          guest: 'Wi-Fi guest users; password rotates periodically',
        },
        networks: 'Networks',
      },
      network_vap: {
        title: 'Network Details',
        descriptions: {
          eap: 'Each network user has their own login ID and password, created using the Brightgate App or Cloud Portal. Use it for laptops, tablets, and phones.',
          psk: 'All devices on this network share the same password.  Use it for IoT devices, such as printers, security cameras, or "smart" appliances.',
          guest: 'Guests use this network. The password rotates periodically.',
        },
        properties: 'Network Properties',
        key_mgmt: 'Authentication Method',
        default_tg: 'Default Trust Group',
        passphrase: 'Passphrase',
        ring_config: 'Trust Group Configuration',
      },
      site_status: {
        title: 'Brightgate - Site Status',
        networks: 'Networks',
        net_eap: 'Trusted User Network',
        net_psk: 'Devices Network',
        net_guest: 'Guest Network',
        devices: 'Device Summary',
        devices_reg: 'Registered Devices',
        devices_active: 'Active Devices',
        devices_scanned: 'Vulnerability Scanned Devices',
        config: 'Configuration',
      },
      compliance_report: {
        title: 'Brightgate - Compliance Report',
        summary: 'Summary',
        summary_violations: 'There are no policy violations. | There is one policy violation.  Correct it to stay compliant. | There are {num} policy violations.  Correct these to stay compliant.',
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
        title: 'Brightgate - Devices',
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
      },
      dev_details: {
        _details: ' - Details',
        device: 'Device',
        uncertain_device: '(Tentative Device Identification)',
        unknown_model: 'Unknown',
        unknown_manufacturer: 'Unknown',
        network_name: 'Network Name',
        ipv4_addr: 'IPv4 Address',
        ipv4_addr_none: 'None',
        hw_addr: 'Hardware Address',
        os_version: 'OS Version',
        os_version_unknown: 'Unknown',
        access_control: 'Access Control',
        security_ring: 'Trust Group',
        vuln_scan: 'Vulnerability Scan',
        vuln_scan_notyet: 'Not Scanned Yet',
        vuln_scan_initial: 'Initial Scan in Progress',
        vuln_scan_rescan: 'Rescan in Progress',
        activity: 'Activity',
        active_true: 'Active',
        active_false: 'Inactive',
        conn_auth: 'Authentication Protocol',
        conn_mode: 'Connection Mode',
        vuln_more_info: 'More Information ({source})',
        vuln_remediation: 'Remediation: {text}',
        vuln_first_detected: 'Reported: {time}',
        vuln_latest_detected: 'Recently Seen: {time}',
        vuln_repaired: 'Repaired: {time}',
        vuln_details: 'Details:',
      },
      enroll_guest: {
        title: 'Brightgate - Enroll Guest',
        header: 'Enroll Guest Users with Brightgate',
        direct_subhead: 'Manually',
        sms_subhead: 'Via SMS',
        qr_subhead: 'Via QR Code',
        phone: 'Guest Phone Number',
        phone_placeholder: 'Phone #',
        send_sms: 'Text Guest',
        sending: 'Sending',
        psk_network: 'PSK Network',
        psk_success: 'Great!  Your guest should receive an SMS momentarily with the network name and password.',
        sms_failure: 'Oops, something went wrong sending the SMS message.',
      },
      login: {
        login: 'Login',
        username: 'Username',
        password: 'Password',
        sign_in: 'Sign In',
        fail_unauthorized: 'Login failed.  Invalid Username or Password.',
        fail_other: 'Login failed unexpectedly: {err}.',
        oauth2_with_google: 'Login with Google',
        oauth2_with_microsoft: 'Login with Microsoft',
        oauth2_with_other: 'Login with {provider}',
        down: 'The login system is not working right now.  It will be back shortly.',
      },
      users: {
        title: 'Brightgate - Users',
        site_local: 'Site-Local Users',
        cloud_self_provisioned: 'Cloud-Self-Provisioned Users',
      },
      user_details: {
        username: 'Username',
        uuid: 'UUID',
        role: 'Role',
        roles: {
          user: 'User',
          admin: 'Administrator',
        },
        password: 'Password',
        edit_title: 'Edit User',
        create_ok: 'Created user {name}',
        create_fail: 'Failed to create new user: {err}',
        save_ok: 'Updated user {name}',
        save_fail: 'Failed to update: {err}',
        delete_ok: 'Deleted user {name}',
        delete_fail: 'Failed to delete: {err}',
      },
    },
  },
  de: {
    message: {
      test_tools: {}, // Note: We don't internationalize this group
      home: {
        local_site: 'Local Site', // XXXI18N
        local_site_explanation: 'You are administering the Brightgate Wi-fi appliances on your local network (the Local Site).  Changes will also be reflected in the Brightgate cloud.', // XXXI18N
        select_site: 'Select Site', // XXXI18N
        tools: 'Tools',
      },
      site_list: { // XXXI18N
      },
      site_controls: { // XXXI18N
      },
      alerts: {
        serious_alerts: 'Schwerwiegende Warnungen',
        problem_on_device: '{problem} auf {device}', // XXXI18N
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
      },
      account_prefs: { // XXXI18N
      },
      self_provision: { // XXXI18N
      },
      network: { // XXXI18N
      },
      network_vap: { // XXXI18N
      },
      site_status: { // XXXI18N
      },
      compliance_report: { // XXXI18N
      },
      devices: {
        title: 'Brightgate - Geräte',
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
        _details: ' - Details',
        device: 'Gerät',
        uncertain_device: '(Tentative Device Identification)', // XXXI18N
        unknown_model: 'Unknown',        // XXXI18N
        unknown_manufacturer: 'Unknown Manufacturer', // XXXI18N
        network_name: 'Netzwerk Name',
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
        conn_auth: 'Authentication Protocol', // XXXI18N
        conn_mode: 'Connection Mode',         // XXXI18N
        vuln_more_info: 'More Information ({source})', // XXXI18N
        vuln_remediation: 'Remediation: {text}', // XXXI18N
        vuln_first_detected: 'Reported: {time}', // XXXI18N
        vuln_latest_detected: 'Recently Seen: {time}', // XXXI18N
        vuln_repaired: 'Repaired: {time}', // XXXI18N
        vuln_details: 'Details:', // XXXI18N
      },
      enroll_guest: {
        title: 'Brightgate – Gastbenutzer Registrieren',
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
        fail_unauthorized: 'Login failed.  Invalid Username or Password.', // XXXI18N
        fail_other: 'Login failed unexpectedly: {err}.', // XXXI18N
        oauth2_with_google: 'Login with Google', // XXXI18N
        oauth2_with_microsoft: 'Login with Microsoft', // XXXI18N
        oauth2_with_other: 'Login with {provider}', // XXXI18N
        down: 'The login system is not working right now.  It will be back shortly.', // XXXI18N
      },
      users: {
        title: 'Brightgate - Benutzer',
        site_local: 'Site-Local Users', // XXXI18N
        cloud_self_provisioned: 'Cloud-Self-Provisioned Users', // XXXI18N
      },
      user_details: {
        username: 'Name',
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
