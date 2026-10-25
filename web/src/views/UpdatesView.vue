<template>
  <div class="tab-view">
    <!-- 1. Cron Timers Section (On Top) -->
    <div class="card mb-4">
      <div class="card-header">
        <div>
          <h3>Cron Timers</h3>
          <p class="section-subtitle">
            Automated background processes stored in SQLite and managed in real-time.
          </p>
        </div>
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
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rule in schedulerRules" :key="rule.id">
              <td>
                <code>{{ rule.id }}</code>
              </td>
              <td>
                <strong>{{ rule.name }}</strong>
              </td>

              <!-- Inline Editable Cron Expression Cell -->
              <td class="cron-cell" :class="{ 'is-editing': editingCronRuleId === rule.id }">
                <!-- Display Mode (Pen icon appears on hover) -->
                <div
                  v-if="editingCronRuleId !== rule.id"
                  class="cron-display-wrap"
                  @click="startInlineEdit(rule)"
                  title="Click or hover pen to edit cron expression"
                >
                  <code class="cron-code">{{ rule.cron_expression }}</code>
                  <button
                    type="button"
                    class="btn-pen-hover"
                    title="Edit cron expression"
                    @click.stop="startInlineEdit(rule)"
                  >
                    <i class="fa-solid fa-pen"></i>
                  </button>
                </div>

                <!-- Edit Mode (Input with Tick & Cross icons in the table row) -->
                <div v-else class="cron-editor-wrap">
                  <input
                    ref="inlineInputRef"
                    type="text"
                    class="form-control form-control-xs inline-cron-input"
                    v-model="inlineCronInput"
                    @keydown.enter.prevent="saveInlineEdit(rule)"
                    @keydown.esc.prevent="cancelInlineEdit"
                    placeholder="0 */10 * * * *"
                  />
                  <div class="inline-actions">
                    <button
                      type="button"
                      class="inline-btn save-btn"
                      title="Save (Enter)"
                      :disabled="!inlineCronInput.trim()"
                      @click="saveInlineEdit(rule)"
                    >
                      <i class="fa-solid fa-check"></i>
                    </button>
                    <button
                      type="button"
                      class="inline-btn cancel-btn"
                      title="Discard (Esc)"
                      @click="cancelInlineEdit"
                    >
                      <i class="fa-solid fa-xmark"></i>
                    </button>
                  </div>
                </div>
              </td>

              <td class="text-muted">{{ formatDate(rule.next_run) }}</td>
              <td>
                <span
                  class="badge clickable-badge"
                  :class="rule.enabled ? 'badge-hit' : 'badge-miss'"
                  title="Click to toggle Active/Paused"
                  @click="toggleRuleState(rule)"
                >
                  {{ rule.enabled ? 'ACTIVE' : 'PAUSED' }}
                </span>
              </td>
              <td class="text-right">
                <button class="btn btn-secondary btn-xs" @click="openEditModal(rule)">
                  <i class="fa-solid fa-clock mr-1"></i> Presets
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 2. Update Log Section (Below Cron Timers) -->
    <div class="card">
      <div class="card-header">
        <div>
          <h3>Update Log</h3>
          <p class="section-subtitle">
            Persistent log of game updates and patches discovered by Artesian for managed games.
          </p>
        </div>
        <div class="header-actions">
          <button class="btn btn-secondary btn-sm" @click="$emit('check-updates')">
            <i class="fa-solid fa-rotate mr-1"></i> Check Updates Now
          </button>
        </div>
      </div>

      <div class="table-wrap">
        <table v-if="updateLogs.length > 0" class="data-table">
          <thead>
            <tr>
              <th>Store</th>
              <th>Game Name</th>
              <th>App ID</th>
              <th>Status</th>
              <th>Discovered</th>
              <th>Details</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="log in updateLogs" :key="log.id || log.discovered_at + log.app_id">
              <td>
                <span
                  class="service-pill"
                  :class="`pill-${(log.service || 'steam').toLowerCase()}`"
                >
                  <svg
                    v-if="platformLogos[(log.service || 'steam').toLowerCase()]"
                    class="service-pill-svg"
                    :viewBox="platformLogos[(log.service || 'steam').toLowerCase()].viewBox"
                    fill="currentColor"
                  >
                    <path :d="platformLogos[(log.service || 'steam').toLowerCase()].path" />
                  </svg>
                  <i v-else class="fa-solid fa-gamepad mr-1"></i>
                  {{ formatStore(log.service || 'steam') }}
                </span>
              </td>
              <td>
                <strong>{{ log.game_title || log.name || 'Unknown Title' }}</strong>
              </td>
              <td>
                <code>{{ log.app_id }}</code>
              </td>
              <td>
                <span class="badge" :class="getStatusBadgeClass(log.status)">
                  {{ (log.status || 'Verified').toUpperCase() }}
                </span>
              </td>
              <td class="text-muted whitespace-nowrap">
                {{ formatDate(log.discovered_at || log.timestamp) }}
              </td>
              <td class="update-message-col">
                {{ log.message || 'Update check verified depot manifests.' }}
              </td>
            </tr>
          </tbody>
        </table>
        <div v-else class="empty-state p-4 text-center">
          <i class="fa-solid fa-clock-rotate-left fa-2x mb-2 text-muted"></i>
          <p class="mb-0">
            No updates discovered yet. Artesian checks managed games according to the Cron Timers
            above.
          </p>
        </div>
      </div>
    </div>

    <!-- Edit Schedule Modal -->
    <BaseModal
      v-model="showEditModal"
      :title="editingRule ? `Edit Schedule: ${editingRule.name}` : 'Edit Schedule'"
    >
      <div v-if="editingRule" class="schedule-form">
        <div class="rule-meta-banner mb-3">
          <span class="text-muted">Job Identifier:</span>
          <code>{{ editingRule.id }}</code>
        </div>

        <!-- Enabled / Paused Toggle -->
        <div class="form-group mb-3">
          <label class="form-label">Task State:</label>
          <div class="toggle-group">
            <button
              type="button"
              class="btn btn-sm"
              :class="formEnabled ? 'btn-primary' : 'btn-outline-secondary'"
              @click="formEnabled = true"
            >
              <i class="fa-solid fa-play mr-1"></i> Active (Enabled)
            </button>
            <button
              type="button"
              class="btn btn-sm"
              :class="!formEnabled ? 'btn-danger' : 'btn-outline-secondary'"
              @click="formEnabled = false"
            >
              <i class="fa-solid fa-pause mr-1"></i> Paused (Disabled)
            </button>
          </div>
        </div>

        <!-- Cadence Presets -->
        <div class="form-group mb-3">
          <label class="form-label">Cadence Preset:</label>
          <div class="presets-grid">
            <button
              v-for="preset in cadencePresets"
              :key="preset.cron"
              type="button"
              class="btn btn-xs preset-btn"
              :class="formCron === preset.cron ? 'btn-accent' : 'btn-outline-secondary'"
              @click="selectPreset(preset.cron)"
            >
              {{ preset.label }}
            </button>
          </div>
        </div>

        <!-- Direct Cron Expression Input -->
        <div class="form-group mb-3">
          <label class="form-label">Cron Expression:</label>
          <input
            type="text"
            class="form-control font-mono"
            v-model="formCron"
            placeholder="0 */10 * * * *"
          />
          <small class="form-helper text-muted">
            Format: <code>sec min hour day month dow</code> (e.g. <code>0 */10 * * * *</code> for
            every 10 min, or <code>0 0 3 * * *</code> for daily at 3 AM). Standard 5-field cron is
            also supported.
          </small>
        </div>

        <div v-if="formError" class="alert alert-danger mt-2">
          {{ formError }}
        </div>
      </div>

      <template #footer>
        <button type="button" class="btn btn-outline-secondary" @click="showEditModal = false">
          Cancel
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="!formCron.trim()"
          @click="saveRule"
        >
          <i class="fa-solid fa-check mr-1"></i> Save Schedule
        </button>
      </template>
    </BaseModal>
  </div>
</template>

<script setup>
import { ref, nextTick } from 'vue';
import { formatDate, formatStore } from '../utils/formatters';
import { platformLogos } from '../utils/logos';
import BaseModal from '../components/common/BaseModal.vue';

defineProps({
  schedulerRules: {
    type: Array,
    required: true,
  },
  updateLogs: {
    type: Array,
    default: () => [],
  },
});

const emit = defineEmits(['check-updates', 'save-rule']);

const showEditModal = ref(false);
const editingRule = ref(null);
const formCron = ref('');
const formEnabled = ref(true);
const formError = ref('');

// Table row inline editing state
const inlineInputRef = ref(null);
const editingCronRuleId = ref(null);
const inlineCronInput = ref('');

function startInlineEdit(rule) {
  editingCronRuleId.value = rule.id;
  inlineCronInput.value = rule.cron_expression || '0 */10 * * * *';
  nextTick(() => {
    if (inlineInputRef.value) {
      inlineInputRef.value.focus();
      inlineInputRef.value.select();
    }
  });
}

function cancelInlineEdit() {
  editingCronRuleId.value = null;
  inlineCronInput.value = '';
}

function saveInlineEdit(rule) {
  if (!inlineCronInput.value.trim()) return;
  emit('save-rule', {
    id: rule.id,
    cron_expression: inlineCronInput.value.trim(),
    enabled: rule.enabled,
  });
  editingCronRuleId.value = null;
}

function toggleRuleState(rule) {
  emit('save-rule', {
    id: rule.id,
    cron_expression: rule.cron_expression,
    enabled: !rule.enabled,
  });
}

const cadencePresets = [
  { label: 'Every 5 min', cron: '0 */5 * * * *' },
  { label: 'Every 10 min', cron: '0 */10 * * * *' },
  { label: 'Every 15 min', cron: '0 */15 * * * *' },
  { label: 'Every 30 min', cron: '0 */30 * * * *' },
  { label: 'Every hour', cron: '0 0 * * * *' },
  { label: 'Every 4 hours', cron: '0 0 */4 * * *' },
  { label: 'Daily at 3 AM', cron: '0 0 3 * * *' },
];

function openEditModal(rule) {
  editingRule.value = rule;
  formCron.value = rule.cron_expression || '0 */10 * * * *';
  formEnabled.value = rule.enabled !== false;
  formError.value = '';
  showEditModal.value = true;
}

function selectPreset(cronExpr) {
  formCron.value = cronExpr;
}

function saveRule() {
  if (!formCron.value.trim()) {
    formError.value = 'Cron expression cannot be empty.';
    return;
  }
  emit('save-rule', {
    id: editingRule.value.id,
    cron_expression: formCron.value.trim(),
    enabled: formEnabled.value,
  });
  showEditModal.value = false;
}

function getStatusBadgeClass(status) {
  const st = (status || '').toLowerCase();
  if (st === 'synced' || st === 'verified' || st === 'up-to-date') {
    return 'badge-hit';
  }
  if (st === 'discovered' || st === 'prefilling' || st === 'downloading') {
    return 'badge-prefill';
  }
  return 'badge-miss';
}
</script>

<style scoped>
.section-subtitle {
  font-size: 0.8rem;
  color: var(--text-muted);
  margin-top: 0.2rem;
}

.update-message-col {
  font-size: 0.85rem;
  color: var(--text-secondary);
  max-width: 420px;
}

.whitespace-nowrap {
  white-space: nowrap;
}

.empty-state {
  color: var(--text-muted);
  font-size: 0.85rem;
  padding: 2.5rem 1rem;
}

.rule-meta-banner {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  background: rgba(255, 255, 255, 0.03);
  padding: 0.5rem 0.75rem;
  border-radius: 6px;
  border: 1px solid var(--border);
}

.form-label {
  display: block;
  font-size: 0.85rem;
  font-weight: 600;
  color: var(--text-secondary);
  margin-bottom: 0.4rem;
}

.toggle-group {
  display: flex;
  gap: 0.5rem;
}

.presets-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
}

.preset-btn {
  font-size: 0.75rem;
  padding: 0.25rem 0.6rem;
}

.font-mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.form-helper {
  display: block;
  font-size: 0.75rem;
  margin-top: 0.35rem;
  line-height: 1.4;
}

.alert-danger {
  background: rgba(239, 68, 68, 0.1);
  border: 1px solid rgba(239, 68, 68, 0.3);
  color: #ef4444;
  padding: 0.5rem 0.75rem;
  border-radius: 6px;
  font-size: 0.85rem;
}

.btn-xs {
  font-size: 0.75rem;
  padding: 0.2rem 0.5rem;
  border-radius: 4px;
}

.service-pill {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  font-size: 0.75rem;
  padding: 0.2rem 0.55rem;
  border-radius: 4px;
  font-weight: 600;
}

.service-pill-svg {
  width: 12px;
  height: 12px;
  flex-shrink: 0;
  display: inline-block;
}

.pill-steam {
  background: rgba(23, 26, 33, 0.85);
  color: #66c0f4;
  border: 1px solid rgba(102, 192, 244, 0.35);
  box-shadow: 0 1px 4px rgba(102, 192, 244, 0.15);
}

.pill-xbox {
  background: rgba(16, 124, 16, 0.15);
  color: #107c10;
  border: 1px solid rgba(16, 124, 16, 0.35);
  box-shadow: 0 1px 4px rgba(16, 124, 16, 0.15);
}

/* Inline Editable Cron Cell Styles */
.cron-cell {
  position: relative;
  min-width: 220px;
}

.cron-display-wrap {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.2rem 0.4rem;
  border-radius: 6px;
  cursor: pointer;
  transition:
    background 0.15s ease,
    border-color 0.15s ease;
  border: 1px solid transparent;
}

.cron-display-wrap:hover {
  background: rgba(255, 255, 255, 0.05);
  border-color: rgba(255, 255, 255, 0.12);
}

.cron-code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.85rem;
  color: var(--accent);
}

.btn-pen-hover {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  background: transparent;
  border: none;
  color: var(--text-muted);
  border-radius: 4px;
  cursor: pointer;
  opacity: 0;
  transform: scale(0.85);
  transition:
    opacity 0.15s ease,
    transform 0.15s ease,
    color 0.15s ease,
    background 0.15s ease;
}

.cron-display-wrap:hover .btn-pen-hover {
  opacity: 1;
  transform: scale(1);
}

.btn-pen-hover:hover {
  color: var(--accent);
  background: rgba(59, 130, 246, 0.15);
}

.cron-editor-wrap {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
}

.inline-cron-input {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.85rem;
  padding: 0.2rem 0.5rem;
  width: 145px;
  height: 28px;
  border-radius: 5px;
  background: var(--bg-surface);
  border: 1px solid var(--accent);
  color: var(--text-primary);
  outline: none;
  box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2);
}

.inline-actions {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
}

.inline-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border-radius: 5px;
  border: 1px solid transparent;
  cursor: pointer;
  font-size: 0.8rem;
  transition: all 0.15s ease;
}

.inline-btn.save-btn {
  background: rgba(34, 197, 94, 0.15);
  border-color: rgba(34, 197, 94, 0.4);
  color: #22c55e;
}

.inline-btn.save-btn:hover:not(:disabled) {
  background: #22c55e;
  color: #ffffff;
  box-shadow: 0 0 8px rgba(34, 197, 94, 0.4);
}

.inline-btn.save-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.inline-btn.cancel-btn {
  background: rgba(239, 68, 68, 0.15);
  border-color: rgba(239, 68, 68, 0.4);
  color: #ef4444;
}

.inline-btn.cancel-btn:hover {
  background: #ef4444;
  color: #ffffff;
  box-shadow: 0 0 8px rgba(239, 68, 68, 0.4);
}

.clickable-badge {
  cursor: pointer;
  user-select: none;
  transition:
    transform 0.15s ease,
    opacity 0.15s ease;
}

.clickable-badge:hover {
  transform: scale(1.05);
  opacity: 0.9;
}
</style>
