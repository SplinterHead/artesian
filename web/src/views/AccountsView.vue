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
          'monitoring-disabled': !svc.intercept_enabled 
        }"
      >
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
              <h3>{{ svc.display_name }}</h3>
              <span class="company-sub">{{ svc.company }}</span>
            </div>
            <div class="service-badges mt-1">
              <span :class="['status-badge', svc.status]">
                <span class="status-dot-sm"></span>
                {{ getServiceStatusText(svc) }}
              </span>
              <span v-if="svc.auth_supported" class="tag tag-blue">AUTH INTEGRATION</span>
              <span v-if="svc.prefill_supported" class="tag tag-purple">PREFILL READY</span>
            </div>
          </div>

          <!-- Request Monitoring Toggle -->
          <ToggleSwitch 
            v-model="svc.intercept_enabled"
            :active-text="'Monitoring ON'"
            :inactive-text="'Monitoring OFF'"
            :title="'Turn ' + (svc.intercept_enabled ? 'off' : 'on') + ' request monitoring for ' + svc.display_name"
            @change="$emit('toggle-monitoring', svc)"
          />
        </div>

        <!-- Matching Patterns -->
        <div class="domains-container mt-3">
          <div class="domains-header">
            <span class="text-xs text-muted uppercase">Matching Patterns ({{ svc.domains.length }})</span>
            <span v-if="!svc.intercept_enabled" class="badge badge-paused">PAUSED</span>
          </div>
          <div class="domain-pills mt-1">
            <span v-for="d in svc.domains" :key="d" class="domain-pill"><code>{{ d }}</code></span>
          </div>
        </div>

        <!-- Service Action Buttons -->
        <div class="service-actions-bar mt-4">
          <template v-if="svc.id === 'steam'">
            <button 
              v-if="svc.status !== 'connected'" 
              class="btn btn-sm btn-primary" 
              @click="$emit('open-steam-login')"
            >
              Connect Steam Account
            </button>
            <div v-else class="connected-btn-group">
              <button class="btn btn-sm btn-outline-danger" @click="$emit('logout-steam')">
                Disconnect ({{ svc.username }})
              </button>
              <button class="btn btn-sm btn-secondary" @click="$emit('switch-tab', 'library')">
                Open Library →
              </button>
            </div>
          </template>
          <template v-else>
            <div class="future-service-row">
              <span class="text-muted text-xs">DNS Routing & Pass-Through Caching Active</span>
              <button class="btn btn-xs btn-secondary" @click="$emit('open-details', svc)">
                Details
              </button>
            </div>
          </template>
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

defineEmits([
  'toggle-monitoring', 
  'open-steam-login', 
  'logout-steam', 
  'switch-tab', 
  'open-details'
]);

function getServiceStatusText(svc) {
  if (svc.id === 'steam') {
    if (svc.status === 'connected') return `Connected (${svc.username})`;
    if (svc.status === 'awaiting_2fa') return 'Awaiting 2FA Code';
    if (svc.status === 'connecting') return 'Connecting...';
    return 'Disconnected';
  }
  return 'Active (Caching)';
}
</script>

<style scoped>
.services-hub-intro .card-header {
  margin-bottom: 0;
}

.services-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
  gap: 1.5rem;
}

.service-panel {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 1.5rem;
  transition: all 0.2s ease;
}

.service-panel:hover {
  border-color: rgba(56, 189, 248, 0.4);
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.2);
}

.service-panel.service-connected {
  border-color: rgba(52, 211, 153, 0.4);
}

.service-panel-top {
  display: flex;
  align-items: flex-start;
  gap: 1rem;
}

.service-icon-box {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  background: #0f172a;
  border: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all 0.2s ease;
}

.platform-svg-logo {
  width: 22px;
  height: 22px;
  display: block;
}

.service-icon-box.icon-steam {
  color: #66c0f4;
  background: rgba(102, 192, 244, 0.1);
  border-color: rgba(102, 192, 244, 0.25);
}

.service-icon-box.icon-epic {
  color: #ffffff;
  background: rgba(255, 255, 255, 0.08);
  border-color: rgba(255, 255, 255, 0.2);
}

.service-icon-box.icon-blizzard {
  color: #00aeff;
  background: rgba(0, 174, 255, 0.1);
  border-color: rgba(0, 174, 255, 0.25);
}

.service-icon-box.icon-riot {
  color: #eb0029;
  background: rgba(235, 0, 41, 0.1);
  border-color: rgba(235, 0, 41, 0.25);
}

.service-icon-box.icon-xbox {
  color: #2ca243;
  background: rgba(16, 124, 16, 0.12);
  border-color: rgba(16, 124, 16, 0.25);
}

.service-title-area {
  flex: 1;
}

.service-name-row {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
}

.company-sub {
  font-size: 0.75rem;
  color: var(--text-muted);
}

.service-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
}

.monitoring-disabled .domain-pills {
  opacity: 0.45;
  filter: grayscale(0.6);
  transition: all 0.2s ease;
}

.domains-container {
  background: #0b1322;
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 0.75rem;
}

.domains-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.domain-pills {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
}

.domain-pill {
  background: #080f1a;
  border: 1px solid rgba(255, 255, 255, 0.06);
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 0.75rem;
  color: var(--accent);
}

.service-actions-bar {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  padding-top: 1rem;
  border-top: 1px solid var(--border);
}

.connected-btn-group {
  display: flex;
  gap: 0.5rem;
  width: 100%;
  justify-content: space-between;
}

.future-service-row {
  display: flex;
  width: 100%;
  justify-content: space-between;
  align-items: center;
}
</style>
