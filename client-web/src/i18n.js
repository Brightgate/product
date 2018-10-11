/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Ready translated locale messages
export default {
  en: {
    message: {
      home: {
        testing: {
          testing: 'For Test Purposes',
          accept_devices: 'Accept Devices',
          accept_success: 'Devices Acceptance succeeded. {devicesChanged} devices were affected.',
          accept_fail: 'There was an error accepting devices: <b>{reason}.</b>',
          enable_mock: 'Enable Mock',
          enable_fakelogin: 'Simulate being Logged In',
          switch_appliance: 'Switch Appliance',
        },
        tools: {
          tools: 'Tools',
          site_status: 'Site Status',
          compliance_report: 'Compliance Report',
          manage_devices: 'Manage Devices ({device_count})',
          enroll_guest: 'Enroll a Guest User',
          login: 'Login',
          logout: 'Logout',
          users: 'Users',
        },
      },
      alerts: {
        serious_alerts: 'Serious Alerts',
        problem_on_device: '{problem} on {device}',
      },
      notifications: {
        notifications: 'Notifications',
        update_device: '⚠️  Update device {device}',
        security_notifications: '⚠️ Security Notifications',
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
        need_login: 'You must be logged in',
      },
      site_status: {
        title: 'Brightgate - Site Status',
        ssids: 'SSIDs',
        ssid_psk: 'Pre-Shared Key SSID',
        ssid_eap: 'Enterprise Auth SSID',
        devices: 'Device Summary',
        devices_reg: 'Registered Devices',
        devices_active: 'Active Devices',
        devices_scanned: 'Vulnerability Scanned Devices',
        config: 'Configuration',
        config_dns_server: 'DNS Server',
        config_default_ring_wpa_eap: 'Default Ring (WPA-EAP)',
        config_default_ring_wpa_psk: 'Default Ring (WPA-PSK)',
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
        ring_summary: 'Security Ring Summary',
        ring_ok: 'Devices scanned, no current vulnerabilties',
        ring_not_scanned: 'Devices not yet scanned',
        ring_vulnerable: 'Devices scanned, Vulnerability detected',
        ring_inactive: 'Inactive Devices',
        vulnScan_active: 'Active Devices Vulnerability Scanned',
        active_violations: 'Active Violations',
        no_active_violations: 'No active vulnerabilities.',
        resolved_violations: 'Resolved Violations',
        no_resolved_violations: 'No resolved violations.',
        security_rings: 'Security Rings',
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
        uncertain_device: '(Tentative Device Identification)', // XXXI18N
        unknown_model: 'Unknown',
        unknown_manufacturer: 'Unknown',
        network_name: 'Network Name',
        ipv4_addr: 'IPv4 Address',
        ipv4_addr_none: 'None',
        hw_addr: 'Hardware Address',
        os_version: 'OS Version',
        os_version_unknown: 'Unknown',
        access_control: 'Access Control',
        security_ring: 'Security Ring',
        vuln_scan: 'Vulnerability Scan',
        vuln_scan_notyet: 'Not Scanned Yet',
        vuln_scan_initial: 'Initial Scan in Progress',
        vuln_scan_rescan: 'Rescan in Progress',
        activity: 'Activity',
        active_true: 'Active',
        active_false: 'Inactive',
        vuln_more_info: 'More Information',
        vuln_remediation: 'Remediation: {text}',
        vuln_first_detected: 'Reported: {time}',
        vuln_latest_detected: 'Recently Seen: {time}',
      },
      enroll_guest: {
        title: 'Brightgate - Enroll Guest',
        header: 'Register Guests with Brightgate',
        subheader: 'Help a guest get online using their phone number',
        phone: 'Guest Phone Number',
        phone_placeholder: 'Phone #',
        email: 'Guest Email',
        email_placeholder: 'user@example.com',
        send_sms: 'Text Guest',
        sending: 'Sending',
        psk_network: 'PSK Network',
        eap_network: 'EAP Network',
        psk_success: 'Great!  Your guest should receive an SMS momentarily with the network name and password.',
        eap_success: 'We made a user, <i>{name}</i> and generated a secure password for your guest.  They should receive an SMS momentarily with the network name, username and password.',
        sms_failure: 'Oops, something went wrong sending the SMS message.',
      },
      login: {
        login: 'Login',
        username: 'Username',
        password: 'Password',
        sign_in: 'Sign In',
        fail_unauthorized: 'Login failed.  Invalid Username or Password.',
        fail_other: 'Login failed unexpectedly: {err}.',
      },
      users: {
        title: 'Brightgate - Users',
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
      home: {
        testing: {
          testing: 'Für Testzwecke',
          accept_devices: 'Akzeptiere Geräte',
          accept_success: 'Akzeptanz war erfolgreich. {devicesChanged} Geräte wurden akzeptiert.',
          accept_fail: 'Fehlermeldung: <b>{reason}.</b>',
          enable_mock: 'Testmodus',
          enable_fakelogin: 'Simulierte Anmeldung',
          switch_appliance: 'Switch Appliance', // XXXI18N
        },
        tools: {
          tools: 'Werkzeuge',
          site_status: 'Site Status', // XXXI18N
          compliance_report: 'Compliance Report', // XXXI18N
          manage_devices: 'Geräte verwalten ({device_count})',
          enroll_guest: 'Registrieren Sie einen Gastbenutzer',
          login: 'Anmelden',
          logout: 'Abmelden',
          users: 'Benutzer',
        },
      },
      alerts: {
        serious_alerts: 'Schwerwiegende Warnungen',
        problem_on_device: '{problem} auf {device}', // XXXI18N
      },
      notifications: {
        notifications: 'Benachrichtigungen',
        update_device: '⚠️  {device} aktualisieren',
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
        need_login: 'Sie müssen angemeldet sein',
      },
      site_status: { // XXXI18N
        title: 'Brightgate- Site Status',
        ssids: 'SSIDs',
        ssid_psk: 'Pre-Shared Key SSID',
        ssid_eap: 'Enterprise Auth SSID',
        devices: 'Device Summary',
        devices_reg: 'Registered Devices',
        devices_active: 'Active Devices',
        devices_scanned: 'Vulnerability Scanned Devices',
        config: 'Configuration',
        config_dns_server: 'DNS Server',
        config_default_ring_wpa_eap: 'Default Ring (WPA-EAP)',
        config_default_ring_wpa_psk: 'Default Ring (WPA-PSK)',
      },
      compliance_report: { // XXX I18N
        title: 'Brightgate - Compliance Report',
        summary: 'Summary',
        summary_violations: 'There are no policy violations. | There is one policy violation.  Correct it to stay compliant. | There are {num} policy violations.  Correct these to stay compliant.',
        summary_no_violations: 'This network is currently in compliance with your policy.',
        summary_enrolled: '{num} users are enrolled in this network.',
        summary_phish: '{num} incidents of phishing activity detected.',
        summary_vuln: 'There are {num} active vulnerabilities.',
        population: 'All Devices',
        ring_summary: 'Security Ring Summary',
        ring_ok: 'Devices scanned, no current vulnerabilties',
        ring_not_scanned: 'Devices not yet scanned',
        ring_vulnerable: 'Devices scanned, Vulnerability detected',
        ring_inactive: 'Inactive Devices',
        vulnScan_active: 'Active Devices Vulnerability Scanned',
        active_violations: 'Active Violations',
        no_active_violations: 'No active vulnerabilities.',
        resolved_violations: 'Resolved Violations',
        no_resolved_violations: 'No resolved violations.',
        security_rings: 'Security Rings',
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
        security_ring: 'Ring',
        vuln_scan: 'Vulnerability Scan', // XXXI18N
        vuln_scan_notyet: 'Not Scanned Yet',  // XXXI18N
        vuln_scan_initial: 'Initial Scan in Progress',  // XXXI18N
        vuln_scan_rescan: 'Rescan in Progress',  // XXXI18N
        activity: 'Activity',         // XXXI18N
        active_true: 'Active',        // XXXI18N
        active_false: 'Inactive',     // XXXI18N
        vuln_more_info: 'More Information', // XXXI18N
        vuln_remediation: 'Remediation: {text}', // XXXI18N
        vuln_first_detected: 'Reported: {time}', // XXXI18N
        vuln_latest_detected: 'Recently Seen: {time}', // XXXI18N
      },
      enroll_guest: {
        title: 'Brightgate – Gastbenutzer Registrieren',
        header: 'Registrieren Sie Gastbenutzer bei Brightgate',
        subheader: 'Helfen Sie einem Gastbenutzer sich zu registrieren mit seinem Telefonnummer',
        phone: 'Telefonnummer des Gastbenutzers',
        phone_placeholder: 'Telefonnummer',
        email: 'Guest Email',         // XXXI18N
        email_placeholder: 'user@example.com', // XXXI18N
        send_sms: 'SMS versenden',
        sending: 'Bitte Warten',
        psk_network: 'PSK Network', // XXXI18N
        eap_network: 'EAP Network', // XXXI18N
        psk_success: 'Super! Der Gastbenutzer sollte in einem Moment eine SMS mit Netzwerkname und Paßwort erhalten.',
        eap_success: 'We made a user, <i>{name}</i> and generated a secure password for your guest.  They should receive an SMS momentarily with the network name, username and password.', // XXXI18N
        sms_failure: 'Oop der Versendung von der SMS ist ein Fehler aufgetreten',
      },
      login: {
        login: 'Login',
        username: 'Name',
        password: 'Passwort',
        sign_in: 'Anmelden',
        fail_unauthorized: 'Login failed.  Invalid Username or Password.', // XXXI18N
        fail_other: 'Login failed unexpectedly: {err}.', // XXXI18N
      },
      users: {
        title: 'Brightgate - Benutzer',
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
