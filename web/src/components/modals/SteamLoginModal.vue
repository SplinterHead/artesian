<template>
  <BaseModal 
    :model-value="modelValue" 
    title="Steam CM Authentication" 
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <div class="form-group mb-3">
      <label>Steam Account Name:</label>
      <input 
        type="text" 
        class="form-control" 
        v-model="username" 
        placeholder="Username" 
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
        class="btn btn-primary btn-steam-connect" 
        :disabled="loading || !username.trim() || !password" 
        @click="handleSubmit"
      >
        <template v-if="loading">
          <i class="fa-solid fa-circle-notch fa-spin mr-2"></i>
          <span>Connecting to Steam...</span>
        </template>
        <template v-else>
          <span>Connect to Steam</span>
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
  if (props.loading || !username.value.trim() || !password.value) return;
  emit('submit', { username: username.value.trim(), password: password.value });
}
</script>

<style scoped>
.btn-steam-connect {
  min-width: 175px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
}
</style>
