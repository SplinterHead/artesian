<template>
  <div class="tab-view">
    <!-- Metric Cards -->
    <div class="stats-grid">
      <div class="stat-card">
        <span class="stat-label">Cache Hit Ratio</span>
        <div class="stat-value text-accent">{{ stats.hit_ratio ? stats.hit_ratio.toFixed(1) : 0 }}%</div>
        <span class="stat-sub">{{ stats.cache_hits }} hits / {{ stats.total_requests }} requests</span>
      </div>

      <div class="stat-card">
        <span class="stat-label">Bandwidth Saved</span>
        <div class="stat-value text-green">{{ formatBytes(stats.bytes_saved) }}</div>
        <span class="stat-sub">Served directly from storage</span>
      </div>

      <div class="stat-card">
        <span class="stat-label">Disk Read I/O</span>
        <div class="stat-value text-blue">{{ formatSpeed(stats.read_bytes_sec) }}</div>
        <span class="stat-sub">Streaming zero-copy to clients</span>
      </div>

      <div class="stat-card">
        <span class="stat-label">Disk Write I/O</span>
        <div class="stat-value text-amber">{{ formatSpeed(stats.write_bytes_sec) }}</div>
        <span class="stat-sub">Staging chunk downloads</span>
      </div>
    </div>

    <!-- Active Downloads -->
    <div class="card mb-4">
      <div class="card-header">
        <h3>Active Downloads</h3>
        <div class="header-actions">
          <button class="btn btn-xs btn-secondary" @click="$emit('clear-traffic')">Clear</button>
        </div>
      </div>

      <div class="table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th></th>
              <th>Client</th>
              <th>Store</th>
              <th>Game Title</th>
              <th>Depot ID</th>
              <th>Size</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="activeDownloads.length === 0">
              <td colspan="6" class="empty-cell">
                Listening on :80. Traffic from game clients will bundle here in real time...
              </td>
            </tr>
            <tr v-for="d in activeDownloads" :key="d.key || (d.client_ip + d.store + d.game_title)" class="traffic-row">
              <td class="text-muted"><code>{{ d.timestamp || d.time }}</code></td>
              <td><code class="ip-code">{{ cleanClientIP(d.client_ip) }}</code></td>
              <td>
                <div 
                  class="store-logo-box" 
                  :class="'store-logo-' + (getLogoKey(d.service, d.store) || 'generic')"
                  :title="d.store || formatStore(d.service)"
                >
                  <svg 
                    v-if="platformLogos[getLogoKey(d.service, d.store)]" 
                    class="store-svg-logo" 
                    :viewBox="platformLogos[getLogoKey(d.service, d.store)].viewBox" 
                    fill="currentColor"
                  >
                    <path :d="platformLogos[getLogoKey(d.service, d.store)].path" />
                  </svg>
                  <i v-else class="fa-solid fa-gamepad fallback-logo-icon"></i>
                </div>
              </td>
              <td>
                <div class="game-cell">
                  <strong class="game-title">{{ d.game_title || 'Game Asset' }}</strong>
                  <span class="platform-tag" :title="d.platform || 'PC'">
                    <i :class="getPlatformIcon(d.platform)"></i>
                  </span>
                </div>
              </td>
              <td>
                <div class="depots-container">
                  <!-- Steam Depots: All depots for this game bundled as clickable badges -->
                  <template v-if="d.service === 'steam'">
                    <template v-if="d.depot_links && d.depot_links.length > 0">
                      <a 
                        v-for="link in d.depot_links" 
                        :key="link.depot_id"
                        :href="link.lookup_url || ('https://steamdb.info/depot/' + link.depot_id)" 
                        target="_blank" 
                        rel="noopener noreferrer"
                        class="depot-link steamdb-link"
                        :title="'View Depot ' + link.depot_id + ' on SteamDB'"
                      >
                        <code>{{ link.depot_id }}</code>
                        <i class="fa-solid fa-arrow-up-right-from-square external-icon"></i>
                      </a>
                    </template>
                    <a 
                      v-else-if="d.depot_id" 
                      :href="d.lookup_url || ('https://steamdb.info/depot/' + d.depot_id)" 
                      target="_blank" 
                      rel="noopener noreferrer"
                      class="depot-link steamdb-link"
                      :title="'View Depot ' + d.depot_id + ' on SteamDB'"
                    >
                      <code>{{ d.depot_id }}</code>
                      <i class="fa-solid fa-arrow-up-right-from-square external-icon"></i>
                    </a>
                    <span v-else class="text-muted">—</span>
                  </template>

                  <!-- Epic Games: Link to egdata.app -->
                  <template v-else-if="d.service === 'epic'">
                    <a 
                      :href="d.lookup_url || 'https://egdata.app'" 
                      target="_blank" 
                      rel="noopener noreferrer"
                      class="depot-link egdata-link"
                      :title="'Look up chunk data on EGData (https://egdata.app)'"
                    >
                      <code>{{ formatChunkID(d.chunk_id) || 'egdata' }}</code>
                      <i class="fa-solid fa-arrow-up-right-from-square external-icon"></i>
                    </a>
                  </template>

                  <!-- Generic / Other Stores -->
                  <template v-else>
                    <a 
                      v-if="d.lookup_url" 
                      :href="d.lookup_url" 
                      target="_blank" 
                      rel="noopener noreferrer"
                      class="depot-link"
                    >
                      <code>{{ d.depot_id || formatChunkID(d.chunk_id) || 'asset' }}</code>
                      <i class="fa-solid fa-arrow-up-right-from-square external-icon"></i>
                    </a>
                    <code v-else-if="d.depot_id">{{ d.depot_id }}</code>
                    <span v-else class="text-muted">—</span>
                  </template>
                </div>
              </td>
              <td>
                <div class="size-cell">
                  <span class="depot-size">
                    {{ formatBytes(d.total_depot_size || d.bytes_transferred || 0) }}
                  </span>
                  <span v-if="d.chunk_count && d.chunk_count > 1" class="chunk-badge">
                    {{ d.chunk_count }} files
                  </span>
                </div>
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
  formatSpeed, 
  cleanClientIP, 
  formatChunkID, 
  formatStore, 
  getLogoKey, 
  getPlatformIcon 
} from '../utils/formatters';
import { platformLogos } from '../utils/logos';

defineProps({
  stats: {
    type: Object,
    required: true,
  },
  activeDownloads: {
    type: Array,
    required: true,
  },
  totalFilesLogged: {
    type: Number,
    default: 0,
  },
});

defineEmits(['clear-traffic']);
</script>

<style scoped>
.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 1.25rem;
  margin-bottom: 2rem;
}

.stat-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 1.25rem;
}

.stat-label {
  font-size: 0.8rem;
  color: var(--text-secondary);
  text-transform: uppercase;
}

.stat-value {
  font-size: 1.85rem;
  font-weight: 700;
  margin: 0.5rem 0 0.25rem;
}

.stat-sub {
  font-size: 0.8rem;
  color: var(--text-muted);
}

.traffic-row {
  transition: background 0.2s;
}

.traffic-row:hover {
  background: rgba(255, 255, 255, 0.02);
}

.ip-code {
  color: #38bdf8;
  font-weight: 500;
}

.store-logo-box {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: 8px;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  cursor: default;
}

.store-svg-logo {
  width: 18px;
  height: 18px;
}

.fallback-logo-icon {
  font-size: 0.95rem;
}

.store-logo-steam {
  background: radial-gradient(circle at top left, rgba(102, 192, 244, 0.25), rgba(23, 26, 38, 0.8));
  color: #66c0f4;
  border: 1px solid rgba(102, 192, 244, 0.35);
  box-shadow: 0 2px 8px rgba(102, 192, 244, 0.15);
}

.store-logo-steam:hover {
  border-color: rgba(102, 192, 244, 0.7);
  box-shadow: 0 4px 14px rgba(102, 192, 244, 0.35);
  color: #ffffff;
}

.store-logo-epic {
  background: radial-gradient(circle at top left, rgba(255, 255, 255, 0.2), rgba(30, 30, 30, 0.8));
  color: #f8fafc;
  border: 1px solid rgba(255, 255, 255, 0.3);
  box-shadow: 0 2px 8px rgba(255, 255, 255, 0.1);
}

.store-logo-epic:hover {
  border-color: rgba(255, 255, 255, 0.6);
  box-shadow: 0 4px 14px rgba(255, 255, 255, 0.25);
  color: #ffffff;
}

.store-logo-blizzard {
  background: radial-gradient(circle at top left, rgba(0, 174, 255, 0.25), rgba(15, 23, 42, 0.8));
  color: #00aeff;
  border: 1px solid rgba(0, 174, 255, 0.35);
  box-shadow: 0 2px 8px rgba(0, 174, 255, 0.15);
}

.store-logo-blizzard:hover {
  border-color: rgba(0, 174, 255, 0.7);
  box-shadow: 0 4px 14px rgba(0, 174, 255, 0.35);
  color: #ffffff;
}

.store-logo-riot {
  background: radial-gradient(circle at top left, rgba(235, 0, 41, 0.25), rgba(20, 10, 15, 0.8));
  color: #eb0029;
  border: 1px solid rgba(235, 0, 41, 0.35);
  box-shadow: 0 2px 8px rgba(235, 0, 41, 0.15);
}

.store-logo-riot:hover {
  border-color: rgba(235, 0, 41, 0.7);
  box-shadow: 0 4px 14px rgba(235, 0, 41, 0.35);
  color: #ff4d6d;
}

.store-logo-xbox {
  background: radial-gradient(circle at top left, rgba(44, 162, 67, 0.25), rgba(10, 25, 15, 0.8));
  color: #2ca243;
  border: 1px solid rgba(44, 162, 67, 0.35);
  box-shadow: 0 2px 8px rgba(44, 162, 67, 0.15);
}

.store-logo-xbox:hover {
  border-color: rgba(44, 162, 67, 0.7);
  box-shadow: 0 4px 14px rgba(44, 162, 67, 0.35);
  color: #4ade80;
}

.store-logo-generic {
  background: rgba(148, 163, 184, 0.15);
  color: #94a3b8;
  border: 1px solid rgba(148, 163, 184, 0.3);
}

.game-cell {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
}

.game-title {
  color: var(--text-primary);
  font-size: 0.88rem;
}

.platform-tag {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  background: rgba(15, 23, 42, 0.85);
  border: 1px solid var(--border);
  color: var(--text-secondary);
  font-size: 0.82rem;
  border-radius: 6px;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  cursor: default;
}

.platform-tag:hover {
  color: var(--text-primary);
  border-color: rgba(255, 255, 255, 0.3);
  background: rgba(30, 41, 59, 0.95);
  transform: translateY(-1px);
}

.depot-link {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  text-decoration: none;
  font-weight: 600;
  transition: color 0.15s ease;
}

.steamdb-link {
  color: #38bdf8;
}

.steamdb-link:hover {
  color: #7dd3fc;
  text-decoration: underline;
}

.egdata-link {
  color: #c084fc;
}

.egdata-link:hover {
  color: #e9d5ff;
  text-decoration: underline;
}

.external-icon {
  font-size: 0.65rem;
  opacity: 0.75;
}

.depot-size {
  font-family: var(--font-mono, monospace);
  font-weight: 600;
  color: var(--text-secondary);
}

.depots-container {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
  align-items: center;
}

.size-cell {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
}

.chunk-badge {
  font-size: 0.72rem;
  padding: 0.15rem 0.45rem;
  border-radius: 9999px;
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: var(--text-secondary);
  font-family: var(--font-mono, monospace);
}
</style>

