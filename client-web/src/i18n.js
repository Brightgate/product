// Import Vue
import Vue from 'vue'

// Import VueI18n
import VueI18n from 'vue-i18n'

// Import Browser Locale
import BrowserLocale from 'browser-locale'

// Init VueI18n Plugin
Vue.use(VueI18n)

// Ready translated locale messages
export const messages = {
  en: {
    message: {
      status: {
        brightgate_status: 'Brightgate Status',
        has_virus: '<b> One of your computers has a virus.</b><br/> See the "{serious_alerts}" below.',
        network_properly: 'Your network is working properly.',
      },
      testing: {
        testing: "For Test Purposes",
        enable_mock: "Enable Mock",
      },
      alerts: {
        serious_alerts: 'Serious Alerts',
        wannacry: '🚫&nbsp;&nbsp;WannaCry on {device}',
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
        open_setup_network: 'Open Setup Network',
        accept_devices: 'Accept Devices',
        success: 'Devices Acceptance succeeded. {devicesChanged} devices were affected.',
        fail: "There was an error accepting devices: <b>{devicesAcceptedError}.</b>",
      },
      notifications: {
        notifications: 'Notifications',
        update_device: '⚠️&nbsp;&nbsp;Update device {device}',
        security_notifications: '⚠️ Security Notifications',
        msg: {
          '0': 'This device is less secure because it is running old software.',
          '1': "Brightgate can't automatically update this device.",
          '2': "Follow the manufacturer's instructions to update its software.",
        },
      },
      general: {
        accept: 'Accept',
        back: 'Back',
        cancel: 'Cancel',
        close: 'Close',
        confirm: 'Confirm',
        details: 'Details',
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
          iot: 'Things'        
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
          owner_phone: 'Owner Phone'
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
            confirm_remove: 'Please confirm you\'d like to permanently remove the device "{device}" from the network.'
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
      login: {
        login: 'Login',
        username: 'Username',
        password: 'Password',
        sign_in: 'Sign In'
      }

    },
  },
  de: {
    message: {
      status: {
        brightgate_status: 'Brightgate Status',
        has_virus: '<b>Einer Ihrer Geräte hat einen Virus.</b><br/> Sehen Sie "{serious_alerts}".',
        network_properly: 'Ihr Netzwerk funktioniert ordnungsgemäß.'
      },
      testing: {
        testing: "Für Testzwecke",
        enable_mock: "Testmodus"
      },
      alerts: {
        serious_alerts: 'Schwerwiegende Warnungen',
        wannacry: '🚫&nbsp;&nbsp;WannaCry auf {device} entdeckt',
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
        open_setup_network: 'Netzwerk Konfiguration',
        accept_devices: 'Akzeptiere Geräte',
        success: 'Akzeptanz war erfolgreich. {devicesChanged} Geräte wurden akzeptiert.',
        fail: "Fehlermeldung: <b>{devicesAcceptedError}.</b>"
      },
      notifications: {
        notifications: 'Benachrichtigungen',
        update_device: '⚠️&nbsp;&nbsp; {device} aktualisieren',
        security_notifications: '⚠️ Sicherheitshinweis',
        msg: {
          '0': 'Dieses Gerät ist unsicher wegen veralteter Software.',
          '1': "Brightgate kann dieses Gerät nicht automatisch aktualisieren.",
          '2': "Folgen Sie den Hinweisen des Herstellers um die Software zu aktualisieren.",
        },
      },
      general: {
        accept: 'Akzeptieren',
        back: 'Zurück',
        cancel: 'Abbrechen',
        close: 'Schließen',
        confirm: 'Fortsetzen',
        details: 'Details',
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
          iot: 'Unidentifiziertes Verbindungsobjekt'        
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
            confirm_remove: 'Möchten Sie das Gerät "{device}" permanent vom Netzwerk entfernen?'
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
          additional_information: 'Weitere Informationen'
        },
      },
      login: {
        login: 'Login',
        username: 'Name',
        password: 'Passwort',
        sign_in: 'Einloggen'
      }
    }
  }
}

// Create VueI18n instance with options
export const i18n = new VueI18n({
  locale: BrowserLocale().substring(0,2), // read locale from browser without differentiating between en-Us and en
  fallbackLocale: 'en', // set fallback locale
  messages, // set locale messages
})
