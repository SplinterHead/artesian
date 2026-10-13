<template>
  <BaseModal 
    :model-value="modelValue" 
    title="Steam Guard 2FA Required" 
    :show-close="false"
    :close-on-backdrop="false"
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <p class="text-muted mb-3">
      Steam Connection Manager has requested 2-factor verification for account <strong>{{ username }}</strong>.
    </p>
    <div class="form-group mb-3">
      <label>Steam Guard Code (Mobile / Email):</label>
      <input 
        type="text" 
        class="form-control text-center code-input" 
        v-model="code" 
        placeholder="e.g. 5R7X9" 
        maxlength="10"
        autofocus
        @keyup.enter="handleSubmit"
      />
    </div>

    <template #footer>
      <button class="btn btn-secondary" @click="$emit('dismiss')">Dismiss</button>
      <button class="btn btn-primary" :disabled="!code" @click="handleSubmit">
        Verify Code
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
});

const emit = defineEmits(['update:modelValue', 'submit', 'dismiss']);

const code = ref('');

watch(() => props.modelValue, (val) => {
  if (val) {
    code.value = '';
  }
});

function handleSubmit() {
  if (!code.value) return;
  emit('submit', code.value);
}
</script>
