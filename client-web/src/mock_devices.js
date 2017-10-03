
module.exports.mockDevices = {
  count: 9,
  alerts: 1,
  notifications: 2,

  devices: {
    categories: {
      recent: {
        name: 'Recent Attempted Connections',
        network_names: ['nosy-neighbor'],
      },
      phone: {
        name: 'Phones & Tablets',
        network_names: ['CAT', 'sch', 'catpad'],
      },
      computer: {
        name: 'Computers',
        network_names: ['catbook', 'schbook', 'jsmith'],
      },
      media: {
        name: 'Media',
        network_names: ['SONOS', 'cat-apple-TV', 'samsung-un50'],
      },
      iot: {
        name: 'Things',
        network_names: ['logicircle-1', 'logicircle-2', 'device']
      }
    },

    by_netname: {
      'nosy-neighbor': {
        category: 'recent',
        device: 'Apple iPhone 8',
        network_name: 'nosy-neighbor',
        os_version: 'iOS 11.0.1',
        owner: 'unknown',
        activated: '',
        owner_phone: '',
        owner_email: '',
        media: 'mobile-phone-1'
      },
      'CAT': {
        category: 'phone',
        device: 'Apple iPhone 6 Plus',
        network_name: 'CAT',
        os_version: 'iOS 10.3.3',
        owner: 'Christopher Thorpe',
        activated: 'August 10, 2017',
        owner_phone: '+1-617-259-4751',
        owner_email: 'cat@brightgate.com',
        media: 'mobile-phone-1'
      },
      'sch': {
        category: 'phone',
        device: 'Samsung Galaxy S8',
        network_name: 'sch',
        os_version: 'Android',
        owner: 'Stephen Hahn',
        activated: 'August 10, 2017',
        owner_phone: '+1-617-259-4751',
        owner_email: 'stephen@brightgate.com',
        media: 'mobile-phone-1'
      },
      'catpad': {
        category: 'phone',
        device: 'Apple iPad 2',
        network_name: 'catpad',
        os_version: 'iOS 9.1.1',
        owner: 'Christopher Thorpe',
        activated: 'August 10, 2017',
        owner_phone: '+1-617-259-4751',
        owner_email: 'cat@brightgate.com',
        notification: 'notification',
        media: 'tablet'
      },
      'catbook': {
        category: 'computer',
        device: 'Apple Macbook Pro',
        network_name: 'catbook',
        os_version: 'MacOS 10.12.4',
        owner: 'Christopher Thorpe',
        activated: 'August 10, 2017',
        owner_phone: '+1-617-259-4751',
        owner_email: 'cat@brightgate.com',
        media: 'laptop-2'
      },
      'schbook': {
        category: 'computer',
        device: 'Unknown Linux PC',
        network_name: 'schbook',
        os_version: 'Linux',
        owner: 'Stephen Hahn',
        activated: 'August 10, 2017',
        owner_phone: '+1-617-259-4751',
        owner_email: 'stephen@brightgate.com',
        media: 'laptop-1'
      },
      'jsmith': {
        category: 'computer',
        device: 'Toshiba Notebook PC',
        network_name: 'jsmith',
        os_version: 'Windows 10',
        owner: 'Jack Smith',
        activated: 'August 10, 2017',
        owner_phone: '+1-617-867-5309',
        owner_email: 'jack@brightgate.com',
        show_alert: 'alert',
        media: 'laptop-1'
      },
      'SONOS': {
        category: 'media',
        device: 'SONOS Audio System',
        network_name: 'SONOS',
        os_version: 'SONOS',
        owner: 'Christopher Thorpe',
        activated: 'August 10, 2017',
        owner_phone: '+1-617-259-4751',
        owner_email: 'cat@brightgate.com',
        media: 'radio-3'
      },
      'cat-apple-TV': {
        category: 'media',
        device: 'Apple TV',
        network_name: 'cat-apple-TV',
        os_version: 'Apple TV',
        owner: 'Stephen Hahn',
        activated: 'August 10, 2017',
        owner_phone: '+1-617-259-4751',
        owner_email: 'stephen@brightgate.com',
        media: 'television'
      },
      'samsung-un50': {
        category: 'media',
        device: 'Samsung UN50MU6300 Series 6',
        network_name: 'samsung-un50',
        os_version: '3.0.1',
        owner: 'Christopher Thorpe',
        activated: 'August 10, 2017',
        owner_phone: '+1-617-259-4751',
        owner_email: 'cat@brightgate.com',
        notification: 'notification',
        media: 'television'
      },
      'logicircle-1': {
        category: 'iot',
        device: 'Logic Circle Security Camera 1',
        network_name: 'logicircle-1',
        os_version: '4.4.4.448',
        owner: 'Christopher Thorpe',
        activated: 'August 10, 2017',
        owner_phone: '+1-617-259-4751',
        owner_email: 'cat@brightgate.com',
        media: 'webcam-1'
      },
      'logicircle-2': {
        category: 'iot',
        device: 'Logic Circle Security Camera 2',
        network_name: 'logicircle-2',
        os_version: '4.4.4.448',
        owner: 'Christopher Thorpe',
        activated: 'August 10, 2017',
        owner_phone: '+1-617-259-4751',
        owner_email: 'cat@brightgate.com',
        media: 'webcam-1'
      },
      'device': {
        category: 'iot',
        device: 'Unknown Device',
        network_name: 'device',
        os_version: 'unknown',
        owner: 'Christopher Thorpe',
        activated: 'August 10, 2017',
        owner_phone: '+1-617-259-4751',
        owner_email: 'cat@brightgate.com',
        media: 'interface-question-mark'
      },
    } //by_netname
  } //devices
}; //mockDevices


