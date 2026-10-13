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

    <!-- Library Games Table -->
    <div class="card">
      <div class="card-header">
        <h3>Game Library</h3>
        <button class="btn btn-primary btn-sm" @click="$emit('open-add-game')">
          + Add Game
        </button>
      </div>

      <div class="table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>App ID</th>
              <th>Game Name</th>
              <th>Status</th>
              <th>Auto-Update</th>
              <th>Last Sync</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="game in library" :key="game.app_id">
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
                <button 
                  class="btn btn-xs btn-primary"
                  :disabled="activePrefillJob && activePrefillJob.status === 'downloading'"
                  @click="$emit('trigger-prefill', game)"
                >
                  ⚡ Initialize
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
import { formatETA, formatDate } from '../utils/formatters';

defineProps({
  library: {
    type: Array,
    required: true,
  },
  activePrefillJob: {
    type: Object,
    default: null,
  },
});

defineEmits(['open-add-game', 'trigger-prefill']);
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
</style>
