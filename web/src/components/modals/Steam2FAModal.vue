<template>
  <BaseModal 
    :model-value="modelValue" 
    title="Steam Guard Authentication" 
    :show-close="false"
    :close-on-backdrop="false"
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <div class="steam-auth-header mb-3">
      <div class="account-badge">
        <i class="fa-brands fa-steam mr-1"></i>
        <span>Account: <strong>{{ username }}</strong></span>
      </div>
    </div>

    <!-- Mobile Push Notification Card (when mobile confirmation is available) -->
    <div v-if="hasMobileConfirmation" class="mobile-approval-card mb-3">
      <div class="approval-icon-col">
        <div class="mobile-phone-ring">
          <i class="fa-solid fa-mobile-screen-button mobile-icon"></i>
          <span class="ping-ring"></span>
        </div>
      </div>
      <div class="approval-text-col">
        <h4 class="approval-title">Steam Mobile App Confirmation</h4>
        <p class="approval-desc">
          Open your <strong>Steam Mobile App</strong> and tap <strong>Approve</strong> on the sign-in prompt to log in automatically.
        </p>
        <div class="approval-status-pill">
          <i class="fa-solid fa-circle-notch fa-spin mr-2"></i>
          <span>Waiting for mobile approval...</span>
        </div>
      </div>
    </div>

    <!-- Email Notice Card (when email verification is required and no mobile) -->
    <div v-else-if="hasEmailCode" class="email-notice-card mb-3">
      <div class="email-icon-box">
        <i class="fa-solid fa-envelope"></i>
      </div>
      <div class="email-text-box">
        <h4>Check your email</h4>
        <p>A Steam Guard verification code has been sent to your registered email address.</p>
      </div>
    </div>

    <!-- Divider when mobile approval is enabled -->
    <div v-if="hasMobileConfirmation" class="auth-divider">
      <span class="divider-line"></span>
      <span class="divider-text">OR ENTER CODE MANUALLY</span>
      <span class="divider-line"></span>
    </div>

    <div class="form-group mb-3">
      <label class="code-label">
        {{ hasEmailCode && !hasMobileConfirmation ? 'Steam Guard Email Code:' : 'Steam Guard Authenticator Code:' }}
      </label>
      <input 
        type="text" 
        class="form-control text-center code-input uppercase-input" 
        :value="code"
        :disabled="loading"
        @input="handleInput"
        placeholder="e.g. 5R7X9" 
        maxlength="10"
        autofocus
        @keyup.enter="handleSubmit"
      />
    </div>

    <div class="mt-2 text-center">
      <button 
        type="button" 
        class="switch-account-btn" 
        :disabled="loading"
        @click="$emit('switch-account')"
      >
        <i class="fa-solid fa-arrow-rotate-left mr-1"></i> Different account? Restart login
      </button>
    </div>

    <template #footer>
      <button 
        type="button" 
        class="btn btn-outline-secondary" 
        :disabled="loading"
        @click="$emit('dismiss')"
      >
        Dismiss
      </button>
      <button 
        type="button" 
        class="btn btn-primary" 
        :disabled="!code.trim() || loading" 
        @click="handleSubmit"
      >
        <span v-if="loading" class="btn-spinner-content">
          <i class="fa-solid fa-circle-notch fa-spin mr-2"></i> Verifying...
        </span>
        <span v-else>
          Verify Code
        </span>
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
  username: {
    type: String,
    default: '',
  },
  loading: {
    type: Boolean,
    default: false,
  },
  hasMobileConfirmation: {
    type: Boolean,
    default: true,
  },
  hasEmailCode: {
    type: Boolean,
    default: false,
  },
});

const emit = defineEmits(['update:modelValue', 'submit', 'dismiss', 'switch-account']);

const code = ref('');

watch(() => props.modelValue, (val) => {
  if (val) {
    code.value = '';
  }
});

function handleInput(event) {
  code.value = (event.target.value || '').toUpperCase();
}

function handleSubmit() {
  if (props.loading) return;
  const cleanCode = code.value.trim().toUpperCase();
  if (!cleanCode) return;
  emit('submit', cleanCode);
}
</script>

<style scoped>
.steam-auth-header {
  display: flex;
  justify-content: center;
}

.account-badge {
  display: inline-flex;
  align-items: center;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 4px 12px;
  font-size: 0.85rem;
  color: var(--text-secondary);
}

.mobile-approval-card {
  display: flex;
  align-items: center;
  gap: 1rem;
  background: linear-gradient(135deg, rgba(56, 189, 248, 0.1) 0%, rgba(17, 24, 39, 0.7) 100%);
  border: 1px solid rgba(56, 189, 248, 0.25);
  border-radius: 10px;
  padding: 1rem 1.15rem;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.2);
}

.approval-icon-col {
  flex-shrink: 0;
}

.mobile-phone-ring {
  position: relative;
  width: 48px;
  height: 48px;
  border-radius: 50%;
  background: rgba(56, 189, 248, 0.15);
  border: 1px solid rgba(56, 189, 248, 0.4);
  display: flex;
  align-items: center;
  justify-content: center;
}

.mobile-icon {
  font-size: 1.5rem;
  color: var(--accent);
  z-index: 2;
}

.ping-ring {
  position: absolute;
  top: -2px;
  left: -2px;
  right: -2px;
  bottom: -2px;
  border-radius: 50%;
  border: 2px solid var(--accent);
  opacity: 0.6;
  animation: radar-ping 2s cubic-bezier(0, 0, 0.2, 1) infinite;
}

@keyframes radar-ping {
  0% {
    transform: scale(0.95);
    opacity: 0.8;
  }
  70% {
    transform: scale(1.35);
    opacity: 0;
  }
  100% {
    transform: scale(1.35);
    opacity: 0;
  }
}

.approval-text-col {
  flex: 1;
}

.approval-title {
  margin: 0;
  font-size: 0.95rem;
  font-weight: 600;
  color: var(--text-primary);
}

.approval-desc {
  margin: 0.25rem 0 0.5rem 0;
  font-size: 0.8rem;
  color: var(--text-secondary);
  line-height: 1.35;
}

.approval-status-pill {
  display: inline-flex;
  align-items: center;
  background: rgba(56, 189, 248, 0.12);
  border: 1px solid rgba(56, 189, 248, 0.3);
  border-radius: 100px;
  padding: 3px 10px;
  font-size: 0.75rem;
  color: var(--accent);
  font-weight: 500;
}

.email-notice-card {
  display: flex;
  align-items: center;
  gap: 1rem;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 1rem;
}

.email-icon-box {
  width: 44px;
  height: 44px;
  border-radius: 8px;
  background: rgba(56, 189, 248, 0.1);
  color: var(--accent);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 1.3rem;
  flex-shrink: 0;
}

.email-text-box h4 {
  margin: 0 0 0.25rem 0;
  font-size: 0.95rem;
  font-weight: 600;
}

.email-text-box p {
  margin: 0;
  font-size: 0.8rem;
  color: var(--text-secondary);
}

.auth-divider {
  display: flex;
  align-items: center;
  margin: 1.25rem 0 1rem 0;
}

.divider-line {
  flex: 1;
  height: 1px;
  background: var(--border);
}

.divider-text {
  padding: 0 0.75rem;
  font-size: 0.7rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  color: var(--text-muted);
}

.code-label {
  display: block;
  font-size: 0.85rem;
  color: var(--text-secondary);
  margin-bottom: 0.4rem;
}

.code-input, .uppercase-input {
  text-transform: uppercase;
  letter-spacing: 0.25em;
  font-family: 'JetBrains Mono', monospace;
  font-size: 1.5rem;
  font-weight: 700;
}

.btn-spinner-content {
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.switch-account-btn {
  background: transparent;
  border: none;
  color: var(--text-muted);
  font-size: 0.75rem;
  cursor: pointer;
  transition: color 0.2s ease;
  padding: 4px 8px;
}

.switch-account-btn:hover:not(:disabled) {
  color: var(--accent);
  text-decoration: underline;
}

.switch-account-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
