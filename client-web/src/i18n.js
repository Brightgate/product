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
      status: {
        brightgate_status: 'Brightgate Status',
      },
      testing: {
        testing: 'For Test Purposes',
        accept_devices: 'Accept Devices',
        accept_success: 'Devices Acceptance succeeded. {devicesChanged} devices were affected.',
        accept_fail: 'There was an error accepting devices: <b>{reason}.</b>',
        enable_mock: 'Enable Mock',
        enable_fakelogin: 'Simulate being Logged In',
      },
      alerts: {
        serious_alerts: 'Serious Alerts',
        wannacry: '🚫 WannaCry on {device}',
        important_alert: '🚫 Important Alert!',
        msg: {
          '0': 'Brightgate detected WannaCry ransomware on this device.',
          '1': 'For your security, Brightgate has disconnected it from the network and attempted to prevent the ransomware from encrypting more files.',
          '2': 'Visit brightgate.com from another computer for more help.',
        },
      },
      tools: {
        tools: 'Tools',
        manage_devices: 'Manage Devices ({device_count})',
        enroll_guest: 'Enroll a Guest User',
        login: 'Login',
        logout: 'Logout',
        users: 'Users',
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
      details: {
        details: {
          _details: ' - Details',
          device_details: 'Device Details',
          device: 'Device',
          network_name: 'Network Name',
          os_version: 'OS Version',
          activated: 'Activated',
          owner: 'Owner',
          owner_email: 'Owner Email',
          owner_phone: 'Owner Phone',
        },
        access: {
          access_control: 'Access Control',
          security_ring: 'Security Ring',
          guest_access: {
            time: 'Guest access expires in {time}.',
            extend: 'Extend 1h',
            pause: 'Pause',
            unpause: 'Unpause',
            remove: 'Remove',
            confirm_remove: 'Please confirm you\'d like to permanently remove the device \'{device}\' from the network.',
          },
          status: 'Status',
          blocked: 'Blocked 🚫',
          blocked_text: 'For your security, Brightgate has blocked this device from your network.  See the alert above for more details.',
          normal: 'Normal ✅',
        },
        activity: {
          activity: 'Activity',
          dates: {
            today: 'Today',
            yesterday: 'Yesterday',
            sunday: 'Sunday',
            saturday: 'Saturday',
          },
          older: 'Older ...',
          not_supported: 'Earlier logs are not yet supported.',
        },
        add_info: {
          additional_information: 'Additional Information',
        },
      },
      enroll: {
        title: 'Brightgate - Enroll Guest',
        header: 'Register Guests with Brightgate',
        subheader: 'Help a guest get online using their phone number',
        phone: 'Guest Phone Number',
        phone_placeholder: 'Phone #',
        send_sms: 'Text Guest',
        sending: 'Sending',
        send_success: 'Great!  Your guest should receive an SMS momentarily with the network name and password.',
        send_failure: 'Oops, something went wrong sending the SMS message.',
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
        create_user_ok: 'Created user {name}',
        create_user_fail: 'Failed to create new user: {err}',
        save_user_ok: 'Updated user {name}',
        save_user_fail: 'Failed to create new user: {err}',
      },
    },
  },
  de: {
    message: {
      status: {
        brightgate_status: 'Brightgate Status',
      },
      testing: {
        testing: 'Für Testzwecke',
        accept_devices: 'Akzeptiere Geräte',
        accept_success: 'Akzeptanz war erfolgreich. {devicesChanged} Geräte wurden akzeptiert.',
        accept_fail: 'Fehlermeldung: <b>{reason}.</b>',
        enable_mock: 'Testmodus',
        enable_fakelogin: 'Simulierte Anmeldung',
      },
      alerts: {
        serious_alerts: 'Schwerwiegende Warnungen',
        wannacry: '🚫 WannaCry auf {device} entdeckt',
        important_alert: '🚫 Dringende Warnung!',
        msg: {
          '0': 'Brightgate hat Kryptotrojaner WannaCry auf diesem Gerät entdeckt.',
          '1': 'Zu Ihrer Sicherheit hat Brightgate es vom Netzwerk getrennt und versucht, es von der Verschlüsselung weiterer Dateien abzuhalten.',
          '2': 'Besuchen Sie brightgate.com auf einem anderen Computer für mehr Hilfe.',
        },
      },
      tools: {
        tools: 'Werkzeuge',
        manage_devices: 'Geräte verwalten ({device_count})',
        enroll_guest: 'Registrieren Sie einen Gastbenutzer',
        login: 'Anmelden',
        logout: 'Abmelden',
        users: 'Benutzer',
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
      details: {
        details: {
          _details: ' - Details',
          device_details: 'Gerätdetails',
          device: 'Gerät',
          network_name: 'Netzwerk Name',
          os_version: 'OS Version',
          activated: 'Aktiviert',
          owner: 'Besitzer',
          owner_email: 'Email',
          owner_phone: 'Mobil',
        },
        access: {
          access_control: 'Zugangskontrolle',
          security_ring: 'Ring',
          guest_access: {
            time: 'Gastzugang läuft in {time} ab.',
            extend: '1h verlängern',
            pause: 'Pausieren',
            unpause: 'Fortsetzen',
            remove: 'Entfernen',
            confirm_remove: 'Möchten Sie das Gerät \'{device}\' permanent vom Netzwerk entfernen?',
          },
          status: 'Status',
          blocked: 'Geblockt 🚫',
          blocked_text: 'Zu Ihrer Sicherheit hat Brightgate dieses Gerät von Ihrem Netzwerk getrennt. Sehen Sie die Warnung weiter oben für mehr Details.',
          normal: 'Normal ✅',
        },
        activity: {
          activity: 'Aktivität',
          dates: {
            today: 'Heute',
            yesterday: 'Gestern',
            sunday: 'Sonntag',
            saturday: 'Samstag',
          },
          older: 'Ältere Einträge',
          not_supported: 'Ältere Einträge werden derzeit noch nicht unterstützt.',
        },
        add_info: {
          additional_information: 'Weitere Informationen',
        },
      },
      enroll: {
        title: 'Brightgate – Gastbenutzer Registrieren',
        header: 'Registrieren Sie Gastbenutzer bei Brightgate',
        subheader: 'Helfen Sie einem Gastbenutzer sich zu registrieren mit seinem Telefonnummer',
        phone: 'Telefonnummer des Gastbenutzers',
        phone_placeholder: 'Telefonnummer',
        send_sms: 'SMS versenden',
        sending: 'Bitte Warten',
        send_success: 'Super! Der Gastbenutzer sollte in einem Moment eine SMS mit Netzwerkname und Paßwort erhalten.',
        send_failure: 'Oop der Versendung von der SMS ist ein Fehler aufgetreten',
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
        create_user_ok: 'Created user {name}',                // XXXI18N
        create_user_fail: 'Failed to create new user: {err}', // XXXI18N
        save_user_ok: 'Updated user {name}',                  // XXXI18N
        save_user_fail: 'Failed to create new user: {err}',   // XXXI18N
      },
    },
  },
};
