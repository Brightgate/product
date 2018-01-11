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
        network_properly: 'Your network is working properly.'
      },
      alerts: {
        serious_alerts: 'Serious Alerts',
        wannacry: '🚫&nbsp;&nbsp;WannaCry on {device}'
      },
      tools: {
        tools: 'Tools',
        manage_devices: 'Manage Devices ({device_count})',
        open_setup_network: 'Open Setup Network',
        accept_devices: 'Accept Devices'
      },
      notifications: {
        notifications: 'Notifications',
        update_device: '⚠️&nbsp;&nbsp;Update device {device}'
      },
      general: {
        accept: 'Accept'
      }
    }
  },
  de: {
    message: {
      status: {
        brightgate_status: 'Brightgate Status',
        has_virus: '<b>Einer Ihrer Geräte hat einen Virus.</b><br/> Sehen Sie "{serious_alerts}".',
        network_properly: 'Ihr Netzwerk funktioniert standarmäßig.'
      },
      alerts: {
        serious_alerts: 'Wichtige Warnungen',
        wannacry: '🚫&nbsp;&nbsp;WannaCry auf {device} entdeckt'
      },
      tools: {
        tools: 'Werkzeuge',
        manage_devices: 'Geräte verwalten ({device_count})',
        open_setup_network: 'Netzwerk Konfiguration',
        accept_devices: 'Akzeptiere Geräte'
      },
      notifications: {
        notifications: 'Benachrichtigungen',
        update_device: '⚠️&nbsp;&nbsp; {device} aktualisieren'
      },
      general: {
        accept: 'Akzeptieren'
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
