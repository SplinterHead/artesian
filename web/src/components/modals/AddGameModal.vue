<template>
  <BaseModal 
    :model-value="modelValue" 
    title="Add Game to Library" 
    @update:model-value="handleCancel"
  >
    <!-- Scenario A: No Connected Accounts -->
    <div v-if="authenticatedServices.length === 0" class="no-accounts-alert">
      <div class="alert-icon-box">
        <i class="fa-solid fa-triangle-exclamation"></i>
      </div>
      <div class="alert-content">
        <h4>No Accounts Connected</h4>
        <p class="alert-message">
          Please set up credentials with a Store so your available games can be seen
        </p>
        <p class="text-xs text-muted mt-2">
          Head over to the Accounts &amp; Modules tab to authenticate your Steam account.
        </p>
      </div>
    </div>

    <!-- Scenarios B & C: 1 or Multiple Connected Accounts -->
    <div v-else class="add-game-form">
      <!-- Store Selection Top Area -->
      <div class="store-selection-section mb-4">
        <div class="single-store-header">
          <div class="store-badge-single selected">
            <div class="store-logo-wrapper store-bg-steam">
              <svg 
                v-if="platformLogos.steam" 
                class="store-svg" 
                :viewBox="platformLogos.steam.viewBox" 
                fill="currentColor"
              >
                <path :d="platformLogos.steam.path" />
              </svg>
              <i v-else class="fa-solid fa-gamepad"></i>
            </div>
            <div class="store-info-single">
              <span class="store-title">Steam</span>
              <span class="store-sub">
                <i class="fa-solid fa-circle-check text-green mr-1"></i> Connected
                <span v-if="authenticatedServices[0]?.username" class="store-user">({{ authenticatedServices[0].username }})</span>
              </span>
            </div>
            <span class="badge badge-hit ml-auto">SELECTED</span>
          </div>
        </div>
      </div>

      <!-- Privacy Hint Notice (e.g. Steam Game Details is Friends Only) -->
      <div v-if="privacyHint && !loadingGames" class="privacy-notice-box mb-4">
        <div class="privacy-icon-col">
          <i class="fa-solid fa-shield-halved"></i>
        </div>
        <div class="privacy-content-col">
          <div class="privacy-title">Only Recent Activity Visible</div>
          <div class="privacy-text">{{ privacyHint }}</div>
        </div>
      </div>

      <!-- Game Selection Dropdown -->
      <div class="form-group mb-4">
        <div class="label-row">
          <label for="game-select">Game</label>
          <span v-if="loadingGames" class="loading-inline-hint text-xs text-accent">
            <i class="fa-solid fa-circle-notch fa-spin mr-1"></i> Fetching games live...
          </span>
          <span v-else-if="availableGames.length > 0" class="text-xs text-muted">
            {{ availableGames.length }} available
          </span>
          <span v-else class="text-xs text-muted">
            0 available
          </span>
        </div>

        <div class="select-wrapper">
          <select 
            id="game-select"
            class="form-control custom-select" 
            v-model="selectedGameAppId"
            :disabled="loadingGames || availableGames.length === 0"
          >
            <option value="" disabled>
              {{ loadingGames ? 'Fetching live games from store...' : (availableGames.length === 0 ? 'No games found on connected account' : 'Please select a game to add') }}
            </option>
            <option 
              v-for="game in availableGames" 
              :key="game.app_id" 
              :value="game.app_id"
            >
              {{ game.name }} (ID: {{ game.app_id }}){{ getExistingGameInfo(game.app_id) ? ` • In Library (${getExistingGameInfo(game.app_id).platform})` : '' }}
            </option>
          </select>
        </div>

        <p v-if="fetchError" class="field-error-text mt-1 text-xs text-danger">
          {{ fetchError }}
        </p>

        <!-- Existing Game In Library Notice -->
        <div v-if="existingInLibrary" class="existing-library-notice mt-2">
          <i class="fa-solid fa-circle-check text-green mr-1"></i>
          <span>
            Already in library for <strong>{{ existingInLibrary.platform }}</strong>.
            <template v-if="isMultiPlatform"> Select additional platforms to cache them together.</template>
          </span>
        </div>
      </div>

      <!-- Platform Selection Dropdown -->
      <div class="form-group mb-3">
        <div class="label-row">
          <label>Platform(s)</label>
          <span v-if="selectedGame && isSinglePlatform" class="text-xs text-muted">
            Single platform detected
          </span>
          <span v-else-if="selectedGame && isMultiPlatform" class="text-xs text-accent">
            Multi-platform title • Select one or more
          </span>
        </div>

        <!-- State 1: No Game Selected -> Disabled Dropdown with Prompt -->
        <div v-if="!selectedGame" class="select-wrapper">
          <select class="form-control custom-select disabled-input" disabled>
            <option selected disabled>Please select the platform(s) for this game</option>
          </select>
        </div>

        <!-- State 2: Game Selected, Exactly 1 Platform -> Disabled Dropdown with that Platform -->
        <div v-else-if="isSinglePlatform" class="single-platform-box">
          <select class="form-control custom-select disabled-input" disabled>
            <option selected>{{ availablePlatforms[0] }}</option>
          </select>
          <span class="platform-lock-tag" :title="availablePlatforms[0]">
            <i :class="getPlatformIcon(availablePlatforms[0])" class="mr-1"></i>
            <i class="fa-solid fa-lock text-xs ml-1 text-muted"></i>
          </span>
        </div>

        <!-- State 3: Game Selected, Multiple Platforms -> Interactive Multi-Select Dropdown -->
        <div v-else-if="isMultiPlatform" class="multi-select-container" ref="dropdownRef">
          <button 
            type="button" 
            class="multi-select-trigger form-control"
            :class="{ 'is-open': isPlatformDropdownOpen, 'has-selection': selectedPlatforms.length > 0 }"
            @click="isPlatformDropdownOpen = !isPlatformDropdownOpen"
          >
            <div class="trigger-content">
              <span v-if="selectedPlatforms.length === 0" class="placeholder-text">
                Please select the platform(s) for this game
              </span>
              <div v-else class="selected-pills-row">
                <span 
                  v-for="p in selectedPlatforms" 
                  :key="p" 
                  class="platform-pill"
                >
                  <i :class="getPlatformIcon(p)" class="mr-1"></i>
                  {{ p }}
                </span>
              </div>
            </div>
            <i class="fa-solid fa-chevron-down trigger-chevron" :class="{ 'rotated': isPlatformDropdownOpen }"></i>
          </button>

          <!-- Multi-Select Popover Menu -->
          <div v-if="isPlatformDropdownOpen" class="multi-select-menu">
            <div class="menu-header">
              <span class="text-xs text-muted uppercase font-bold">Available Platforms</span>
              <span class="text-xs text-accent">{{ selectedPlatforms.length }} / {{ availablePlatforms.length }} selected</span>
            </div>
            <div class="menu-options-list">
              <label 
                v-for="p in availablePlatforms" 
                :key="p"
                class="option-item"
                :class="{ 'checked': selectedPlatforms.includes(p) }"
              >
                <input 
                  type="checkbox" 
                  class="option-checkbox"
                  :checked="selectedPlatforms.includes(p)"
                  @change="togglePlatform(p)"
                />
                <div class="option-icon-box">
                  <i :class="getPlatformIcon(p)"></i>
                </div>
                <span class="option-label">{{ p }}</span>
                <span v-if="selectedPlatforms.includes(p)" class="check-indicator">
                  <i class="fa-solid fa-check"></i>
                </span>
              </label>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Modal Footer -->
    <template #footer>
      <button 
        type="button"
        class="btn btn-outline-secondary" 
        @click="handleCancel"
      >
        Cancel
      </button>
      <button 
        type="button"
        class="btn btn-primary" 
        :disabled="!canAdd" 
        @click="handleSubmit"
      >
        {{ existingInLibrary ? (isSinglePlatform && !canAdd ? 'Already in Library' : 'Update Platforms') : 'Add' }}
      </button>
    </template>
  </BaseModal>
</template>

<script setup>
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue';
import BaseModal from '../common/BaseModal.vue';
import { api } from '../../services/api';
import { platformLogos } from '../../utils/logos';
import { getPlatformIcon, getPlatformList } from '../../utils/formatters';

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false,
  },
  servicesList: {
    type: Array,
    default: () => [],
  },
  steamStatus: {
    type: Object,
    default: () => ({}),
  },
  existingLibrary: {
    type: Array,
    default: () => [],
  },
});

const emit = defineEmits(['update:modelValue', 'submit', 'switch-tab']);

// State
const selectedServiceId = ref('steam');
const availableGames = ref([]);
const loadingGames = ref(false);
const fetchError = ref('');
const privacyHint = ref('');
const selectedGameAppId = ref('');
const selectedPlatforms = ref([]);
const isPlatformDropdownOpen = ref(false);
const dropdownRef = ref(null);

// Authenticated Services
const authenticatedServices = computed(() => {
  const list = props.servicesList || [];
  return list.filter(svc => {
    if (svc.id === 'steam') {
      return props.steamStatus?.has_valid_auth || svc.status === 'connected';
    }
    return svc.status === 'connected';
  });
});

// Selected Game & Platforms
const selectedGame = computed(() => {
  if (!selectedGameAppId.value) return null;
  return availableGames.value.find(g => String(g.app_id) === String(selectedGameAppId.value)) || null;
});

const availablePlatforms = computed(() => {
  if (!selectedGame.value || !Array.isArray(selectedGame.value.platforms)) return [];
  return selectedGame.value.platforms;
});

const isSinglePlatform = computed(() => {
  return availablePlatforms.value.length === 1;
});

const isMultiPlatform = computed(() => {
  return availablePlatforms.value.length > 1;
});

function getExistingGameInfo(appId) {
  if (!appId || !props.existingLibrary) return null;
  return props.existingLibrary.find(
    g => String(g.app_id) === String(appId) && (g.service || 'steam') === selectedServiceId.value
  ) || null;
}

const existingInLibrary = computed(() => {
  if (!selectedGame.value) return null;
  return getExistingGameInfo(selectedGame.value.app_id);
});

// Can Add button be enabled?
const canAdd = computed(() => {
  if (authenticatedServices.value.length === 0) return false;
  if (!selectedGame.value) return false;
  if (isSinglePlatform.value) {
    if (existingInLibrary.value && existingInLibrary.value.platform) {
      const existingPlats = getPlatformList(existingInLibrary.value.platform);
      if (existingPlats.includes(availablePlatforms.value[0])) {
        return false;
      }
    }
    return true;
  }
  if (isMultiPlatform.value) return selectedPlatforms.value.length > 0;
  return false;
});

// Live game fetching
async function loadGames() {
  loadingGames.value = true;
  fetchError.value = '';
  privacyHint.value = '';
  try {
    const res = await api.getAvailableGames('steam');
    availableGames.value = res.games || [];
    privacyHint.value = res.hint || '';
  } catch (err) {
    console.error('Failed to load available Steam games', err);
    fetchError.value = err.message || 'Failed to fetch games list';
    availableGames.value = [];
    privacyHint.value = '';
  } finally {
    loadingGames.value = false;
  }
}

// Toggling platforms for multi-platform games
function togglePlatform(platform) {
  const idx = selectedPlatforms.value.indexOf(platform);
  if (idx === -1) {
    selectedPlatforms.value.push(platform);
  } else {
    selectedPlatforms.value.splice(idx, 1);
  }
}

// Close platform dropdown on outside click
function handleClickOutside(e) {
  if (dropdownRef.value && !dropdownRef.value.contains(e.target)) {
    isPlatformDropdownOpen.value = false;
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside);
});

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside);
});

// Watch when modal opens/closes
watch(() => props.modelValue, async (isOpen) => {
  if (isOpen) {
    selectedGameAppId.value = '';
    selectedPlatforms.value = [];
    isPlatformDropdownOpen.value = false;
    fetchError.value = '';
    privacyHint.value = '';

    if (authenticatedServices.value.length > 0) {
      selectedServiceId.value = 'steam';
      await loadGames();
    } else {
      availableGames.value = [];
    }
  }
});

// Watch game selection to handle single vs multi-platform setup
watch(selectedGameAppId, async (newId) => {
  isPlatformDropdownOpen.value = false;
  if (selectedGame.value && newId) {
    try {
      const platRes = await api.getGamePlatforms(selectedServiceId.value, newId);
      if (platRes && Array.isArray(platRes.platforms) && platRes.platforms.length > 0) {
        selectedGame.value.platforms = platRes.platforms;
      }
    } catch (e) {}

    const existing = getExistingGameInfo(newId);
    if (existing && existing.platform) {
      const existingPlats = getPlatformList(existing.platform);
      selectedPlatforms.value = Array.from(new Set(existingPlats));
    } else if (isSinglePlatform.value) {
      // Auto-assign single platform
      selectedPlatforms.value = [availablePlatforms.value[0]];
    } else {
      // Multiple platforms: user must pick
      selectedPlatforms.value = [];
    }
  } else {
    selectedPlatforms.value = [];
  }
});

function handleCancel() {
  isPlatformDropdownOpen.value = false;
  emit('update:modelValue', false);
}

function handleSubmit() {
  if (!canAdd.value || !selectedGame.value) return;

  let platformResult = '';
  if (isSinglePlatform.value) {
    platformResult = availablePlatforms.value[0];
  } else {
    platformResult = selectedPlatforms.value.join(', ');
  }

  emit('submit', {
    appId: selectedGame.value.app_id,
    name: selectedGame.value.name,
    service: selectedServiceId.value,
    platform: platformResult,
    replacePlatforms: true,
  });
  emit('update:modelValue', false);
}
</script>

<style scoped>
/* No accounts alert */
.no-accounts-alert {
  display: flex;
  gap: 1rem;
  padding: 1.25rem;
  background: rgba(239, 68, 68, 0.08);
  border: 1px solid rgba(239, 68, 68, 0.25);
  border-radius: 8px;
  color: #fca5a5;
  align-items: flex-start;
}

.alert-icon-box {
  font-size: 1.5rem;
  color: #ef4444;
  margin-top: 0.15rem;
}

.alert-content h4 {
  margin: 0 0 0.35rem 0;
  font-size: 1rem;
  color: #fff;
}

.alert-message {
  margin: 0;
  font-size: 0.9rem;
  line-height: 1.4;
  color: #fca5a5;
  font-weight: 500;
}

/* Store Selection Section */
.store-selection-section {
  border-bottom: 1px solid var(--border);
  padding-bottom: 1rem;
}

.section-label {
  display: block;
  font-size: 0.75rem;
  font-weight: 700;
  text-transform: uppercase;
  color: var(--text-muted);
  letter-spacing: 0.05em;
  margin-bottom: 0.5rem;
}

/* Scenario B: Single Store Badge */
.store-badge-single {
  display: flex;
  align-items: center;
  gap: 0.85rem;
  padding: 0.75rem 1rem;
  background: rgba(56, 189, 248, 0.08);
  border: 1px solid rgba(56, 189, 248, 0.3);
  border-radius: 8px;
}

.store-logo-wrapper {
  width: 36px;
  height: 36px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #1e293b;
  color: #fff;
  flex-shrink: 0;
}

.store-svg {
  width: 20px;
  height: 20px;
}

.store-info-single {
  display: flex;
  flex-direction: column;
}

.store-title {
  font-size: 0.95rem;
  font-weight: 700;
  color: #fff;
}

.store-sub {
  font-size: 0.75rem;
  color: var(--text-muted);
}

.store-user {
  color: var(--text-secondary);
}

/* Scenario C: Multiple Store Tabs */
.store-tabs-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
  gap: 0.5rem;
}

.store-tab-btn {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.6rem 0.75rem;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--text-secondary);
  cursor: pointer;
  transition: all 0.2s ease;
  position: relative;
  text-align: left;
}

.store-tab-btn:hover:not(:disabled) {
  border-color: rgba(255, 255, 255, 0.25);
  background: rgba(255, 255, 255, 0.03);
  color: #fff;
}

.store-tab-btn.active {
  background: rgba(56, 189, 248, 0.12);
  border-color: var(--accent);
  color: #fff;
  box-shadow: 0 0 12px rgba(56, 189, 248, 0.2);
}

.store-tab-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.store-tab-icon {
  width: 24px;
  height: 24px;
  border-radius: 5px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(255, 255, 255, 0.08);
  flex-shrink: 0;
}

.store-tab-icon .store-svg {
  width: 14px;
  height: 14px;
}

.store-tab-name {
  font-size: 0.8rem;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex: 1;
}

.store-active-indicator {
  color: var(--accent);
  font-size: 0.8rem;
}

/* Service Colors */
.store-bg-steam { color: #66c0f4; }

/* Form Controls */
.label-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.45rem;
}

.label-row label {
  font-size: 0.85rem;
  color: var(--text-secondary);
  font-weight: 600;
  margin: 0;
}

.select-wrapper {
  position: relative;
}

.privacy-notice-box {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
  padding: 0.75rem 1rem;
  background: rgba(99, 102, 241, 0.08);
  border: 1px solid rgba(99, 102, 241, 0.25);
  border-radius: var(--radius-md);
  color: var(--color-text-main);
}

.privacy-icon-col {
  color: var(--color-accent);
  font-size: 1.1rem;
  margin-top: 0.15rem;
}

.privacy-content-col {
  flex: 1;
}

.privacy-title {
  font-weight: 600;
  font-size: 0.82rem;
  color: #a5b4fc;
  margin-bottom: 0.2rem;
}

.privacy-text {
  font-size: 0.75rem;
  line-height: 1.45;
  color: var(--color-text-muted);
}

.custom-select {
  appearance: none;
  background-image: url("data:image/svg+xml;charset=UTF-8,%3csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%2394a3b8' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3e%3cpolyline points='6 9 12 15 18 9'%3e%3c/polyline%3e%3c/svg%3e");
  background-repeat: no-repeat;
  background-position: right 0.75rem center;
  background-size: 1em;
  padding-right: 2.5rem;
}

.disabled-input {
  opacity: 0.65;
  cursor: not-allowed;
  background-color: rgba(15, 23, 42, 0.6);
  border-color: rgba(255, 255, 255, 0.1);
  color: var(--text-muted);
}

/* Single Platform Box */
.single-platform-box {
  position: relative;
}

.platform-lock-tag {
  position: absolute;
  right: 2.25rem;
  top: 50%;
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  font-size: 0.85rem;
  color: var(--text-secondary);
  pointer-events: none;
}

/* Multi-select Dropdown */
.multi-select-container {
  position: relative;
}

.multi-select-trigger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  text-align: left;
  cursor: pointer;
  min-height: 42px;
  padding: 0.4rem 0.75rem;
}

.multi-select-trigger.is-open {
  border-color: var(--accent);
  box-shadow: 0 0 0 1px var(--accent);
}

.trigger-content {
  flex: 1;
  overflow: hidden;
}

.placeholder-text {
  color: var(--text-muted);
  font-size: 0.85rem;
}

.selected-pills-row {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}

.platform-pill {
  display: inline-flex;
  align-items: center;
  background: rgba(56, 189, 248, 0.15);
  color: var(--accent);
  border: 1px solid rgba(56, 189, 248, 0.3);
  padding: 2px 7px;
  border-radius: 4px;
  font-size: 0.75rem;
  font-weight: 600;
}

.trigger-chevron {
  font-size: 0.8rem;
  color: var(--text-muted);
  transition: transform 0.2s ease;
  margin-left: 0.5rem;
}

.trigger-chevron.rotated {
  transform: rotate(180deg);
}

/* Popover Menu */
.multi-select-menu {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 10px 25px rgba(0, 0, 0, 0.6);
  z-index: 50;
  overflow: hidden;
  animation: fadeIn 0.15s ease-out;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}

.menu-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.6rem 0.85rem;
  background: rgba(255, 255, 255, 0.03);
  border-bottom: 1px solid var(--border);
}

.menu-options-list {
  padding: 0.35rem;
  max-height: 200px;
  overflow-y: auto;
}

.option-item {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  padding: 0.55rem 0.65rem;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s ease;
  margin-bottom: 2px;
  user-select: none;
}

.option-item:hover {
  background: rgba(255, 255, 255, 0.05);
}

.option-item.checked {
  background: rgba(56, 189, 248, 0.1);
}

.option-checkbox {
  accent-color: var(--accent);
  width: 15px;
  height: 15px;
  cursor: pointer;
}

.option-icon-box {
  width: 20px;
  text-align: center;
  font-size: 0.95rem;
  color: var(--text-secondary);
}

.option-item.checked .option-icon-box {
  color: var(--accent);
}

.option-label {
  flex: 1;
  font-size: 0.85rem;
  font-weight: 500;
  color: #fff;
}

.check-indicator {
  color: var(--accent);
  font-size: 0.8rem;
}

.existing-library-notice {
  display: flex;
  align-items: center;
  font-size: 0.78rem;
  color: var(--text-secondary);
  background: rgba(34, 197, 94, 0.08);
  border: 1px solid rgba(34, 197, 94, 0.25);
  border-radius: 6px;
  padding: 0.45rem 0.75rem;
  gap: 0.4rem;
}
</style>
