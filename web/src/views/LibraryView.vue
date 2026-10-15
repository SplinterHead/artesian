<template>
  <div class="tab-view">
    <!-- Active Prefill Banner -->
    <div v-if="activePrefillJob" class="card active-job-card mb-4">
      <div class="job-header">
        <div>
          <span class="badge badge-accent">PREFILL ACTIVE</span>
          <h2 class="mt-1">{{ activePrefillJob.app_name }}</h2>
          <span class="text-muted">App ID: {{ activePrefillJob.app_id }} • Depot Sync</span>
        </div>
        <div class="job-metrics">
          <div class="metric-block">
            <span class="metric-label">Transfer Speed</span>
            <span class="metric-val text-green">{{ activePrefillJob.speed_mbps ? activePrefillJob.speed_mbps.toFixed(1) : 0 }} Mbps</span>
          </div>
          <div class="metric-block">
            <span class="metric-label">Remaining Time</span>
            <span class="metric-val text-accent">{{ formatETA(activePrefillJob.eta_seconds) }}</span>
          </div>
        </div>
      </div>

      <!-- Animated Progress Bar -->
      <div class="progress-container mt-3">
        <div class="progress-track">
          <div 
            class="progress-fill" 
            :style="{ width: (activePrefillJob.progress_pct || 0) + '%' }"
          ></div>
        </div>
        <div class="progress-details mt-2">
          <span>{{ activePrefillJob.status.toUpperCase() }}: {{ activePrefillJob.downloaded_chunks + activePrefillJob.existing_chunks }} / {{ activePrefillJob.total_chunks }} chunks</span>
          <span class="font-bold text-accent">{{ (activePrefillJob.progress_pct || 0).toFixed(1) }}%</span>
        </div>
      </div>
    </div>

    <!-- Table 1: Monitored Game Library -->
    <div class="card mb-4">
      <div class="card-header">
        <div>
          <h3>Game Library</h3>
          <span class="text-xs text-muted">Explicitly monitored games configured for automated caching and prefilling</span>
        </div>
        <button class="btn btn-primary btn-sm" @click="$emit('open-add-game')">
          + Add Game
        </button>
      </div>

      <div class="table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th style="width: 50px;">Store</th>
              <th>App ID</th>
              <th>Game Name</th>
              <th>Status</th>
              <th>Auto-Update</th>
              <th>Last Sync</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="library.length === 0">
              <td colspan="7" class="empty-cell">
                No games added to library yet. Add a game above or select from cached games below.
              </td>
            </tr>
            <tr v-for="game in library" :key="(game.service || 'steam') + game.app_id">
              <td>
                <div 
                  class="store-logo-box" 
                  :class="'store-logo-' + (getLogoKey(game.service) || 'generic')"
                  :title="game.service ? game.service.toUpperCase() : 'Steam'"
                >
                  <svg 
                    v-if="platformLogos[getLogoKey(game.service)]" 
                    class="store-svg-logo" 
                    :viewBox="platformLogos[getLogoKey(game.service)].viewBox" 
                    fill="currentColor"
                  >
                    <path :d="platformLogos[getLogoKey(game.service)].path" />
                  </svg>
                  <i v-else class="fa-solid fa-gamepad fallback-logo-icon"></i>
                </div>
              </td>
              <td><code>{{ game.app_id }}</code></td>
              <td><strong>{{ game.name }}</strong></td>
              <td>
                <span :class="['status-badge', game.status]">
                  {{ game.status.toUpperCase() }}
                </span>
              </td>
              <td>
                <span class="badge" :class="game.auto_update ? 'badge-hit' : 'badge-secondary'">
                  {{ game.auto_update ? 'ENABLED' : 'OFF' }}
                </span>
              </td>
              <td class="text-muted">{{ formatDate(game.last_checked) }}</td>
              <td>
                <div class="table-actions">
                  <button 
                    class="btn btn-xs btn-primary"
                    :disabled="activePrefillJob && activePrefillJob.status === 'downloading'"
                    title="Initialize full depot prefill"
                    @click="$emit('trigger-prefill', game)"
                  >
                    ⚡ Initialize
                  </button>
                  <button 
                    class="btn btn-xs btn-outline-danger"
                    title="Remove game from library"
                    @click="$emit('remove-from-library', game)"
                  >
                    <i class="fa-solid fa-trash"></i>
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Table 2: Intercepted & Cached Games (Underneath Library) -->
    <div class="card">
      <div class="card-header">
        <div>
          <h3>Cached Games</h3>
          <span class="text-xs text-muted">Games intercepted through the proxy cache, not yet explicitly added to your library</span>
        </div>
        <button class="btn btn-secondary btn-xs" @click="$emit('refresh-cached')">
          <i class="fa-solid fa-rotate mr-1"></i> Refresh
        </button>
      </div>

      <div class="table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th style="width: 50px;">Store</th>
              <th>App ID</th>
              <th>Game Title</th>
              <th>Cached Content</th>
              <th>Last Intercepted</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="cachedGames.length === 0">
              <td colspan="6" class="empty-cell">
                No unmanaged games in cache yet. Traffic intercepted from Steam, Epic, or Battle.net will appear here automatically.
              </td>
            </tr>
            <tr v-for="item in cachedGames" :key="(item.service || 'steam') + item.app_id + item.name">
              <td>
                <div 
                  class="store-logo-box" 
                  :class="'store-logo-' + (getLogoKey(item.service) || 'generic')"
                  :title="item.service ? item.service.toUpperCase() : 'Steam'"
                >
                  <svg 
                    v-if="platformLogos[getLogoKey(item.service)]" 
                    class="store-svg-logo" 
                    :viewBox="platformLogos[getLogoKey(item.service)].viewBox" 
                    fill="currentColor"
                  >
                    <path :d="platformLogos[getLogoKey(item.service)].path" />
                  </svg>
                  <i v-else class="fa-solid fa-gamepad fallback-logo-icon"></i>
                </div>
              </td>
              <td><code>{{ item.app_id || '—' }}</code></td>
              <td>
                <div class="game-cell">
                  <strong>{{ item.name }}</strong>
                  <span v-if="item.platform" class="platform-tag" :title="item.platform">
                    <i :class="getPlatformIcon(item.platform)"></i>
                  </span>
                </div>
              </td>
              <td>
                <div class="size-cell">
                  <span class="depot-size">{{ formatBytes(item.cached_bytes) }}</span>
                  <span v-if="item.cached_chunks" class="chunk-badge">{{ item.cached_chunks }} chunks</span>
                  <span v-if="item.total_size > 0 && item.total_size !== item.cached_bytes" class="text-xs text-muted block w-full mt-0.5">
                    Depot: {{ formatBytes(item.total_size) }}
                  </span>
                </div>
              </td>
              <td class="text-muted">{{ formatDate(item.last_seen) }}</td>
              <td>
                <button 
                  class="btn btn-xs btn-primary add-library-btn"
                  title="Add this intercepted game to your managed library"
                  @click="$emit('add-to-library', item)"
                >
                  <i class="fa-solid fa-plus mr-1"></i> Add to Library
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
import { 
  formatBytes, 
  formatETA, 
  formatDate, 
  getLogoKey, 
  getPlatformIcon 
} from '../utils/formatters';
import { platformLogos } from '../utils/logos';

defineProps({
  library: {
    type: Array,
    required: true,
  },
  cachedGames: {
    type: Array,
    default: () => [],
  },
  activePrefillJob: {
    type: Object,
    default: null,
  },
});

defineEmits(['open-add-game', 'trigger-prefill', 'add-to-library', 'remove-from-library', 'refresh-cached']);
</script>

<style scoped>
.active-job-card {
  background: linear-gradient(135deg, #111e33, #0f172a);
  border-color: var(--accent);
  box-shadow: 0 0 20px rgba(56, 189, 248, 0.15);
}

.job-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
}

.job-metrics {
  display: flex;
  gap: 1.5rem;
}

.metric-block {
  display: flex;
  flex-direction: column;
  text-align: right;
}

.metric-label {
  font-size: 0.75rem;
  color: var(--text-muted);
}

.metric-val {
  font-size: 1.25rem;
  font-weight: 700;
}

.progress-track {
  width: 100%;
  height: 12px;
  background: rgba(255, 255, 255, 0.08);
  border-radius: 6px;
  overflow: hidden;
}

.progress-fill {
  height: 100%;
  background: linear-gradient(90deg, var(--accent), var(--accent-green));
  transition: width 0.3s ease;
}

.progress-details {
  display: flex;
  justify-content: space-between;
  font-size: 0.85rem;
}

.empty-cell {
  text-align: center;
  padding: 2.5rem 1rem !important;
  color: var(--text-muted);
  font-style: italic;
}

.game-cell {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
}

.platform-tag {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  background: rgba(15, 23, 42, 0.85);
  border: 1px solid var(--border);
  color: var(--text-secondary);
  font-size: 0.75rem;
  border-radius: 5px;
  transition: all 0.2s ease;
  cursor: default;
}

.platform-tag:hover {
  color: var(--text-primary);
  border-color: rgba(255, 255, 255, 0.25);
  background: rgba(30, 41, 59, 0.9);
}

.size-cell {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  flex-wrap: wrap;
}

.depot-size {
  font-family: var(--font-mono, monospace);
  font-weight: 600;
  color: var(--text-secondary);
}

.chunk-badge {
  font-size: 0.7rem;
  padding: 0.12rem 0.45rem;
  border-radius: 9999px;
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: var(--text-secondary);
  font-family: var(--font-mono, monospace);
}

.table-actions {
  display: flex;
  align-items: center;
  gap: 0.4rem;
}

.add-library-btn {
  font-weight: 600;
  display: inline-flex;
  align-items: center;
}

.store-logo-box {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: 7px;
  cursor: default;
}

.store-svg-logo {
  width: 17px;
  height: 17px;
}

.store-logo-steam {
  background: radial-gradient(circle at top left, rgba(102, 192, 244, 0.25), rgba(23, 26, 38, 0.8));
  color: #66c0f4;
  border: 1px solid rgba(102, 192, 244, 0.35);
}

.store-logo-epic {
  background: radial-gradient(circle at top left, rgba(255, 255, 255, 0.2), rgba(30, 30, 30, 0.8));
  color: #f8fafc;
  border: 1px solid rgba(255, 255, 255, 0.3);
}

.store-logo-blizzard {
  background: radial-gradient(circle at top left, rgba(0, 174, 255, 0.25), rgba(15, 23, 42, 0.8));
  color: #00aeff;
  border: 1px solid rgba(0, 174, 255, 0.35);
}

.store-logo-riot {
  background: radial-gradient(circle at top left, rgba(235, 0, 41, 0.25), rgba(20, 10, 15, 0.8));
  color: #eb0029;
  border: 1px solid rgba(235, 0, 41, 0.35);
}

.store-logo-xbox {
  background: radial-gradient(circle at top left, rgba(44, 162, 67, 0.25), rgba(10, 25, 15, 0.8));
  color: #2ca243;
  border: 1px solid rgba(44, 162, 67, 0.35);
}

.store-logo-generic {
  background: rgba(148, 163, 184, 0.15);
  color: #94a3b8;
  border: 1px solid rgba(148, 163, 184, 0.3);
}

.fallback-logo-icon {
  font-size: 0.9rem;
}
</style>
