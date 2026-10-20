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
        :deleting-key="deletingGameKey"
        :rechecking-key="recheckingGameKey"
        @open-add-game="openAddGameModal"
        @trigger-prefill="triggerPrefill"
        @recheck-game="handleRecheckGame"
        @add-to-library="handleAddCachedGame"
        @remove-from-library="confirmRemoveFromLibrary"
        @delete-cached-game="confirmDeleteCachedGame"
        @refresh-cached="fetchCachedGames"
        @toggle-game-autoupdate="handleToggleGameAutoUpdate"
      />

      <UpdatesView 
        v-if="currentTab === 'automation'" 
        :update-logs="updateLogs" 
        :scheduler-rules="schedulerRules" 
        @check-updates="checkUpdatesNow"
        @save-rule="handleSaveSchedulerRule"
      />

      <SettingsView 
        v-if="currentTab === 'config'" 
        v-model:dns-input="dnsInput" 
        v-model:intercept-ip-input="dnsInterceptIpInput"
        v-model:storage-path-input="storagePathInput" 
        v-model:max-storage-gb-input="maxStorageGbInput"
        @save-dns="saveDNSConfig"
        @save-storage="saveStorageConfig"
      />

      <AccountsView 
        v-if="currentTab === 'services'" 
        :services-list="servicesList" 
        @toggle-monitoring="toggleServiceMonitoring"
        @toggle-autoupdate="handleToggleServiceAutoUpdate"
        @open-steam-login="handleOpenSteamLogin"
        @open-steam-2fa="openSteam2FAModal"
        @logout-steam="logoutSteam"
        @switch-tab="currentTab = $event"
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
      :loading="verifying2FA"
      :has-mobile-confirmation="steamStatus.has_mobile_confirmation ?? true"
      :has-email-code="steamStatus.has_email_code ?? false"
      @submit="submit2FACode"
      @dismiss="show2FAModal = false"
      @switch-account="handleRestartSteamLogin"
    />

    <AddGameModal 
      v-model="showAddGameModal" 
      :services-list="servicesList"
      :steam-status="steamStatus"
      :existing-library="library"
      @submit="saveNewGame"
      @switch-tab="currentTab = $event"
    />

    <ConfirmDeleteModal 
      v-model="showDeleteConfirmModal" 
      :game="gameToDelete" 
      :loading="deletingInProgress"
      @confirm="executeDeleteGame"
    />
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue';

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
import ConfirmDeleteModal from './components/modals/ConfirmDeleteModal.vue';

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
  fetchTraffic,
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
const verifying2FA = ref(false);

watch(showLoginModal, (isOpen) => {
  if (!isOpen) {
    loggingIn.value = false;
  }
});

watch(show2FAModal, (isOpen) => {
  if (!isOpen) {
    verifying2FA.value = false;
  }
});

// Library & Prefill State
const library = ref([]);
const cachedGames = ref([]);
const activePrefillJob = ref(null);
const showAddGameModal = ref(false);

// Delete Game Confirmation State
const showDeleteConfirmModal = ref(false);
const gameToDelete = ref(null);
const isDeletingCachedGame = ref(false);
const deletingGameKey = ref(null);
const deletingInProgress = ref(false);
const recheckingGameKey = ref(null);

// Automation State
const schedulerRules = ref([]);
const updateLogs = ref([]);

// Config State
const dnsInput = ref('1.1.1.1:53, 8.8.8.8:53');
const dnsInterceptIpInput = ref('127.0.0.1');
const storagePathInput = ref('/data/cache');
const maxStorageGbInput = ref(2048);

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
  'library:updated': () => {
    fetchLibrary();
    fetchCachedGames();
  },
  'steam:status': (data) => {
    steamStatus.value = data;
    const stm = servicesList.value.find(s => s.id === 'steam');
    if (stm) {
      stm.status = data.state;
      stm.username = data.username;
    }
    if (data.state === 'connected') {
      loggingIn.value = false;
      showLoginModal.value = false;
      show2FAModal.value = false;
      fetchLibrary();
      fetchServices();
    } else if (data.state === 'awaiting_2fa') {
      loggingIn.value = false;
      showLoginModal.value = false;
      show2FAModal.value = true;
    } else if (data.state === 'error') {
      loggingIn.value = false;
      show2FAModal.value = false;
      if (data.error_message) {
        alert(data.error_message);
      }
    } else if (data.state !== 'connecting') {
      loggingIn.value = false;
    }
  },
  'steam:2fa_required': (data) => {
    loggingIn.value = false;
    showLoginModal.value = false;
    show2FAModal.value = true;
    if (data) {
      if (!steamStatus.value) steamStatus.value = {};
      if (data.username) steamStatus.value.username = data.username;
      if (data.has_mobile_confirmation !== undefined) steamStatus.value.has_mobile_confirmation = data.has_mobile_confirmation;
      if (data.has_email_code !== undefined) steamStatus.value.has_email_code = data.has_email_code;
    }
    const stm = servicesList.value.find(s => s.id === 'steam');
    if (stm) stm.status = 'awaiting_2fa';
  },
  'prefill:progress': (data) => {
    activePrefillJob.value = data;
    const g = library.value.find(item => item.app_id === data.app_id);
    if (g) {
      if (data.bytes_cached != null) {
        g.size_bytes = data.bytes_cached;
      }
      if (data.status) {
        if (data.status === 'completed') {
          g.status = 'synced';
        } else if (data.status === 'downloading' || data.status === 'diffing') {
          g.status = 'prefilling';
        }
      }
    }
    if (data.status === 'completed') {
      fetchLibrary();
      setTimeout(() => {
        if (activePrefillJob.value && activePrefillJob.value.id === data.id) {
          activePrefillJob.value = null;
        }
      }, 5000);
    }
  },
  'update:discovered': (entry) => {
    updateLogs.value.unshift(entry);
    if (updateLogs.value.length > 100) updateLogs.value.pop();
  },
  'scheduler:rules_updated': () => {
    fetchScheduler();
    fetchUpdateLogs();
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
    const svcObj = servicesList.value.find(s => s.id === (game.service || 'steam'));
    const defaultAutoUpdate = svcObj?.auto_update ?? true;
    await api.addGame({
      appId: game.app_id,
      name: game.name,
      service: game.service || 'steam',
      platform: game.platform || '',
      autoUpdate: defaultAutoUpdate,
    });
    await fetchLibrary();
    await fetchCachedGames();
  } catch (e) {
    console.error('Failed to add cached game to library:', e);
  }
}

function confirmRemoveFromLibrary(game) {
  gameToDelete.value = game;
  isDeletingCachedGame.value = false;
  showDeleteConfirmModal.value = true;
}

function confirmDeleteCachedGame(item) {
  gameToDelete.value = item;
  isDeletingCachedGame.value = true;
  showDeleteConfirmModal.value = true;
}

async function executeDeleteGame() {
  if (!gameToDelete.value || deletingInProgress.value) return;
  const game = gameToDelete.value;
  const key = `${game.service || 'steam'}:${game.app_id || 0}:${game.name || ''}`;
  deletingGameKey.value = key;
  deletingInProgress.value = true;

  try {
    if (isDeletingCachedGame.value) {
      await api.deleteCachedGame(game.app_id, game.service || 'steam');
    } else {
      await api.removeGame(game.app_id, game.service || 'steam');
    }
    showDeleteConfirmModal.value = false;
    await fetchLibrary();
    await fetchCachedGames();
    await fetchTraffic();
    await fetchStats();
  } catch (e) {
    console.error('Failed to delete game:', e);
  } finally {
    deletingInProgress.value = false;
    deletingGameKey.value = null;
    gameToDelete.value = null;
  }
}

async function fetchScheduler() {
  try {
    const data = await api.getSchedulerRules();
    schedulerRules.value = data.rules;
  } catch (e) {}
}

async function fetchUpdateLogs() {
  try {
    const data = await api.getUpdateLogs();
    if (data.logs) {
      updateLogs.value = data.logs;
    }
  } catch (e) {}
}

async function fetchConfig() {
  try {
    const dnsData = await api.getDNSConfig();
    if (dnsData.upstream_dns) {
      dnsInput.value = dnsData.upstream_dns.join(', ');
    }
    if (dnsData.intercept_ipv4) {
      dnsInterceptIpInput.value = dnsData.intercept_ipv4;
    }
    const stData = await api.getStorageConfig();
    if (stData.storage_path) {
      storagePathInput.value = stData.storage_path;
    }
    if (stData.max_size_bytes) {
      maxStorageGbInput.value = Math.round(stData.max_size_bytes / (1024 * 1024 * 1024));
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

    if (data.status.state === 'awaiting_2fa') {
      loggingIn.value = false;
      showLoginModal.value = false;
      show2FAModal.value = true;
      return;
    }
    if (data.status.state === 'connected') {
      loggingIn.value = false;
      showLoginModal.value = false;
      return;
    }

    // Still connecting in the background: keep loading animation active and poll for resolution
    const start = Date.now();
    const pollInterval = setInterval(async () => {
      if (!showLoginModal.value || !loggingIn.value) {
        clearInterval(pollInterval);
        return;
      }
      try {
        const s = await api.getSteamStatus();
        steamStatus.value = s;
        if (stm) {
          stm.status = s.state;
          stm.username = s.username;
        }
        if (s.state === 'awaiting_2fa') {
          clearInterval(pollInterval);
          loggingIn.value = false;
          showLoginModal.value = false;
          show2FAModal.value = true;
        } else if (s.state === 'connected') {
          clearInterval(pollInterval);
          loggingIn.value = false;
          showLoginModal.value = false;
        } else if (s.state === 'error' || s.state === 'disconnected') {
          clearInterval(pollInterval);
          loggingIn.value = false;
          if (s.error_message) {
            alert(s.error_message);
          }
        } else if (Date.now() - start > 15000) {
          clearInterval(pollInterval);
          loggingIn.value = false;
        }
      } catch {
        clearInterval(pollInterval);
        loggingIn.value = false;
      }
    }, 400);
  } catch (err) {
    loggingIn.value = false;
    alert(err.message || 'Failed to authenticate with Steam');
  }
}

function handleOpenSteamLogin() {
  const isAwaiting2FA = steamStatus.value?.state === 'awaiting_2fa' || 
    servicesList.value.some(s => s.id === 'steam' && s.status === 'awaiting_2fa');

  if (isAwaiting2FA) {
    showLoginModal.value = false;
    show2FAModal.value = true;
  } else {
    showLoginModal.value = true;
  }
}

function openSteam2FAModal() {
  showLoginModal.value = false;
  show2FAModal.value = true;
}

function handleRestartSteamLogin() {
  show2FAModal.value = false;
  showLoginModal.value = true;
}

async function submit2FACode(code) {
  verifying2FA.value = true;
  try {
    await api.submitSteam2FA(code);
    show2FAModal.value = false;
    await fetchSteamStatus();
    await fetchServices();
    await fetchLibrary();
  } catch (err) {
    alert(err.message || 'Failed to verify Steam 2FA code. Please check the code and try again.');
  } finally {
    verifying2FA.value = false;
  }
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
      service: game.service || 'steam',
    });
  } catch (e) {}
}

async function handleRecheckGame(game) {
  if (!game) return;
  const key = `${game.service || 'steam'}:${game.app_id || 0}:${game.name || ''}`;
  recheckingGameKey.value = key;
  try {
    game.status = 'prefilling';
    activePrefillJob.value = await api.startPrefill({
      appId: game.app_id,
      name: game.name,
      service: game.service || 'steam',
    });
    await fetchLibrary();
  } catch (err) {
    console.error('Failed to trigger depot re-check:', err);
    alert(err.message || 'Failed to trigger depot re-check');
  } finally {
    setTimeout(() => {
      if (recheckingGameKey.value === key) {
        recheckingGameKey.value = null;
      }
    }, 1200);
  }
}

async function toggleServiceMonitoring(svc) {
  try {
    const data = await api.toggleService(svc.id, svc.intercept_enabled);
    svc.intercept_enabled = data.enabled;
  } catch (err) {
    console.error('Failed to toggle service monitoring:', err);
  }
}

async function checkUpdatesNow() {
  try {
    await api.checkUpdates();
    await fetchUpdateLogs();
  } catch (err) {
    console.error('Failed to trigger update check:', err);
  }
}

async function saveDNSConfig() {
  const parts = dnsInput.value.split(',').map(s => s.trim()).filter(Boolean);
  await api.saveDNSConfig(parts, dnsInterceptIpInput.value.trim());
  alert('Upstream DNS & Interception IP settings saved to database.');
}

async function saveStorageConfig() {
  const maxBytes = (parseInt(maxStorageGbInput.value, 10) || 2048) * 1024 * 1024 * 1024;
  await api.saveStorageConfig(storagePathInput.value.trim(), maxBytes);
  alert('Storage path & quota saved to database.');
}

async function handleSaveSchedulerRule({ id, cron_expression, enabled }) {
  try {
    await api.updateSchedulerRule(id, cron_expression, enabled);
    await fetchScheduler();
  } catch (err) {
    alert(err.message || 'Failed to update schedule');
  }
}

function openAddGameModal() {
  fetchServices();
  fetchSteamStatus();
  showAddGameModal.value = true;
}

async function saveNewGame({ appId, name, service = 'steam', platform = '', replacePlatforms = false, autoUpdate }) {
  const svcObj = servicesList.value.find(s => s.id === service);
  const effectiveAutoUpdate = autoUpdate !== undefined ? autoUpdate : (svcObj?.auto_update ?? true);
  await api.addGame({ appId, name, service, platform, autoUpdate: effectiveAutoUpdate, replacePlatforms });
  showAddGameModal.value = false;
  fetchLibrary();
  fetchCachedGames();
  fetchTraffic();
}

async function handleToggleServiceAutoUpdate(svc) {
  try {
    const res = await api.toggleServiceAutoUpdate(svc.id, svc.auto_update);
    svc.auto_update = res.auto_update;
  } catch (err) {
    console.error('Failed to toggle service auto-update:', err);
    svc.auto_update = !svc.auto_update;
  }
}

async function handleToggleGameAutoUpdate(game) {
  try {
    const newStatus = !game.auto_update;
    await api.updateGameAutoUpdate(game.app_id, game.service || 'steam', newStatus);
    game.auto_update = newStatus;
  } catch (err) {
    console.error('Failed to toggle game auto-update:', err);
  }
}

onMounted(() => {
  fetchServices();
  fetchStats();
  fetchSteamStatus();
  fetchLibrary();
  fetchCachedGames();
  fetchScheduler();
  fetchUpdateLogs();
  fetchConfig();
  setInterval(fetchStats, 3000);
});
</script>
