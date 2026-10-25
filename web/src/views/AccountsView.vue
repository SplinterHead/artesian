<template>
  <div class="tab-view">
    <div class="services-hub-intro card mb-4">
      <div class="card-header">
        <h3>Game Platform Accounts & CDN Modules</h3>
      </div>
    </div>

    <div class="services-grid">
      <div
        v-for="svc in servicesList"
        :key="svc.id"
        class="service-panel card"
        :class="{
          'service-connected': svc.status === 'connected',
          'monitoring-disabled': !svc.intercept_enabled,
        }"
      >
        <!-- Card Top Identity Header -->
        <div class="service-panel-top">
          <div class="service-icon-box" :class="'icon-' + svc.id">
            <svg
              v-if="platformLogos[svc.id]"
              class="platform-svg-logo"
              :viewBox="platformLogos[svc.id].viewBox"
              fill="currentColor"
            >
              <path :d="platformLogos[svc.id].path" />
            </svg>
            <i v-else class="fa-solid fa-gamepad"></i>
          </div>
          <div class="service-title-area">
            <div class="service-name-row">
              <h3 class="service-title-heading" :title="svc.display_name">
                {{ svc.display_name }}
              </h3>
              <span class="company-sub">{{ svc.company }}</span>
            </div>
            <div class="service-badges mt-1">
              <span :class="['status-badge', svc.status]">
                <span class="status-dot-sm"></span>
                {{ getServiceStatusText(svc) }}
              </span>
            </div>
          </div>
        </div>

        <!-- Integrated Controls Bar (Monitoring & Auto-Update on Separate Rows) -->
        <div class="service-toggles-bar">
          <div
            class="toggle-card-row"
            :class="{ 'is-active': svc.intercept_enabled }"
            :title="
              'Turn ' +
              (svc.intercept_enabled ? 'off' : 'on') +
              ' request monitoring for ' +
              svc.display_name
            "
          >
            <div class="toggle-row-left">
              <span class="toggle-label-main">
                <i class="fa-solid fa-satellite-dish mr-1 text-xs"></i> Monitoring
              </span>
              <span class="toggle-state-badge" :class="{ 'is-on': svc.intercept_enabled }">
                {{ svc.intercept_enabled ? 'ON' : 'OFF' }}
              </span>
            </div>
            <ToggleSwitch
              v-model="svc.intercept_enabled"
              @change="$emit('toggle-monitoring', svc)"
            />
          </div>

          <div class="toggle-row-divider"></div>

          <div
            class="toggle-card-row"
            :class="{ 'is-active': svc.auto_update }"
            :title="
              'Turn ' +
              (svc.auto_update ? 'off' : 'on') +
              ' automatic updates for ' +
              svc.display_name
            "
          >
            <div class="toggle-row-left">
              <span class="toggle-label-main">
                <i class="fa-solid fa-arrows-rotate mr-1 text-xs"></i> Auto-Update
              </span>
              <span class="toggle-state-badge" :class="{ 'is-on': svc.auto_update }">
                {{ svc.auto_update ? 'ON' : 'OFF' }}
              </span>
            </div>
            <ToggleSwitch v-model="svc.auto_update" @change="$emit('toggle-autoupdate', svc)" />
          </div>
        </div>

        <!-- Matching Patterns -->
        <div class="domains-container mt-3">
          <div class="domains-header">
            <span class="text-xs text-muted uppercase font-semibold"
              >Matching Patterns ({{ svc.domains.length }})</span
            >
            <span v-if="!svc.intercept_enabled" class="badge badge-paused">PAUSED</span>
          </div>
          <div class="domain-pills mt-1">
            <span v-for="d in svc.domains" :key="d" class="domain-pill"
              ><code>{{ d }}</code></span
            >
          </div>
        </div>

        <!-- Service Action Buttons -->
        <div class="service-actions-bar mt-4">
          <button
            v-if="svc.status !== 'connected'"
            class="btn btn-sm btn-primary btn-block"
            :class="{ 'btn-awaiting-2fa': svc.status === 'awaiting_2fa' }"
            @click="handleConnectButtonClick(svc)"
          >
            <template v-if="svc.status === 'awaiting_2fa'">
              <i class="fa-solid fa-shield-halved mr-1"></i> Enter 2FA / Approve
            </template>
            <template v-else-if="svc.status === 'connecting'">
              <i class="fa-solid fa-circle-notch fa-spin mr-1"></i> Connecting...
            </template>
            <template v-else> Connect {{ svc.display_name }} Account </template>
          </button>
          <div v-else class="connected-btn-group">
            <button
              class="btn btn-sm btn-outline-danger"
              :title="'Disconnect ' + svc.username"
              @click="$emit('logout-service', svc.id)"
            >
              Disconnect ({{ svc.username }})
            </button>
            <button class="btn btn-sm btn-secondary" @click="$emit('switch-tab', 'library')">
              Open Library →
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { platformLogos } from '../utils/logos';
import ToggleSwitch from '../components/common/ToggleSwitch.vue';

defineProps({
  servicesList: {
    type: Array,
    required: true,
  },
});

const emit = defineEmits([
  'toggle-monitoring',
  'toggle-autoupdate',
  'open-login',
  'open-steam-2fa',
  'logout-service',
  'switch-tab',
]);

function handleConnectButtonClick(svc) {
  if (svc.id === 'steam' && svc.status === 'awaiting_2fa') {
    emit('open-steam-2fa');
  } else {
    emit('open-login', svc);
  }
}

function getServiceStatusText(svc) {
  if (svc.status === 'connected') return `Connected (${svc.username})`;
  if (svc.status === 'awaiting_2fa') return 'Awaiting 2FA / Approval';
  if (svc.status === 'connecting') return 'Connecting...';
  return 'Disconnected';
}
</script>

<style scoped>
.services-hub-intro .card-header {
  margin-bottom: 0;
}

.services-grid {
  display: grid;
  gap: 1.5rem;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 420px), 1fr));
}

/* On standard desktop and 90% zoom viewports (1350px - 2100px), enforce 3 per row so cards overflow to next row */
@media (min-width: 1350px) and (max-width: 2100px) {
  .services-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

/* Ultra-wide / 4K displays (> 2100px) have room to fit 4 or 5 with full breathing room */
@media (min-width: 2101px) and (max-width: 2550px) {
  .services-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@media (min-width: 2551px) {
  .services-grid {
    grid-template-columns: repeat(5, minmax(0, 1fr));
  }
}

/* Laptops & tablet view (800px - 1349px): 2 columns */
@media (min-width: 800px) and (max-width: 1349px) {
  .services-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

/* Mobile (< 800px): 1 column */
@media (max-width: 799px) {
  .services-grid {
    grid-template-columns: 1fr;
  }
}

.service-panel {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 1.25rem;
  min-width: 0;
  transition:
    border-color 0.2s ease,
    box-shadow 0.2s ease;
}

.service-panel:hover {
  border-color: rgba(56, 189, 248, 0.4);
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.25);
}

.service-panel.service-connected {
  border-color: rgba(52, 211, 153, 0.4);
}

.service-panel-top {
  display: flex;
  align-items: flex-start;
  gap: 0.85rem;
  min-width: 0;
}

.service-icon-box {
  width: 44px;
  height: 44px;
  border-radius: 10px;
  background: #0f172a;
  border: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all 0.2s ease;
}

.platform-svg-logo {
  width: 20px;
  height: 20px;
  display: block;
}

.service-icon-box.icon-steam {
  color: #66c0f4;
  background: rgba(102, 192, 244, 0.1);
  border-color: rgba(102, 192, 244, 0.25);
}

.service-icon-box.icon-xbox {
  color: #107c10;
  background: rgba(16, 124, 16, 0.12);
  border-color: rgba(16, 124, 16, 0.35);
  box-shadow: 0 0 12px rgba(16, 124, 16, 0.15);
}

.service-title-area {
  flex: 1;
  min-width: 0;
}

.service-name-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.35rem;
}

.service-title-heading {
  font-size: 1.15rem;
  font-weight: 700;
  color: var(--text-primary);
  margin: 0;
  line-height: 1.25;
}

.company-sub {
  font-size: 0.72rem;
  color: var(--text-muted);
  flex-shrink: 0;
}

.service-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}

/* Controls Bar (Separate Rows) */
.service-toggles-bar {
  display: flex;
  flex-direction: column;
  background: rgba(11, 19, 34, 0.75);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 0.55rem 0.8rem;
  margin-top: 0.85rem;
}

.toggle-card-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}

.toggle-row-left {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.toggle-label-main {
  font-size: 0.72rem;
  font-weight: 700;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.03em;
  display: flex;
  align-items: center;
}

.toggle-label-main i {
  color: var(--text-muted);
  transition: color 0.2s ease;
}

.toggle-card-row.is-active .toggle-label-main i {
  color: var(--accent);
}

.toggle-state-badge {
  font-size: 0.62rem;
  font-weight: 700;
  padding: 1px 5px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.06);
  color: var(--text-muted);
  transition: all 0.2s ease;
}

.toggle-state-badge.is-on {
  background: rgba(56, 189, 248, 0.15);
  color: var(--accent);
}

.toggle-row-divider {
  height: 1px;
  background: rgba(255, 255, 255, 0.06);
  margin: 0.45rem 0;
  width: 100%;
}

/* Domains Container */
.monitoring-disabled .domain-pills {
  opacity: 0.45;
  filter: grayscale(0.6);
  transition: all 0.2s ease;
}

.domains-container {
  background: #0b1322;
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 0.65rem 0.75rem;
  height: 108px;
  display: flex;
  flex-direction: column;
}

.domains-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.35rem;
  flex-shrink: 0;
}

.font-semibold {
  font-weight: 600;
}

.domain-pills {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
  overflow-y: auto;
  flex: 1;
  padding-right: 2px;
}

.domain-pills::-webkit-scrollbar {
  width: 4px;
}

.domain-pills::-webkit-scrollbar-track {
  background: transparent;
}

.domain-pills::-webkit-scrollbar-thumb {
  background: rgba(255, 255, 255, 0.12);
  border-radius: 4px;
}

.domain-pills::-webkit-scrollbar-thumb:hover {
  background: rgba(255, 255, 255, 0.25);
}

.domain-pill {
  background: #080f1a;
  border: 1px solid rgba(255, 255, 255, 0.06);
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 0.72rem;
  color: var(--accent);
  line-height: 1.3;
  white-space: nowrap;
}

/* Actions Bar */
.service-actions-bar {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  padding-top: 0.85rem;
  margin-top: 0.85rem;
  border-top: 1px solid var(--border);
}

.btn-block {
  width: 100%;
}

.connected-btn-group {
  display: flex;
  gap: 0.75rem;
  width: 100%;
}

.connected-btn-group .btn {
  flex: 1;
  padding: 0.5rem 0.85rem;
  font-size: 0.85rem;
  font-weight: 600;
  white-space: nowrap;
}
</style>
