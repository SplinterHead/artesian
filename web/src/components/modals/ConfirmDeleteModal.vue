<template>
  <BaseModal
    :model-value="modelValue"
    title="Delete Game & Purge Cache"
    :show-close="!loading"
    :close-on-backdrop="!loading"
    @update:model-value="!loading && $emit('update:modelValue', $event)"
  >
    <div v-if="game" class="confirm-delete-body">
      <div class="warning-banner">
        <div class="warning-icon-box">
          <i class="fa-solid fa-triangle-exclamation"></i>
        </div>
        <div class="warning-text">
          <div class="warning-title">Are you sure?</div>
          <div class="warning-subtitle">
            The files for this game will be removed and no longer cached.
          </div>
        </div>
      </div>

      <div class="game-info-card">
        <div class="game-store-badge">
          <span class="store-badge-label">{{ (game.service || 'steam').toUpperCase() }}</span>
          <span v-if="game.app_id" class="app-id-label">App ID: {{ game.app_id }}</span>
        </div>
        <div class="game-name">{{ game.name }}</div>
        <div v-if="gameSize > 0" class="game-size-row">
          <i class="fa-solid fa-hard-drive mr-1"></i>
          <span>Cache size to delete: <strong>{{ formatBytes(gameSize) }}</strong></span>
        </div>
      </div>

      <div class="warning-message-box">
        <p class="mb-1 text-danger-message">
          <i class="fa-solid fa-circle-exclamation mr-1"></i>
          All associated chunks and manifests on disk will be deleted.
        </p>
        <p class="mb-0 text-muted text-xs">
          This game will no longer be cached on your local network. Any active downloads or prefill processes will be cancelled immediately.
        </p>
      </div>
    </div>

    <template #footer>
      <button 
        type="button" 
        class="btn btn-outline-secondary" 
        :disabled="loading" 
        @click="$emit('update:modelValue', false)"
      >
        Cancel
      </button>
      <button 
        type="button" 
        class="btn btn-danger btn-confirm-delete" 
        :disabled="loading" 
        @click="$emit('confirm')"
      >
        <template v-if="loading">
          <i class="fa-solid fa-circle-notch fa-spin mr-2"></i>
          <span>Removing files...</span>
        </template>
        <template v-else>
          <i class="fa-solid fa-trash mr-2"></i>
          <span>Delete & Purge Cache</span>
        </template>
      </button>
    </template>
  </BaseModal>
</template>

<script setup>
import { computed } from 'vue';
import BaseModal from '../common/BaseModal.vue';
import { formatBytes } from '../../utils/formatters';

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false,
  },
  game: {
    type: Object,
    default: null,
  },
  loading: {
    type: Boolean,
    default: false,
  },
});

defineEmits(['update:modelValue', 'confirm']);

const gameSize = computed(() => {
  if (!props.game) return 0;
  return props.game.size_bytes || props.game.cached_bytes || 0;
});
</script>

<style scoped>
.confirm-delete-body {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.warning-banner {
  display: flex;
  align-items: center;
  gap: 0.85rem;
  padding: 0.85rem 1rem;
  background: rgba(239, 68, 68, 0.1);
  border: 1px solid rgba(239, 68, 68, 0.25);
  border-radius: 8px;
}

.warning-icon-box {
  width: 38px;
  height: 38px;
  border-radius: 8px;
  background: rgba(239, 68, 68, 0.2);
  color: #ef4444;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 1.25rem;
  flex-shrink: 0;
}

.warning-title {
  font-weight: 700;
  font-size: 1.05rem;
  color: #f87171;
}

.warning-subtitle {
  font-size: 0.825rem;
  color: var(--text-muted);
}

.game-info-card {
  padding: 0.85rem 1rem;
  background: var(--bg-surface, #1e293b);
  border: 1px solid var(--border);
  border-radius: 8px;
}

.game-store-badge {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.75rem;
  margin-bottom: 0.35rem;
}

.store-badge-label {
  background: rgba(56, 189, 248, 0.15);
  color: var(--accent);
  padding: 2px 6px;
  border-radius: 4px;
  font-weight: 600;
}

.app-id-label {
  color: var(--text-muted);
  font-family: monospace;
}

.game-name {
  font-size: 1.1rem;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 0.35rem;
}

.game-size-row {
  font-size: 0.85rem;
  color: var(--text-muted);
}

.game-size-row strong {
  color: var(--text-primary);
}

.warning-message-box {
  padding: 0.75rem 1rem;
  background: rgba(0, 0, 0, 0.2);
  border-left: 3px solid #ef4444;
  border-radius: 4px;
  font-size: 0.85rem;
}

.text-danger-message {
  color: #f87171;
  font-weight: 500;
}

.btn-confirm-delete {
  min-width: 195px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
</style>
