<template>
  <div class="artesian-layout">
    <!-- Sidebar Navigation -->
    <AppSidebar 
      :tabs="NAV_TABS" 
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
        :active-downloads="activeDownloads" 
        :total-files-logged="totalFilesLogged"
        @clear-traffic="clearTraffic"
      />

      <LibraryView 
        v-if="currentTab === 'library'" 
        :library="library" 
        :cached-games="cachedGames"
        :active-prefill-job="activePrefillJob" 
        @open-add-game="showAddGameModal = true"
        @trigger-prefill="triggerPrefill"
        @add-to-library="handleAddCachedGame"
        @remove-from-library="handleRemoveFromLibrary"
        @refresh-cached="fetchCachedGames"
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

// Constants & API Layer
import { NAV_TABS, DEFAULT_SERVICES } from './constants';
import { api } from './services/api';

// Composables
import { useWebSocket } from './composables/useWebSocket';
import { useDownloads } from './composables/useDownloads';

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

// Navigation State
const currentTab = ref('dashboard');
const currentTabTitle = computed(() => {
  const t = NAV_TABS.find(item => item.id === currentTab.value);
  return t ? t.label : 'Artesian';
});

// Active Downloads & Traffic Composable
const {
  activeDownloads,
  totalFilesLogged,
  updateOrInsertSession,
  bundleIncomingChunk,
  handleDownloadRemoved,
  handleDownloadsCleared,
  clearTraffic,
} = useDownloads();

// Stats State
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

// Services State
const servicesList = ref(DEFAULT_SERVICES);
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
const cachedGames = ref([]);
const activePrefillJob = ref(null);
const showAddGameModal = ref(false);

// Automation State
const schedulerRules = ref([]);
const picsEvents = ref([]);

// Config State
const dnsInput = ref('1.1.1.1:53, 8.8.8.8:53');
const storagePathInput = ref('/data/cache');

// Real-Time WebSocket Event Stream
const { wsConnected } = useWebSocket({
  'proxy:traffic': (data) => {
    totalFilesLogged.value++;
    stats.value.total_requests++;
    if (data.is_hit) {
      stats.value.cache_hits++;
      stats.value.bytes_saved += (data.bytes_transferred || 0);
    } else {
      stats.value.cache_misses++;
    }
    stats.value.bytes_served += (data.bytes_transferred || 0);
    if (stats.value.total_requests > 0) {
      stats.value.hit_ratio = (stats.value.cache_hits / stats.value.total_requests) * 100;
    }

    if (data.session) {
      updateOrInsertSession(data.session);
    } else {
      bundleIncomingChunk(data);
    }
  },
  'proxy:download_update': (session) => {
    updateOrInsertSession(session);
  },
  'proxy:download_removed': (data) => {
    handleDownloadRemoved(data?.key);
  },
  'proxy:downloads_cleared': () => {
    handleDownloadsCleared();
  },
  'steam:status': (data) => {
    steamStatus.value = data;
    const stm = servicesList.value.find(s => s.id === 'steam');
    if (stm) {
      stm.status = data.state;
      stm.username = data.username;
    }
    if (data.state === 'connected') {
      showLoginModal.value = false;
      show2FAModal.value = false;
    }
  },
  'steam:2fa_required': () => {
    showLoginModal.value = false;
    show2FAModal.value = true;
    const stm = servicesList.value.find(s => s.id === 'steam');
    if (stm) stm.status = 'awaiting_2fa';
  },
  'prefill:progress': (data) => {
    activePrefillJob.value = data;
    if (data.status === 'completed') {
      fetchLibrary();
      setTimeout(() => {
        if (activePrefillJob.value && activePrefillJob.value.id === data.id) {
          activePrefillJob.value = null;
        }
      }, 5000);
    }
  },
  'pics:update_detected': (data) => {
    picsEvents.value.unshift({
      time: new Date().toLocaleTimeString(),
      title: `Update Detected: ${data.name}`,
      message: data.message,
    });
  },
  'pics:poll': (data) => {
    picsEvents.value.unshift({
      time: new Date().toLocaleTimeString(),
      title: `PICS Check: ${data.name}`,
      message: `Change #${data.change_number} verified against manifest catalog.`,
    });
  },
});

// API Calls
async function fetchServices() {
  try {
    const data = await api.getServices();
    if (data.services && data.services.length > 0) {
      servicesList.value = data.services;
    }
  } catch (e) {}
}

async function fetchStats() {
  try {
    const data = await api.getStats();
    stats.value = data.stats;
  } catch (e) {}
}

async function fetchSteamStatus() {
  try {
    steamStatus.value = await api.getSteamStatus();
    const stm = servicesList.value.find(s => s.id === 'steam');
    if (stm) {
      stm.status = steamStatus.value.state;
      stm.username = steamStatus.value.username;
    }
  } catch (e) {}
}

async function fetchLibrary() {
  try {
    library.value = await api.getLibrary();
  } catch (e) {}
}

async function fetchCachedGames() {
  try {
    cachedGames.value = await api.getCachedGames();
  } catch (e) {}
}

async function handleAddCachedGame(game) {
  try {
    await api.addGame({
      appId: game.app_id,
      name: game.name,
      service: game.service || 'steam',
      autoUpdate: true,
    });
    await fetchLibrary();
    await fetchCachedGames();
  } catch (e) {
    console.error('Failed to add cached game to library:', e);
  }
}

async function handleRemoveFromLibrary(game) {
  try {
    await api.removeGame(game.app_id, game.service || 'steam');
    await fetchLibrary();
    await fetchCachedGames();
  } catch (e) {
    console.error('Failed to remove game from library:', e);
  }
}

async function fetchScheduler() {
  try {
    const data = await api.getSchedulerRules();
    schedulerRules.value = data.rules;
  } catch (e) {}
}

async function fetchConfig() {
  try {
    const dnsData = await api.getDNSConfig();
    if (dnsData.upstream_dns) {
      dnsInput.value = dnsData.upstream_dns.join(', ');
    }
    const stData = await api.getStorageConfig();
    if (stData.storage_path) {
      storagePathInput.value = stData.storage_path;
    }
  } catch (e) {}
}

async function handleSteamLogin({ username, password }) {
  loginUsername.value = username;
  loginPassword.value = password;
  loggingIn.value = true;
  try {
    const data = await api.steamLogin(username, password);
    steamStatus.value = data.status;
    const stm = servicesList.value.find(s => s.id === 'steam');
    if (stm) {
      stm.status = data.status.state;
      stm.username = data.status.username;
    }
  } finally {
    loggingIn.value = false;
  }
}

async function submit2FACode(code) {
  try {
    await api.submitSteam2FA(code);
    show2FAModal.value = false;
    fetchSteamStatus();
  } catch (e) {}
}

async function logoutSteam() {
  await api.steamLogout();
  fetchSteamStatus();
}

async function triggerPrefill(game) {
  try {
    activePrefillJob.value = await api.startPrefill({
      appId: game.app_id,
      name: game.name,
      service: 'steam',
    });
  } catch (e) {}
}

async function toggleServiceMonitoring(svc) {
  try {
    const data = await api.toggleService(svc.id, svc.intercept_enabled);
    svc.intercept_enabled = data.enabled;
  } catch (err) {
    console.error('Failed to toggle service monitoring:', err);
  }
}

async function pollPICSNow() {
  await api.pollPICS();
}

async function triggerSimulatedPICS() {
  await api.triggerSimulatedPICS(730);
}

async function saveDNSConfig() {
  const parts = dnsInput.value.split(',').map(s => s.trim()).filter(Boolean);
  await api.saveDNSConfig(parts);
  alert('Upstream DNS configuration saved.');
}

async function saveStorageConfig() {
  await api.saveStorageConfig(storagePathInput.value);
  alert('Storage tank path saved.');
}

async function saveNewGame({ appId, name }) {
  await api.addGame({ appId, name, autoUpdate: true });
  showAddGameModal.value = false;
  fetchLibrary();
  fetchCachedGames();
}

onMounted(() => {
  fetchServices();
  fetchStats();
  fetchSteamStatus();
  fetchLibrary();
  fetchCachedGames();
  fetchScheduler();
  fetchConfig();
  setInterval(fetchStats, 3000);
});
</script>
