<template>
  <BaseModal 
    :model-value="modelValue" 
    title="Steam CM Authentication" 
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <p class="text-muted mb-3">
      Authenticate with the Steam Connection Manager to enable manifest decryption and headless prefilling.
    </p>
    <div class="form-group mb-3">
      <label>Steam Account Name:</label>
      <input 
        type="text" 
        class="form-control" 
        v-model="username" 
        placeholder="username" 
      />
    </div>
    <div class="form-group mb-3">
      <label>Password:</label>
      <input 
        type="password" 
        class="form-control" 
        v-model="password" 
        placeholder="••••••••" 
        @keyup.enter="handleSubmit"
      />
    </div>

    <template #footer>
      <button class="btn btn-secondary" @click="$emit('update:modelValue', false)">Cancel</button>
      <button 
        class="btn btn-primary" 
        :disabled="loading || !username || !password" 
        @click="handleSubmit"
      >
        {{ loading ? 'Connecting...' : 'Connect to Steam' }}
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
});

const emit = defineEmits(['update:modelValue', 'submit']);

const username = ref('');
const password = ref('');

watch(() => props.modelValue, (val) => {
  if (val) {
    username.value = '';
    password.value = '';
  }
});

function handleSubmit() {
  if (!username.value || !password.value) return;
  emit('submit', { username: username.value, password: password.value });
}
</script>
