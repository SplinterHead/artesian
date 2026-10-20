<template>
  <div class="tab-view">
    <!-- Metric Cards -->
    <div class="stats-grid">
      <div class="stat-card">
        <span class="stat-label">Cache Hit Ratio</span>
        <div class="stat-value text-accent">
          {{ stats.hit_ratio ? stats.hit_ratio.toFixed(1) : 0 }}%
        </div>
        <span class="stat-sub"
          >{{ stats.cache_hits }} hits / {{ stats.total_requests }} requests</span
        >
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
        <table class="data-table active-downloads-table">
          <thead>
            <tr>
              <th class="col-time"></th>
              <th class="col-client">Client</th>
              <th class="col-store">Store</th>
              <th class="col-game">Game</th>
              <th class="col-depot">Depot ID</th>
              <th class="col-size">Size</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="activeDownloads.length === 0">
              <td colspan="6" class="empty-cell">
                Listening on :80. Traffic from game clients will bundle here in real time...
              </td>
            </tr>
            <tr
              v-for="d in activeDownloads"
              :key="d.key || d.client_ip + d.store + d.game_title"
              class="traffic-row"
            >
              <td
                class="col-time text-muted"
                :title="
                  'Initial chunk: ' +
                  (d.first_seen ? new Date(d.first_seen).toLocaleString() : d.timestamp || d.time)
                "
              >
                <code>{{ d.timestamp || d.time }}</code>
              </td>
              <td class="col-client">
                <span
                  v-if="cleanClientIP(d.client_ip) === 'Artesian'"
                  class="client-badge-artesian"
                  title="Internal Artesian Depot Caching Engine"
                >
                  <i class="fa-solid fa-server mr-1"></i> Artesian
                </span>
                <code v-else class="ip-code">{{ cleanClientIP(d.client_ip) }}</code>
              </td>
              <td class="col-store">
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
              <td class="col-game">
                <div class="game-cell">
                  <strong class="game-title">{{ d.game_title || 'Game Asset' }}</strong>
                  <span v-if="d.platform" class="platform-tags-wrap">
                    <span
                      v-for="p in getPlatformList(d.platform)"
                      :key="p"
                      class="platform-tag"
                      :title="p"
                    >
                      <i :class="getPlatformIcon(p)"></i>
                    </span>
                  </span>
                </div>
              </td>
              <td class="col-depot">
                <div class="depots-container">
                  <!-- Steam Depots: All depots for this game bundled as clickable badges -->
                  <template v-if="d.depot_links && d.depot_links.length > 0">
                    <a
                      v-for="link in d.depot_links"
                      :key="link.depot_id"
                      :href="link.lookup_url || 'https://steamdb.info/depot/' + link.depot_id"
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
                    :href="d.lookup_url || 'https://steamdb.info/depot/' + d.depot_id"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="depot-link steamdb-link"
                    :title="'View Depot ' + d.depot_id + ' on SteamDB'"
                  >
                    <code>{{ d.depot_id }}</code>
                    <i class="fa-solid fa-arrow-up-right-from-square external-icon"></i>
                  </a>
                  <span v-else class="text-muted">—</span>
                </div>
              </td>
              <td class="col-size">
                <div class="size-cell">
                  <div class="size-progress-wrap">
                    <div class="size-header-row">
                      <span class="depot-size">
                        <template
                          v-if="d.total_depot_size && d.bytes_transferred < d.total_depot_size"
                        >
                          {{ formatBytes(d.bytes_transferred) }} /
                          {{ formatBytes(d.total_depot_size) }}
                        </template>
                        <template v-else>
                          {{ formatBytes(d.total_depot_size || d.bytes_transferred || 0) }}
                        </template>
                      </span>
                      <span
                        v-if="d.total_depot_size && d.total_depot_size > 0"
                        class="pct-badge"
                        :class="{ 'pct-done': d.bytes_transferred >= d.total_depot_size }"
                      >
                        {{
                          Math.min(
                            100,
                            Math.round((d.bytes_transferred / d.total_depot_size) * 100)
                          )
                        }}%
                      </span>
                      <span v-else-if="d.chunk_count && d.chunk_count > 1" class="chunk-badge">
                        {{ d.chunk_count }} files
                      </span>
                    </div>
                    <div
                      v-if="d.total_depot_size && d.total_depot_size > 0"
                      class="download-mini-progress"
                    >
                      <div
                        class="download-mini-progress-fill"
                        :style="{
                          width:
                            Math.min(100, (d.bytes_transferred / d.total_depot_size) * 100) + '%',
                        }"
                      ></div>
                    </div>
                  </div>
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
  formatStore,
  getLogoKey,
  getPlatformIcon,
  getPlatformList,
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

.platform-tags-wrap {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
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

.external-icon {
  font-size: 0.65rem;
  opacity: 0.75;
}

.depots-container {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
  align-items: center;
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

.client-badge-artesian {
  background: rgba(56, 189, 248, 0.15);
  color: #38bdf8;
  border: 1px solid rgba(56, 189, 248, 0.3);
  font-weight: 700;
  font-family: var(--font-mono, monospace);
  font-size: 0.8rem;
  padding: 0.2rem 0.55rem;
  border-radius: 4px;
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  letter-spacing: 0.02em;
}

.active-downloads-table {
  width: 100%;
}

.active-downloads-table .col-time {
  width: 95px;
  white-space: nowrap;
}

.active-downloads-table .col-client {
  width: 140px;
  white-space: nowrap;
}

.active-downloads-table .col-store {
  width: 60px;
  text-align: center;
}

.active-downloads-table .col-game {
  min-width: 180px;
}

.active-downloads-table .col-depot {
  min-width: 150px;
}

.active-downloads-table .col-size {
  width: 270px;
}

.size-cell {
  display: flex;
  align-items: center;
}

.size-progress-wrap {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  width: 250px;
}

.size-header-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}

.depot-size {
  font-family: var(--font-mono, monospace);
  font-variant-numeric: tabular-nums;
  font-weight: 600;
  font-size: 0.8rem;
  color: var(--text-secondary);
  white-space: nowrap;
}

.pct-badge {
  font-size: 0.72rem;
  font-family: var(--font-mono, monospace);
  font-variant-numeric: tabular-nums;
  font-weight: 700;
  color: #38bdf8;
  background: rgba(56, 189, 248, 0.15);
  padding: 0.1rem 0.4rem;
  border-radius: 4px;
  min-width: 42px;
  text-align: center;
  display: inline-block;
  flex-shrink: 0;
}

.pct-badge.pct-done {
  color: #22c55e;
  background: rgba(34, 197, 94, 0.15);
}

.download-mini-progress {
  width: 100%;
  height: 5px;
  background: rgba(255, 255, 255, 0.08);
  border-radius: 999px;
  overflow: hidden;
}

.download-mini-progress-fill {
  height: 100%;
  background: linear-gradient(90deg, #38bdf8, #818cf8);
  border-radius: 999px;
  transition: width 0.2s ease-out;
}
</style>
