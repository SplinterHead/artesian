<template>
  <BaseModal
    :model-value="!!service"
    :title="service ? `Connect ${service.display_name} Account` : ''"
    @update:model-value="$emit('close')"
  >
    <div v-if="service" class="connect-service-body">
      <div class="service-preview mb-3">
        <div class="service-icon-box" :class="'store-bg-' + service.id">
          <svg 
            v-if="platformLogos[service.id]" 
            class="platform-svg-logo" 
            :viewBox="platformLogos[service.id].viewBox" 
            fill="currentColor"
          >
            <path :d="platformLogos[service.id].path" />
          </svg>
          <i v-else class="fa-solid fa-gamepad"></i>
        </div>
        <div>
          <strong>{{ service.display_name }}</strong>
        </div>
      </div>

      <div class="form-group mb-3">
        <label>Username / Account ID:</label>
        <input 
          type="text" 
          class="form-control" 
          v-model="username" 
          placeholder="Username"
          :disabled="loading"
          @keyup.enter="handleSubmit"
        />
      </div>

      <div class="form-group mb-2">
        <label>API Key / Token (Optional):</label>
        <input 
          type="password" 
          class="form-control" 
          v-model="token" 
          placeholder="Optional OAuth or API Token"
          :disabled="loading"
          @keyup.enter="handleSubmit"
        />
      </div>
    </div>

    <template #footer>
      <button class="btn btn-outline-secondary" @click="$emit('close')">Cancel</button>
      <button 
        class="btn btn-primary" 
        :disabled="!username.trim() || loading" 
        @click="handleSubmit"
      >
        <i v-if="loading" class="fa-solid fa-circle-notch fa-spin mr-1"></i>
        Connect Account
      </button>
    </template>
  </BaseModal>
</template>

<script setup>
import { ref, watch } from 'vue';
import BaseModal from '../common/BaseModal.vue';
import { platformLogos } from '../../utils/logos';

const props = defineProps({
  service: {
    type: Object,
    default: null,
  },
  loading: {
    type: Boolean,
    default: false,
  },
});

const emit = defineEmits(['close', 'submit']);

const username = ref('');
const token = ref('');

watch(() => props.service, (svc) => {
  if (svc) {
    username.value = svc.username || '';
    token.value = '';
  }
});

function handleSubmit() {
  if (!username.value.trim() || props.loading) return;
  emit('submit', {
    service: props.service.id,
    username: username.value.trim(),
    token: token.value.trim(),
  });
}
</script>

<style scoped>
.connect-service-body {
  margin-top: 0.5rem;
}

.service-preview {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 8px;
}

.service-icon-box {
  width: 36px;
  height: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #1e293b;
  border-radius: 8px;
  flex-shrink: 0;
}

.platform-svg-logo {
  width: 20px;
  height: 20px;
}

.store-bg-steam { color: #66c0f4; }
.store-bg-epic { color: #ffffff; }
.store-bg-blizzard { color: #00aeff; }
.store-bg-riot { color: #d13639; }
.store-bg-xbox { color: #107c10; }
</style>
