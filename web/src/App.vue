<template>
  <div class="artesian-layout">
    <!-- Sidebar Navigation -->
    <AppSidebar 
      :tabs="tabs" 
      :current-tab="currentTab" 
      :ws-connected="wsConnected"
      @update:current-tab="currentTab = $event"
    />

    <!-- Main Content Area -->
    <main class="main-content">
      <AppHeader :title="currentTabTitle" />

      <!-- Tab Views -->
      <StatsView 
        v-if="currentTab === 'dashboard'" 
        :stats="stats" 
        :live-traffic="liveTraffic" 
        @clear-traffic="liveTraffic = []"
      />

      <LibraryView 
        v-if="currentTab === 'library'" 
        :library="library" 
        :active-prefill-job="activePrefillJob" 
        @open-add-game="showAddGameModal = true"
        @trigger-prefill="triggerPrefill"
      />

      <UpdatesView 
        v-if="currentTab === 'automation'" 
        :pics-events="picsEvents" 
        :scheduler-rules="schedulerRules" 
        @poll-pics="pollPICSNow"
        @trigger-simulated-pics="triggerSimulatedPICS"
      />

      <SettingsView 
        v-if="currentTab === 'config'" 
        v-model:dns-input="dnsInput" 
        v-model:storage-path-input="storagePathInput" 
        @save-dns="saveDNSConfig"
        @save-storage="saveStorageConfig"
      />

      <AccountsView 
        v-if="currentTab === 'services'" 
        :services-list="servicesList" 
        @toggle-monitoring="toggleServiceMonitoring"
        @open-steam-login="showLoginModal = true"
        @logout-steam="logoutSteam"
        @switch-tab="currentTab = $event"
        @open-details="selectedServiceInfo = $event"
      />
    </main>

    <!-- Modal Dialogs -->
    <SteamLoginModal 
      v-model="showLoginModal" 
      :loading="loggingIn" 
      @submit="handleSteamLogin"
    />

    <Steam2FAModal 
      v-model="show2FAModal" 
      :username="steamStatus.username || loginUsername" 
      @submit="submit2FACode"
      @dismiss="show2FAModal = false"
    />

    <AddGameModal 
      v-model="showAddGameModal" 
      @submit="saveNewGame"
    />

    <ServiceDetailsModal 
      :service="selectedServiceInfo" 
      @close="selectedServiceInfo = null"
    />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue';

// Layout Components
import AppSidebar from './components/layout/AppSidebar.vue';
import AppHeader from './components/layout/AppHeader.vue';

// View Components
import StatsView from './views/StatsView.vue';
import LibraryView from './views/LibraryView.vue';
import UpdatesView from './views/UpdatesView.vue';
import SettingsView from './views/SettingsView.vue';
import AccountsView from './views/AccountsView.vue';

// Modal Components
import SteamLoginModal from './components/modals/SteamLoginModal.vue';
import Steam2FAModal from './components/modals/Steam2FAModal.vue';
import AddGameModal from './components/modals/AddGameModal.vue';
import ServiceDetailsModal from './components/modals/ServiceDetailsModal.vue';

// Navigation Configuration
const tabs = [
  { id: 'dashboard', label: 'Stats', icon: 'fa-regular fa-chart-bar' },
  { id: 'library', label: 'Library', icon: 'fa-regular fa-folder-open' },
  { id: 'automation', label: 'Updates', icon: 'fa-regular fa-clock' },
  { id: 'config', label: 'Settings', icon: 'fa-solid fa-gear' },
  { id: 'services', label: 'Accounts', icon: 'fa-regular fa-circle-user' },
];

const currentTab = ref('dashboard');

const currentTabTitle = computed(() => {
  const t = tabs.find(item => item.id === currentTab.value);
  return t ? t.label : 'Artesian';
});

// App State
const wsConnected = ref(false);
const liveTraffic = ref([]);
const stats = ref({
  total_requests: 0,
  cache_hits: 0,
  cache_misses: 0,
  hit_ratio: 0,
  bytes_served: 0,
  bytes_saved: 0,
  read_bytes_sec: 0,
  write_bytes_sec: 0,
});

const servicesList = ref([
  {
    id: 'steam',
    display_name: 'Steam',
    company: 'Valve',
    status: 'disconnected',
    intercept_enabled: true,
    auth_supported: true,
    prefill_supported: true,
    username: '',
    domains: ['*.cs.steampowered.com', '*.content.steampowered.com', '*.steamcontent.com'],
    description: 'Depot manifest decryption, PICS change polling, and high-throughput chunk caching.',
  },
  {
    id: 'epic',
    display_name: 'Epic Games Store',
    company: 'Epic Games',
    status: 'ready',
    intercept_enabled: true,
    auth_supported: false,
    prefill_supported: false,
    domains: ['*.download.epicgames.com', '*.ol.epicgames.com'],
    description: 'Chunked binary manifest (.manifest) and hash-addressed .chunk pass-through caching.',
  },
  {
    id: 'blizzard',
    display_name: 'Battle.net',
    company: 'Blizzard Entertainment',
    status: 'ready',
    intercept_enabled: true,
    auth_supported: false,
    prefill_supported: false,
    domains: ['*.dist.blizzard.com', '*.cdn.blizzard.com'],
    description: 'Battle.net TACT archive format and CDN edge server interception.',
  },
  {
    id: 'riot',
    display_name: 'Riot Games',
    company: 'Riot Games',
    status: 'ready',
    intercept_enabled: true,
    auth_supported: false,
    prefill_supported: false,
    domains: ['*.dyn.riotcdn.net', '*.content.riotgames.com'],
    description: 'Valorant & League of Legends RMAN release manifests and client asset caching.',
  },
  {
    id: 'xbox',
    display_name: 'Xbox / Windows Store',
    company: 'Microsoft',
    status: 'ready',
    intercept_enabled: true,
    auth_supported: false,
    prefill_supported: false,
    domains: ['*.assets1.xboxlive.com', '*.delivery.mp.microsoft.com'],
    description: 'Delivery Optimization range-request caching for Xbox game downloads.',
  },
]);

const selectedServiceInfo = ref(null);

// Steam Auth State
const steamStatus = ref({
  state: 'disconnected',
  username: '',
  has_valid_auth: false,
});
const showLoginModal = ref(false);
const show2FAModal = ref(false);
const loginUsername = ref('');
const loginPassword = ref('');
const loggingIn = ref(false);

// Library & Prefill State
const library = ref([]);
const activePrefillJob = ref(null);
const showAddGameModal = ref(false);

// Automation State
const schedulerRules = ref([]);
const picsEvents = ref([]);

// Config State
const dnsInput = ref('1.1.1.1:53, 8.8.8.8:53');
const storagePathInput = ref('/data/cache');

// WebSocket Setup
function initWebSocket() {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${protocol}//${window.location.host}/api/v1/ws`;
  const socket = new WebSocket(wsUrl);

  socket.onopen = () => {
    wsConnected.value = true;
  };

  socket.onclose = () => {
    wsConnected.value = false;
    setTimeout(initWebSocket, 2000);
  };

  socket.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data);
      handleWSEvent(msg);
    } catch (e) {
      console.error(e);
    }
  };
}

function handleWSEvent(msg) {
  const nowStr = new Date().toLocaleTimeString();

  if (msg.type === 'proxy:traffic') {
    liveTraffic.value.unshift({
      time: nowStr,
      ...msg.data,
    });
    if (liveTraffic.value.length > 50) {
      liveTraffic.value.pop();
    }
    stats.value.total_requests++;
    if (msg.data.is_hit) {
      stats.value.cache_hits++;
      stats.value.bytes_saved += (msg.data.bytes_transferred || 0);
    } else {
      stats.value.cache_misses++;
    }
    stats.value.bytes_served += (msg.data.bytes_transferred || 0);
    if (stats.value.total_requests > 0) {
      stats.value.hit_ratio = (stats.value.cache_hits / stats.value.total_requests) * 100;
    }
  } 
  else if (msg.type === 'steam:status') {
    steamStatus.value = msg.data;
    const stm = servicesList.value.find(s => s.id === 'steam');
    if (stm) {
      stm.status = msg.data.state;
      stm.username = msg.data.username;
    }
    if (msg.data.state === 'connected') {
      showLoginModal.value = false;
      show2FAModal.value = false;
    }
  } 
  else if (msg.type === 'steam:2fa_required') {
    showLoginModal.value = false;
    show2FAModal.value = true;
    const stm = servicesList.value.find(s => s.id === 'steam');
    if (stm) stm.status = 'awaiting_2fa';
  } 
  else if (msg.type === 'prefill:progress') {
    activePrefillJob.value = msg.data;
    if (msg.data.status === 'completed') {
      fetchLibrary();
      setTimeout(() => {
        if (activePrefillJob.value && activePrefillJob.value.id === msg.data.id) {
          activePrefillJob.value = null;
        }
      }, 5000);
    }
  }
  else if (msg.type === 'pics:update_detected') {
    picsEvents.value.unshift({
      time: nowStr,
      title: `Update Detected: ${msg.data.name}`,
      message: msg.data.message,
    });
  }
  else if (msg.type === 'pics:poll') {
    picsEvents.value.unshift({
      time: nowStr,
      title: `PICS Check: ${msg.data.name}`,
      message: `Change #${msg.data.change_number} verified against manifest catalog.`,
    });
  }
}

// API Calls
async function fetchServices() {
  try {
    const res = await fetch('/api/v1/services');
    if (res.ok) {
      const data = await res.json();
      if (data.services && data.services.length > 0) {
        servicesList.value = data.services;
      }
    }
  } catch (e) {}
}

async function fetchStats() {
  try {
    const res = await fetch('/api/v1/stats');
    if (res.ok) {
      const data = await res.json();
      stats.value = data.stats;
    }
  } catch (e) {}
}

async function fetchSteamStatus() {
  try {
    const res = await fetch('/api/v1/steam/status');
    if (res.ok) {
      steamStatus.value = await res.json();
      const stm = servicesList.value.find(s => s.id === 'steam');
      if (stm) {
        stm.status = steamStatus.value.state;
        stm.username = steamStatus.value.username;
      }
    }
  } catch (e) {}
}

async function fetchLibrary() {
  try {
    const res = await fetch('/api/v1/library');
    if (res.ok) {
      library.value = await res.json();
    }
  } catch (e) {}
}

async function fetchScheduler() {
  try {
    const res = await fetch('/api/v1/scheduler/rules');
    if (res.ok) {
      const data = await res.json();
      schedulerRules.value = data.rules;
    }
  } catch (e) {}
}

async function fetchConfig() {
  try {
    const dnsRes = await fetch('/api/v1/config/dns');
    if (dnsRes.ok) {
      const data = await dnsRes.json();
      if (data.upstream_dns) {
        dnsInput.value = data.upstream_dns.join(', ');
      }
    }
    const stRes = await fetch('/api/v1/config/storage');
    if (stRes.ok) {
      const data = await stRes.json();
      if (data.storage_path) {
        storagePathInput.value = data.storage_path;
      }
    }
  } catch (e) {}
}

async function handleSteamLogin({ username, password }) {
  loginUsername.value = username;
  loginPassword.value = password;
  loggingIn.value = true;
  try {
    const res = await fetch('/api/v1/steam/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });
    if (res.ok) {
      const data = await res.json();
      steamStatus.value = data.status;
      const stm = servicesList.value.find(s => s.id === 'steam');
      if (stm) {
        stm.status = data.status.state;
        stm.username = data.status.username;
      }
    }
  } finally {
    loggingIn.value = false;
  }
}

async function submit2FACode(code) {
  try {
    const res = await fetch('/api/v1/steam/2fa', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code }),
    });
    if (res.ok) {
      show2FAModal.value = false;
      fetchSteamStatus();
    }
  } catch (e) {}
}

async function logoutSteam() {
  await fetch('/api/v1/steam/logout', { method: 'POST' });
  fetchSteamStatus();
}

async function triggerPrefill(game) {
  try {
    const res = await fetch('/api/v1/prefill/start', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        app_id: game.app_id,
        app_name: game.name,
        service: 'steam',
      }),
    });
    if (res.ok) {
      activePrefillJob.value = await res.json();
    }
  } catch (e) {}
}

async function toggleServiceMonitoring(svc) {
  try {
    const res = await fetch('/api/v1/services/toggle', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        id: svc.id,
        enabled: svc.intercept_enabled,
      }),
    });
    if (res.ok) {
      const data = await res.json();
      svc.intercept_enabled = data.enabled;
    }
  } catch (err) {
    console.error('Failed to toggle service monitoring:', err);
  }
}

async function pollPICSNow() {
  await fetch('/api/v1/scheduler/pics/poll', { method: 'POST' });
}

async function triggerSimulatedPICS() {
  await fetch('/api/v1/scheduler/pics/trigger?app_id=730', { method: 'POST' });
}

async function saveDNSConfig() {
  const parts = dnsInput.value.split(',').map(s => s.trim()).filter(Boolean);
  await fetch('/api/v1/config/dns', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ upstream_dns: parts }),
  });
  alert('Upstream DNS configuration saved.');
}

async function saveStorageConfig() {
  await fetch('/api/v1/config/storage', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ storage_path: storagePathInput.value }),
  });
  alert('Storage tank path saved.');
}

async function saveNewGame({ appId, name }) {
  await fetch('/api/v1/library', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      app_id: appId,
      name: name,
      auto_update: true,
    }),
  });
  showAddGameModal.value = false;
  fetchLibrary();
}

onMounted(() => {
  initWebSocket();
  fetchServices();
  fetchStats();
  fetchSteamStatus();
  fetchLibrary();
  fetchScheduler();
  fetchConfig();
  setInterval(fetchStats, 3000);
});
</script>
