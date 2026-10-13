<template>
  <div class="tab-view">
    <div class="card mb-4">
      <div class="card-header">
        <h3>Steam PICS Change Detection</h3>
        <div class="header-actions">
          <button class="btn btn-secondary btn-sm" @click="$emit('poll-pics')">
            <i class="fa-solid fa-rotate mr-1"></i> Check PICS Now
          </button>
          <button class="btn btn-accent btn-sm" @click="$emit('trigger-simulated-pics')">
            ⚡ Simulate PICS Update (CS2)
          </button>
        </div>
      </div>

      <div class="pics-events-feed">
        <h4 class="mb-2">Recent PICS Events</h4>
        <div v-if="picsEvents.length === 0" class="empty-state">
          No PICS changes detected yet. Automatic polling runs according to the cron schedule below.
        </div>
        <div v-for="(evt, i) in picsEvents" :key="i" class="pics-event-item">
          <span class="pics-event-time">{{ evt.time }}</span>
          <span class="pics-event-title">{{ evt.title }}</span>
          <p class="pics-event-desc">{{ evt.message }}</p>
        </div>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <h3>Configured Cron Timers (robfig/cron)</h3>
      </div>

      <div class="table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>Job ID</th>
              <th>Task Name</th>
              <th>Cron Expression</th>
              <th>Next Execution</th>
              <th>State</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rule in schedulerRules" :key="rule.id">
              <td><code>{{ rule.id }}</code></td>
              <td><strong>{{ rule.name }}</strong></td>
              <td><code>{{ rule.cron_expression }}</code></td>
              <td class="text-muted">{{ formatDate(rule.next_run) }}</td>
              <td>
                <span class="badge" :class="rule.enabled ? 'badge-hit' : 'badge-miss'">
                  {{ rule.enabled ? 'ACTIVE' : 'PAUSED' }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
import { formatDate } from '../utils/formatters';

defineProps({
  picsEvents: {
    type: Array,
    required: true,
  },
  schedulerRules: {
    type: Array,
    required: true,
  },
});

defineEmits(['poll-pics', 'trigger-simulated-pics']);
</script>

<style scoped>
.pics-events-feed {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 1rem;
}

.pics-event-item {
  padding: 0.75rem 0;
  border-bottom: 1px solid var(--border);
}

.pics-event-item:last-child {
  border-bottom: none;
}

.pics-event-time {
  font-size: 0.75rem;
  color: var(--text-muted);
  margin-right: 0.5rem;
}

.pics-event-title {
  font-weight: 600;
  color: var(--accent);
  font-size: 0.9rem;
}

.pics-event-desc {
  font-size: 0.85rem;
  color: var(--text-secondary);
  margin-top: 0.2rem;
}

.empty-state {
  color: var(--text-muted);
  font-size: 0.85rem;
  padding: 1rem 0;
}
</style>
