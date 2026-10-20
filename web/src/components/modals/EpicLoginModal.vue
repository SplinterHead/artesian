<template>
  <BaseModal 
    :model-value="modelValue" 
    title="Connect Epic Games Account" 
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <div class="epic-login-container">
      <div class="epic-step-card mb-3">
        <div class="step-num">1</div>
        <div class="step-content">
          <div class="step-title">Sign in to your Epic Games account</div>
          <p class="text-xs text-muted mt-1">
            Open the official Epic Games authorization page in a new browser tab and log into your account.
          </p>
          <a 
            :href="loginUrl" 
            target="_blank" 
            rel="noopener noreferrer" 
            class="btn btn-sm btn-outline-primary mt-2 launch-link"
          >
            <i class="fa-solid fa-arrow-up-right-from-square mr-1"></i> Open Epic Games Sign-In
          </a>
        </div>
      </div>

      <div class="epic-step-card mb-3">
        <div class="step-num">2</div>
        <div class="step-content">
          <div class="step-title">Paste the authorization code or JSON</div>
          <p class="text-xs text-muted mt-1">
            After logging in, Epic will display a page with your <code>authorizationCode</code>. Paste the code or the full JSON response below.
          </p>
          <textarea 
            class="form-control mt-2 code-textarea" 
            rows="3"
            v-model="authCodeInput" 
            placeholder='Paste code here (e.g. 859f518a4a2b4b4bb2e8f1dd63... or {"authorizationCode":"..."})'
            :disabled="loading"
            @keyup.enter.ctrl="handleSubmit"
          ></textarea>
        </div>
      </div>

      <div v-if="errorMsg" class="alert alert-danger text-xs mt-2">
        <i class="fa-solid fa-triangle-exclamation mr-1"></i> {{ errorMsg }}
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
        class="btn btn-primary btn-epic-connect" 
        :disabled="loading || !authCodeInput.trim()" 
        @click="handleSubmit"
      >
        <template v-if="loading">
          <i class="fa-solid fa-circle-notch fa-spin mr-2"></i>
          <span>Authenticating...</span>
        </template>
        <template v-else>
          <span>Connect Epic Account</span>
        </template>
      </button>
    </template>
  </BaseModal>
</template>

<script setup>
import { ref, watch } from 'vue';
import BaseModal from '../common/BaseModal.vue';

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false,
  },
  loading: {
    type: Boolean,
    default: false,
  },
  errorMsg: {
    type: String,
    default: '',
  },
});

const emit = defineEmits(['update:modelValue', 'submit']);

const loginUrl = 'https://www.epicgames.com/id/login?redirectUrl=https%3A%2F%2Fwww.epicgames.com%2Fid%2Fapi%2Fredirect%3FclientId%3D34a02cf8f4414e29b15921876da36f9a%26responseType%3Dcode';

const authCodeInput = ref('');

watch(() => props.modelValue, (val) => {
  if (val) {
    authCodeInput.value = '';
  }
});

function handleSubmit() {
  if (props.loading || !authCodeInput.value.trim()) return;
  emit('submit', { code: authCodeInput.value.trim() });
}
</script>

<style scoped>
.epic-login-container {
  display: flex;
  flex-direction: column;
}

.epic-step-card {
  display: flex;
  gap: 0.75rem;
  padding: 0.75rem;
  border-radius: var(--radius-md);
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid var(--border-color);
}

.step-num {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--color-primary);
  color: #fff;
  font-weight: 700;
  font-size: 0.75rem;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  margin-top: 2px;
}

.step-content {
  flex: 1;
}

.step-title {
  font-size: 0.85rem;
  font-weight: 600;
  color: var(--text-color);
}

.launch-link {
  display: inline-flex;
  align-items: center;
  font-weight: 600;
}

.code-textarea {
  font-family: monospace;
  font-size: 0.8rem;
  resize: vertical;
}

.btn-epic-connect {
  min-width: 180px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
}
</style>
