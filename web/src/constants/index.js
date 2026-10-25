// Navigation Tabs Configuration
export const NAV_TABS = [
  { id: 'dashboard', label: 'Stats', icon: 'fa-regular fa-chart-bar' },
  { id: 'library', label: 'Library', icon: 'fa-regular fa-folder-open' },
  { id: 'automation', label: 'Updates', icon: 'fa-regular fa-clock' },
  { id: 'config', label: 'Settings', icon: 'fa-solid fa-gear' },
  { id: 'services', label: 'Accounts', icon: 'fa-regular fa-circle-user' },
];

// Initial default services definition
export const DEFAULT_SERVICES = [
  {
    id: 'steam',
    display_name: 'Steam',
    company: 'Valve',
    status: 'disconnected',
    intercept_enabled: true,
    auto_update: true,
    username: '',
    domains: [
      '*.cs.steampowered.com',
      '*.content.steampowered.com',
      '*.hsar.steampowered.com',
      'content*.steampowered.com',
      '*.steamcontent.com',
    ],
    description:
      'Depot manifest decryption, PICS change polling, and high-throughput chunk caching.',
  },
  {
    id: 'xbox',
    display_name: 'Xbox / Microsoft Store',
    company: 'Microsoft',
    status: 'disconnected',
    intercept_enabled: true,
    auto_update: true,
    username: '',
    domains: [
      '*.assets1.xboxlive.com',
      '*.xvcf1.xboxlive.com',
      'assets1.xboxlive.com',
      'xvcf1.xboxlive.com',
      '*.dl.delivery.mp.microsoft.com',
      'dl.delivery.mp.microsoft.com',
      'tlu.dl.delivery.mp.microsoft.com',
      '*.delivery.mp.microsoft.com',
      '*.update.microsoft.com',
      '*.windowsupdate.com',
      '*.mp.microsoft.com',
    ],
    description:
      'OAuth2 Device Code authentication, Package Service & Delivery Optimization, 1MB byte-range chunk pre-fetching.',
  },
  {
    id: 'epic',
    display_name: 'Epic Games',
    company: 'Epic Games',
    status: 'disconnected',
    intercept_enabled: true,
    auto_update: true,
    username: '',
    domains: [
      'download*.epicgames.com',
      '*.download.epicgames.com',
      'cdn*.epicgames.com',
      '*.epicgamescdn.com',
      'epicgames-download*.akamaized.net',
      '*download*.fastly-edge.com',
      'fastly-download.epicgames.com',
      '*.unrealengine.com',
    ],
    description:
      'OAuth2 launcher authorization, binary manifest parsing, and high-throughput chunk caching.',
  },
];
