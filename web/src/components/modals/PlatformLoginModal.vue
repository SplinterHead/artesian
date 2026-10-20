<template>
  <BaseModal 
    :model-value="modelValue" 
    :title="resolvedTitle" 
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <!-- Mode A: Direct Username & Password Credentials (e.g. Steam) -->
    <div v-if="resolvedAuthType === 'credentials'" class="credentials-container">
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

    <!-- Mode B: OAuth2 Browser Sign-In & Code Paste (e.g. Epic Games) -->
    <div v-else-if="resolvedAuthType === 'oauth_code'" class="oauth-container">
      <div class="step-card mb-3">
        <div class="step-num">1</div>
        <div class="step-content">
          <div class="step-title">Sign in to your {{ resolvedServiceName }} account</div>
          <p class="text-xs text-muted mt-1">
            {{ resolvedOauthHelpText }}
          </p>
          <a 
            v-if="resolvedOauthUrl"
            :href="resolvedOauthUrl" 
            target="_blank" 
            rel="noopener noreferrer" 
            class="btn btn-sm btn-outline-primary mt-2 launch-link"
          >
            <i class="fa-solid fa-arrow-up-right-from-square mr-1"></i> Open {{ resolvedServiceName }} Sign-In
          </a>
        </div>
      </div>

      <div class="step-card mb-3">
        <div class="step-num">2</div>
        <div class="step-content">
          <div class="step-title">Paste the authorization code or JSON</div>
          <p class="text-xs text-muted mt-1">
            After signing in, paste the resulting <code>authorizationCode</code> or the complete JSON response below.
          </p>
          <textarea 
            class="form-control mt-2 code-textarea" 
            rows="3"
            v-model="code" 
            :placeholder="resolvedCodePlaceholder"
            :disabled="loading"
            @keyup.enter.ctrl="handleSubmit"
          ></textarea>
        </div>
      </div>
    </div>

    <!-- Error Alert Notice -->
    <div v-if="errorMsg" class="alert alert-danger text-xs mt-2">
      <i class="fa-solid fa-triangle-exclamation mr-1"></i> {{ errorMsg }}
    </div>

    <!-- Modal Footer Actions -->
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
    </template>
  </BaseModal>
</template>

<script setup>
import { ref, computed, watch } from 'vue';
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
  epic: {
    serviceName: 'Epic Games',
    authType: 'oauth_code',
    title: 'Connect Epic Games Account',
    oauthUrl: 'https://www.epicgames.com/id/login?redirectUrl=https%3A%2F%2Fwww.epicgames.com%2Fid%2Fapi%2Fredirect%3FclientId%3D34a02cf8f4414e29b15921876da36f9a%26responseType%3Dcode',
    oauthHelpText: 'Open the official Epic Games authorization page in a new browser tab and log into your account.',
    codePlaceholder: 'Paste code here (e.g. 859f518a4a2b4b4bb2e8f1dd63... or {"authorizationCode":"..."})',
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
  authType: {
    type: String,
    default: '', // 'credentials' | 'oauth_code'
  },
  title: {
    type: String,
    default: '',
  },
  oauthUrl: {
    type: String,
    default: '',
  },
  oauthHelpText: {
    type: String,
    default: '',
  },
  codePlaceholder: {
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
});

const emit = defineEmits(['update:modelValue', 'submit']);

// Form fields
const username = ref('');
const password = ref('');
const code = ref('');

const svcConfig = computed(() => {
  return SERVICE_DEFAULTS[props.service?.toLowerCase()] || {};
});

const resolvedServiceName = computed(() => {
  return props.serviceName || svcConfig.value.serviceName || (props.service ? props.service.toUpperCase() : 'Platform');
});

const resolvedAuthType = computed(() => {
  if (props.authType) return props.authType;
  if (svcConfig.value.authType) return svcConfig.value.authType;
  return 'credentials';
});

const resolvedTitle = computed(() => {
  if (props.title) return props.title;
  if (svcConfig.value.title) return svcConfig.value.title;
  return `Connect ${resolvedServiceName.value} Account`;
});

const resolvedOauthUrl = computed(() => {
  return props.oauthUrl || svcConfig.value.oauthUrl || '';
});

const resolvedOauthHelpText = computed(() => {
  return props.oauthHelpText || svcConfig.value.oauthHelpText || `Open the official ${resolvedServiceName.value} authorization page in a new browser tab and log into your account.`;
});

const resolvedCodePlaceholder = computed(() => {
  return props.codePlaceholder || svcConfig.value.codePlaceholder || 'Paste authorization code or JSON here...';
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

const canSubmit = computed(() => {
  if (resolvedAuthType.value === 'credentials') {
    return !!username.value.trim() && !!password.value;
  }
  return !!code.value.trim();
});

watch(() => props.modelValue, (isOpen) => {
  if (isOpen) {
    username.value = '';
    password.value = '';
    code.value = '';
  }
});

function handleSubmit() {
  if (props.loading || !canSubmit.value) return;

  if (resolvedAuthType.value === 'credentials') {
    emit('submit', {
      service: props.service,
      username: username.value.trim(),
      password: password.value,
    });
  } else {
    emit('submit', {
      service: props.service,
      code: code.value.trim(),
    });
  }
}
</script>

<style scoped>
.credentials-container {
  display: flex;
  flex-direction: column;
}

.oauth-container {
  display: flex;
  flex-direction: column;
}

.step-card {
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

.btn-connect-action {
  min-width: 175px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
}
</style>
