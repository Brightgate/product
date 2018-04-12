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
        status_block: 'Brightgate Status',
        testing: {
          testing: 'For Test Purposes',
          accept_devices: 'Accept Devices',
          accept_success: 'Devices Acceptance succeeded. {devicesChanged} devices were affected.',
          accept_fail: 'There was an error accepting devices: <b>{reason}.</b>',
          enable_mock: 'Enable Mock',
          enable_fakelogin: 'Simulate being Logged In',
        },
        tools: {
          tools: 'Tools',
          site_status: 'Site Status',
          manage_devices: 'Manage Devices ({device_count})',
          enroll_guest: 'Enroll a Guest User',
          login: 'Login',
          logout: 'Logout',
          users: 'Users',
        },
      },
      alerts: {
        serious_alerts: 'Serious Alerts',
        wannacry: '🚫 WannaCry on {device}',
        important_alert: 'Important Alert!',
        msg: {
          '0': 'Brightgate detected WannaCry ransomware on this device.',
          '1': 'For your security, Brightgate has disconnected it from the network and attempted to prevent the ransomware from encrypting more files.',
          '2': 'Visit brightgate.com from another computer for more help.',
        },
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
        config_default_ring: 'Default Ring',
      },
      devices: {
        title: 'Brightgate - Devices',
        show_recent: 'Show Recent Attempts...',
        hide_recent: 'Hide Recent Attempts...',
        cats: {
          recent: 'Recent Attempted Connections',
          phone: 'Phones & Tablets',
          computer: 'Computers',
          media: 'Media',
          iot: 'Things',
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
        hw_addr: 'Hardware Address',
        os_version: 'OS Version',
        os_version_unknown: 'Unknown',
        owner: 'Owner',
        access_control: 'Access Control',
        security_ring: 'Security Ring',
        vuln_scan: 'Vulnerability Scan',
        vuln_scan_notyet: 'Not Yet',
        activity: 'Activity',
        active_true: 'Active',
        active_false: 'Inactive',
        access: 'Access Restriction',
        access_blocked: 'Blocked 🚫',
        access_blocked_text: 'For your security, Brightgate has blocked this device from your network.  See the alert above for more details.',
        access_normal: 'Normal✅',
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
        status_block: 'Brightgate Status',
        testing: {
          testing: 'Für Testzwecke',
          accept_devices: 'Akzeptiere Geräte',
          accept_success: 'Akzeptanz war erfolgreich. {devicesChanged} Geräte wurden akzeptiert.',
          accept_fail: 'Fehlermeldung: <b>{reason}.</b>',
          enable_mock: 'Testmodus',
          enable_fakelogin: 'Simulierte Anmeldung',
        },
        tools: {
          tools: 'Werkzeuge',
          site_status: 'Site Status', // XXXI18N
          manage_devices: 'Geräte verwalten ({device_count})',
          enroll_guest: 'Registrieren Sie einen Gastbenutzer',
          login: 'Anmelden',
          logout: 'Abmelden',
          users: 'Benutzer',
        },
      },
      alerts: {
        serious_alerts: 'Schwerwiegende Warnungen',
        wannacry: '🚫 WannaCry auf {device} entdeckt',
        important_alert: 'Dringende Warnung!',
        msg: {
          '0': 'Brightgate hat Kryptotrojaner WannaCry auf diesem Gerät entdeckt.',
          '1': 'Zu Ihrer Sicherheit hat Brightgate es vom Netzwerk getrennt und versucht, es von der Verschlüsselung weiterer Dateien abzuhalten.',
          '2': 'Besuchen Sie brightgate.com auf einem anderen Computer für mehr Hilfe.',
        },
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
        config_default_ring: 'Default Ring',
      },
      devices: {
        title: 'Brightgate - Geräte',
        show_recent: 'Neueste Versuche zeigen ...',
        hide_recent: 'Neueste Versuche verbergen ...',
        cats: {
          recent: 'Neueste Verbindungsversuche',
          phone: 'Handies & Tablets',
          computer: 'Computers',
          media: 'Media',
          iot: 'Unidentifiziertes Verbindungsobjekt',
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
        hw_addr: 'Hardware Address',     // XXXI18N
        os_version: 'OS Version',
        os_version_unknown: 'Unknown', // XXXI18N
        owner: 'Besitzer',
        access_control: 'Zugangskontrolle',
        security_ring: 'Ring',
        vuln_scan: 'Vulnerability Scan', // XXXI18N
        vuln_scan_notyet: 'Not Yet',  // XXXI18N
        activity: 'Activity',         // XXXI18N
        active_true: 'Active',        // XXXI18N
        active_false: 'Inactive',     // XXXI18N
        access: 'Access Restriction', // XXXI18N
        access_blocked: 'Geblockt 🚫',
        access_blocked_text: 'Zu Ihrer Sicherheit hat Brightgate dieses Gerät von Ihrem Netzwerk getrennt. Sehen Sie die Warnung weiter oben für mehr Details.',
        access_normal: 'Normal ✅',
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
