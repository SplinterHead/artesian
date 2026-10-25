<template>
  <BaseModal
    :model-value="modelValue"
    :title="resolvedTitle"
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <template #header>
      <div class="modal-title-with-logo">
        <div class="modal-title-logo-box" :class="'store-logo-' + (service || 'generic')">
          <svg
            v-if="platformLogos[service]"
            class="store-svg-logo"
            :viewBox="platformLogos[service].viewBox"
            fill="currentColor"
          >
            <path :d="platformLogos[service].path" />
          </svg>
          <i v-else class="fa-solid fa-gamepad fallback-logo-icon"></i>
        </div>
        <h3 class="modal-title-text">{{ resolvedTitle }}</h3>
      </div>
    </template>

    <!-- Case A: Xbox Device Code Flow -->
    <div v-if="isXboxService" class="xbox-auth-container">
      <!-- Device Code Card when active -->
      <div v-if="activeUserCode" class="device-login-card">
        <div class="device-login-header">
          <div class="radar-pulse-icon">
            <span class="pulse-ring"></span>
            <svg
              v-if="platformLogos.xbox"
              class="pulse-svg-icon"
              :viewBox="platformLogos.xbox.viewBox"
              fill="#107c10"
            >
              <path :d="platformLogos.xbox.path" />
            </svg>
            <i v-else class="fa-brands fa-xbox text-green"></i>
          </div>
          <div>
            <h4 class="device-card-title">Microsoft Device Login</h4>
            <p class="text-xs text-muted mb-0">Follow the two steps below to link your account</p>
          </div>
        </div>

        <div class="device-steps-list mt-3">
          <!-- Step 1 -->
          <div class="device-step-item">
            <div class="step-num">1</div>
            <div class="step-content">
              <span class="step-label">Open the verification page:</span>
              <a
                :href="activeVerificationUrl"
                target="_blank"
                rel="noopener noreferrer"
                class="btn btn-sm btn-outline-primary btn-link-action mt-1"
              >
                <span>{{ cleanUrl(activeVerificationUrl) }}</span>
                <i class="fa-solid fa-arrow-up-right-from-square ml-1 text-xs"></i>
              </a>
            </div>
          </div>

          <!-- Step 2 -->
          <div class="device-step-item mt-3">
            <div class="step-num">2</div>
            <div class="step-content">
              <span class="step-label">Enter this one-time code:</span>
              <div class="code-display-box mt-1">
                <span class="code-text">{{ activeUserCode }}</span>
                <button
                  type="button"
                  class="btn btn-sm btn-copy-code"
                  :class="{ copied: codeCopied }"
                  @click="copyCodeToClipboard"
                  title="Copy code to clipboard"
                >
                  <i :class="codeCopied ? 'fa-solid fa-check' : 'fa-regular fa-copy'"></i>
                  <span>{{ codeCopied ? 'Copied!' : 'Copy' }}</span>
                </button>
              </div>
            </div>
          </div>
        </div>

        <div class="waiting-indicator mt-3">
          <i class="fa-solid fa-circle-notch fa-spin text-green mr-2"></i>
          <span class="text-xs text-muted">Waiting for approval in your browser...</span>
        </div>
      </div>

      <!-- Initial / Not yet started state for Xbox -->
      <div v-else-if="!showRefreshTokenInput" class="xbox-intro-card">
        <div class="xbox-intro-icon">
          <svg
            v-if="platformLogos.xbox"
            class="store-svg-logo-lg"
            :viewBox="platformLogos.xbox.viewBox"
            fill="currentColor"
          >
            <path :d="platformLogos.xbox.path" />
          </svg>
        </div>
        <h4>Sign In with Microsoft</h4>
        <p class="text-xs text-muted text-center mt-1">
          Artesian uses Microsoft’s official OAuth2 Device Code flow. You authenticate directly on
          Microsoft’s website without typing your password into Artesian.
        </p>
        <button
          type="button"
          class="btn btn-primary btn-block btn-start-device mt-3"
          :disabled="loading"
          @click="handleStartDeviceLogin"
        >
          <template v-if="loading">
            <i class="fa-solid fa-circle-notch fa-spin mr-2"></i>
            <span>Requesting Code...</span>
          </template>
          <template v-else>
            <i class="fa-brands fa-microsoft mr-2"></i>
            <span>Generate Login Code</span>
          </template>
        </button>

        <div class="mt-3 text-center">
          <button
            type="button"
            class="btn-link-subtle text-xs"
            @click="showRefreshTokenInput = true"
          >
            Or sign in with MSA Refresh Token →
          </button>
        </div>
      </div>

      <!-- Advanced Refresh Token Input -->
      <div v-else class="refresh-token-form">
        <div class="form-group mb-2">
          <label>MSA Refresh Token:</label>
          <textarea
            class="form-control font-mono text-xs"
            rows="3"
            v-model="password"
            placeholder="M.R3_..."
            :disabled="loading"
          ></textarea>
        </div>
        <div class="d-flex justify-content-between align-items-center mt-2">
          <button
            type="button"
            class="btn-link-subtle text-xs"
            @click="showRefreshTokenInput = false"
          >
            ← Back to Device Code
          </button>
        </div>
      </div>
    </div>

    <!-- Case B: Epic Games OAuth2 Code Flow -->
    <div v-else-if="isEpicService" class="epic-auth-container">
      <div class="device-login-card">
        <div class="device-steps-list">
          <!-- Step 1 -->
          <div class="device-step-item">
            <div class="step-num">1</div>
            <div class="step-content">
              <span class="step-label">Open the official Epic Games authorization page:</span>
              <a
                :href="svcConfig.oauthUrl"
                target="_blank"
                rel="noopener noreferrer"
                class="btn btn-sm btn-outline-primary btn-link-action mt-1"
              >
                <span>Open Epic Games Sign-In</span>
                <i class="fa-solid fa-arrow-up-right-from-square ml-1 text-xs"></i>
              </a>
              <p class="text-xs text-muted mt-1 mb-0">
                Log into your Epic account in your browser and approve launcher access.
              </p>
            </div>
          </div>

          <!-- Step 2 -->
          <div class="device-step-item mt-3">
            <div class="step-num">2</div>
            <div class="step-content">
              <span class="step-label">Paste the authorization code or JSON:</span>
              <textarea
                class="form-control font-mono text-xs mt-2"
                rows="3"
                v-model="epicCode"
                :placeholder="svcConfig.codePlaceholder"
                :disabled="loading"
                @keyup.enter.ctrl="handleSubmit"
              ></textarea>
              <p class="text-xs text-muted mt-1 mb-0">
                Paste the resulting 32-character code, redirect URL, or JSON response above.
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Case C: Standard Credentials Form (Steam & Generic) -->
    <div v-else class="credentials-container">
      <div class="form-group mb-3">
        <label>{{ resolvedUsernameLabel }}:</label>
        <input
          type="text"
          class="form-control"
          v-model="username"
          :placeholder="resolvedUsernamePlaceholder"
          :disabled="loading"
          @keyup.enter="handleSubmit"
        />
      </div>
      <div class="form-group mb-3">
        <label>Password:</label>
        <input
          type="password"
          class="form-control"
          v-model="password"
          placeholder="••••••••"
          :disabled="loading"
          @keyup.enter="handleSubmit"
        />
      </div>
    </div>

    <!-- Error Alert Notice -->
    <div v-if="errorMsg" class="alert alert-danger text-xs mt-3">
      <i class="fa-solid fa-triangle-exclamation mr-1"></i> {{ errorMsg }}
    </div>

    <!-- Modal Footer Actions -->
    <template #footer>
      <button
        type="button"
        class="btn btn-outline-secondary"
        :disabled="loading && !activeUserCode"
        @click="handleCancel"
      >
        {{ activeUserCode ? 'Close' : 'Cancel' }}
      </button>

      <button
        v-if="!isXboxService || showRefreshTokenInput"
        type="button"
        class="btn btn-primary btn-connect-action"
        :disabled="loading || !canSubmit"
        @click="handleSubmit"
      >
        <template v-if="loading">
          <i class="fa-solid fa-circle-notch fa-spin mr-2"></i>
          <span>Connecting...</span>
        </template>
        <template v-else>
          <span>{{ resolvedSubmitText }}</span>
        </template>
      </button>

      <button
        v-else-if="activeUserCode"
        type="button"
        class="btn btn-primary"
        @click="openVerificationLink"
      >
        <span>Open Microsoft Login</span>
        <i class="fa-solid fa-arrow-up-right-from-square ml-1 text-xs"></i>
      </button>
    </template>
  </BaseModal>
</template>

<script setup>
import { ref, computed, watch } from 'vue';
import { platformLogos } from '../../utils/logos';
import BaseModal from '../common/BaseModal.vue';

const SERVICE_DEFAULTS = {
  steam: {
    serviceName: 'Steam',
    authType: 'credentials',
    title: 'Steam CM Authentication',
    usernameLabel: 'Steam Account Name',
    usernamePlaceholder: 'Username',
    submitText: 'Connect to Steam',
  },
  xbox: {
    serviceName: 'Xbox / Microsoft Store',
    authType: 'device_code',
    title: 'Xbox & Microsoft Store Authentication',
    submitText: 'Connect Xbox Account',
  },
  epic: {
    serviceName: 'Epic Games',
    authType: 'oauth_code',
    title: 'Connect Epic Games Account',
    oauthUrl:
      'https://www.epicgames.com/id/login?redirectUrl=https%3A%2F%2Fwww.epicgames.com%2Fid%2Fapi%2Fredirect%3FclientId%3D34a02cf8f4414e29b15921876da36f9a%26responseType%3Dcode',
    codePlaceholder:
      'Paste code here (e.g. 859f518a4a2b4b4bb2e8f1dd63... or {"authorizationCode":"..."})',
    submitText: 'Connect Epic Account',
  },
};

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false,
  },
  service: {
    type: String,
    default: 'steam',
  },
  serviceName: {
    type: String,
    default: '',
  },
  title: {
    type: String,
    default: '',
  },
  usernameLabel: {
    type: String,
    default: '',
  },
  usernamePlaceholder: {
    type: String,
    default: '',
  },
  submitText: {
    type: String,
    default: '',
  },
  loading: {
    type: Boolean,
    default: false,
  },
  errorMsg: {
    type: String,
    default: '',
  },
  authStatus: {
    type: Object,
    default: () => ({}),
  },
});

const emit = defineEmits(['update:modelValue', 'submit', 'cancel']);

// Form fields
const username = ref('');
const password = ref('');
const epicCode = ref('');
const showRefreshTokenInput = ref(false);
const codeCopied = ref(false);

const authFlowType = computed(() => {
  switch (props.service?.toLowerCase()) {
    case 'xbox':
      return 'xbox';
    case 'epic':
      return 'epic';
    default:
      return 'credentials';
  }
});

const isXboxService = computed(() => authFlowType.value === 'xbox');
const isEpicService = computed(() => authFlowType.value === 'epic');

const svcConfig = computed(() => {
  return SERVICE_DEFAULTS[props.service?.toLowerCase()] || {};
});

const resolvedServiceName = computed(() => {
  return (
    props.serviceName ||
    svcConfig.value.serviceName ||
    (props.service ? props.service.toUpperCase() : 'Platform')
  );
});

const resolvedTitle = computed(() => {
  if (props.title) return props.title;
  if (svcConfig.value.title) return svcConfig.value.title;
  return `Connect ${resolvedServiceName.value} Account`;
});

const resolvedUsernameLabel = computed(() => {
  return props.usernameLabel || svcConfig.value.usernameLabel || 'Username';
});

const resolvedUsernamePlaceholder = computed(() => {
  return props.usernamePlaceholder || svcConfig.value.usernamePlaceholder || 'Username';
});

const resolvedSubmitText = computed(() => {
  if (props.submitText) return props.submitText;
  if (svcConfig.value.submitText) return svcConfig.value.submitText;
  return `Connect ${resolvedServiceName.value}`;
});

const activeUserCode = computed(() => {
  return props.authStatus?.user_code || '';
});

const activeVerificationUrl = computed(() => {
  return props.authStatus?.verification_url || 'https://www.microsoft.com/link';
});

const canSubmit = computed(() => {
  switch (props.service?.toLowerCase()) {
    case 'xbox':
      if (showRefreshTokenInput.value) {
        return !!password.value.trim();
      }
      return true;
    case 'epic':
      return !!epicCode.value.trim();
    default:
      return !!username.value.trim() && !!password.value;
  }
});

watch(
  () => props.modelValue,
  (isOpen) => {
    if (isOpen) {
      username.value = '';
      password.value = '';
      epicCode.value = '';
      showRefreshTokenInput.value = false;
      codeCopied.value = false;

      // Automatically trigger device code generation if opening Xbox modal
      switch (props.service?.toLowerCase()) {
        case 'xbox':
          if (!activeUserCode.value) {
            emit('submit', {
              service: 'xbox',
              username: '',
              password: '',
            });
          }
          break;
      }
    }
  }
);

function handleStartDeviceLogin() {
  emit('submit', {
    service: 'xbox',
    username: '',
    password: '',
  });
}

function handleSubmit() {
  if (props.loading || !canSubmit.value) return;

  switch (props.service?.toLowerCase()) {
    case 'epic':
      emit('submit', {
        service: 'epic',
        username: epicCode.value.trim(),
        password: '',
      });
      break;
    default:
      emit('submit', {
        service: props.service,
        username: username.value.trim(),
        password: password.value.trim(),
      });
      break;
  }
}

function handleCancel() {
  emit('update:modelValue', false);
  emit('cancel');
}

function cleanUrl(raw) {
  if (!raw) return 'microsoft.com/devicelogin';
  return raw.replace(/^https?:\/\//, '').replace(/\/$/, '');
}

async function copyCodeToClipboard() {
  if (!activeUserCode.value) return;
  try {
    await navigator.clipboard.writeText(activeUserCode.value);
    codeCopied.value = true;
    setTimeout(() => {
      codeCopied.value = false;
    }, 2500);
  } catch {
    // Fallback
    const input = document.createElement('input');
    input.value = activeUserCode.value;
    document.body.appendChild(input);
    input.select();
    document.execCommand('copy');
    document.body.removeChild(input);
    codeCopied.value = true;
    setTimeout(() => {
      codeCopied.value = false;
    }, 2500);
  }
}

function openVerificationLink() {
  window.open(activeVerificationUrl.value, '_blank', 'noopener,noreferrer');
}
</script>

<style scoped>
.credentials-container {
  display: flex;
  flex-direction: column;
}

.btn-connect-action {
  min-width: 175px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
}

.modal-title-with-logo {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.modal-title-logo-box {
  width: 28px;
  height: 28px;
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.modal-title-logo-box .store-svg-logo {
  width: 16px;
  height: 16px;
  display: block;
}

.store-logo-steam {
  background: radial-gradient(circle at top left, rgba(102, 192, 244, 0.25), rgba(23, 26, 38, 0.8));
  color: #66c0f4;
  border: 1px solid rgba(102, 192, 244, 0.35);
  box-shadow: 0 2px 8px rgba(102, 192, 244, 0.15);
}

.store-logo-xbox {
  background: radial-gradient(circle at top left, rgba(16, 124, 16, 0.35), rgba(15, 23, 42, 0.8));
  color: #107c10;
  border: 1px solid rgba(16, 124, 16, 0.45);
  box-shadow: 0 2px 8px rgba(16, 124, 16, 0.2);
}

.store-logo-epic {
  background: radial-gradient(circle at top left, rgba(255, 255, 255, 0.2), rgba(20, 20, 20, 0.85));
  color: #ffffff;
  border: 1px solid rgba(255, 255, 255, 0.28);
  box-shadow: 0 2px 8px rgba(255, 255, 255, 0.1);
}

.modal-title-text {
  margin: 0;
  font-size: 1.15rem;
  font-weight: 700;
  color: var(--text-primary);
}

/* Xbox Auth Cards */
.xbox-auth-container {
  display: flex;
  flex-direction: column;
}

.xbox-intro-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 1.25rem 0.5rem;
}

.xbox-intro-icon {
  width: 56px;
  height: 56px;
  border-radius: 14px;
  background: rgba(16, 124, 16, 0.15);
  border: 1px solid rgba(16, 124, 16, 0.3);
  display: flex;
  align-items: center;
  justify-content: center;
  color: #107c10;
  margin-bottom: 1rem;
}

.store-svg-logo-lg {
  width: 32px;
  height: 32px;
  display: block;
}

.device-login-card {
  background: rgba(15, 23, 42, 0.6);
  border: 1px solid rgba(16, 124, 16, 0.3);
  border-radius: var(--radius);
  padding: 1.25rem;
}

.device-login-header {
  display: flex;
  align-items: center;
  gap: 0.85rem;
}

.radar-pulse-icon {
  position: relative;
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: rgba(16, 124, 16, 0.15);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 1.25rem;
}

.pulse-svg-icon {
  width: 22px;
  height: 22px;
  position: relative;
  z-index: 2;
  display: block;
}

.pulse-ring {
  position: absolute;
  width: 100%;
  height: 100%;
  border-radius: 50%;
  border: 2px solid rgba(16, 124, 16, 0.6);
  animation: radar-pulse 2s ease-out infinite;
}

@keyframes radar-pulse {
  0% {
    transform: scale(0.9);
    opacity: 1;
  }
  100% {
    transform: scale(1.6);
    opacity: 0;
  }
}

.device-card-title {
  margin: 0;
  font-size: 1.05rem;
  font-weight: 700;
  color: var(--text-primary);
}

.device-steps-list {
  display: flex;
  flex-direction: column;
}

.device-step-item {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
}

.step-num {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--bg-hover);
  border: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 0.75rem;
  font-weight: 700;
  color: var(--accent);
  flex-shrink: 0;
  margin-top: 0.15rem;
}

.step-content {
  flex: 1;
  display: flex;
  flex-direction: column;
}

.step-label {
  font-size: 0.8rem;
  color: var(--text-secondary);
}

.btn-link-action {
  align-self: flex-start;
  font-family: var(--font-mono, monospace);
  font-size: 0.8rem;
  padding: 0.35rem 0.65rem;
}

.code-display-box {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.code-text {
  font-family: var(--font-mono, monospace);
  font-size: 1.35rem;
  font-weight: 800;
  letter-spacing: 0.15em;
  color: #10d832;
  background: #090d16;
  padding: 0.4rem 0.85rem;
  border-radius: 8px;
  border: 1px solid rgba(16, 124, 16, 0.5);
  box-shadow: 0 0 14px rgba(16, 124, 16, 0.2);
  user-select: all;
}

.btn-copy-code {
  background: var(--bg-hover);
  border: 1px solid var(--border);
  color: var(--text-primary);
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  padding: 0.45rem 0.75rem;
  border-radius: 6px;
  font-size: 0.8rem;
  transition: all 0.2s ease;
}

.btn-copy-code:hover {
  border-color: #10d832;
  color: #10d832;
}

.btn-copy-code.copied {
  background: rgba(16, 124, 16, 0.2);
  border-color: #10d832;
  color: #10d832;
}

.waiting-indicator {
  display: flex;
  align-items: center;
  padding-top: 0.5rem;
  border-top: 1px dashed var(--border);
}

.btn-start-device {
  background: #107c10;
  border-color: #107c10;
  font-weight: 600;
}

.btn-start-device:hover {
  background: #0e6b0e;
  border-color: #0e6b0e;
}

.btn-link-subtle {
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  padding: 0;
}

.btn-link-subtle:hover {
  color: var(--text-primary);
  text-decoration: underline;
}

.text-green {
  color: #10d832 !important;
}
</style>
