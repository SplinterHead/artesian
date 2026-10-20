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
    auth_supported: true,
    prefill_supported: true,
    username: '',
    domains: [
      '*.cs.steampowered.com',
      '*.content.steampowered.com',
      '*.hsar.steampowered.com',
      'content*.steampowered.com',
      '*.steamcontent.com',
    ],
    description: 'Depot manifest decryption, PICS change polling, and high-throughput chunk caching.',
  },
];
