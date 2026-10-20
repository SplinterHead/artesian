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
    <!-- Credentials Form -->
    <div class="credentials-container">
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
});

const emit = defineEmits(['update:modelValue', 'submit']);

// Form fields
const username = ref('');
const password = ref('');

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

const canSubmit = computed(() => {
  return !!username.value.trim() && !!password.value;
});

watch(
  () => props.modelValue,
  (isOpen) => {
    if (isOpen) {
      username.value = '';
      password.value = '';
    }
  }
);

function handleSubmit() {
  if (props.loading || !canSubmit.value) return;

  emit('submit', {
    service: props.service,
    username: username.value.trim(),
    password: password.value,
  });
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

.modal-title-text {
  margin: 0;
  font-size: 1.15rem;
  font-weight: 700;
  color: var(--text-primary);
}
</style>
