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
          <span class="live-pulse"></span>
          <span class="text-muted text-sm">{{ liveTraffic.length }} requests logged</span>
          <button class="btn btn-xs btn-secondary" @click="$emit('clear-traffic')">Clear</button>
        </div>
      </div>

      <div class="table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>Timestamp</th>
              <th>Client IP</th>
              <th>Method</th>
              <th>Host</th>
              <th>Request URI / Chunk</th>
              <th>Transfer Size</th>
              <th>Cache State</th>
              <th>Latency</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="liveTraffic.length === 0">
              <td colspan="8" class="empty-cell">
                Listening on :80. Traffic from game clients will stream here in real time...
              </td>
            </tr>
            <tr v-for="(t, idx) in liveTraffic" :key="idx" class="traffic-row">
              <td class="text-muted"><code>{{ t.time }}</code></td>
              <td><code>{{ t.client_ip }}</code></td>
              <td><span class="method-tag">{{ t.method }}</span></td>
              <td><strong>{{ t.host }}</strong></td>
              <td class="uri-cell" :title="t.url"><code>{{ t.url }}</code></td>
              <td>{{ formatBytes(t.bytes_transferred) }}</td>
              <td>
                <span :class="['cache-badge', t.is_hit ? 'badge-hit' : 'badge-miss']">
                  {{ t.is_hit ? 'HIT' : 'MISS' }}
                </span>
              </td>
              <td class="text-muted">{{ t.duration_ms }}ms</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
import { formatBytes, formatSpeed } from '../utils/formatters';

defineProps({
  stats: {
    type: Object,
    required: true,
  },
  liveTraffic: {
    type: Array,
    required: true,
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

.method-tag {
  background: #0f172a;
  border: 1px solid var(--border);
  padding: 2px 6px;
  border-radius: 4px;
  font-weight: 600;
  font-size: 0.75rem;
}

.uri-cell {
  max-width: 320px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.cache-badge {
  font-weight: 700;
  font-size: 0.75rem;
  padding: 2px 6px;
  border-radius: 4px;
}
</style>
