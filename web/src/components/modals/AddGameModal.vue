<template>
  <BaseModal 
    :model-value="modelValue" 
    title="Add Game to Library" 
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <div class="form-group mb-3">
      <label>Steam App ID:</label>
      <input 
        type="number" 
        class="form-control" 
        v-model="appId" 
        placeholder="730" 
      />
    </div>
    <div class="form-group mb-3">
      <label>Game Title:</label>
      <input 
        type="text" 
        class="form-control" 
        v-model="name" 
        placeholder="Counter-Strike 2" 
        @keyup.enter="handleSubmit"
      />
    </div>

    <template #footer>
      <button class="btn btn-secondary" @click="$emit('update:modelValue', false)">Cancel</button>
      <button 
        class="btn btn-primary" 
        :disabled="!appId || !name" 
        @click="handleSubmit"
      >
        Add Game
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
});

const emit = defineEmits(['update:modelValue', 'submit']);

const appId = ref(null);
const name = ref('');

watch(() => props.modelValue, (val) => {
  if (val) {
    appId.value = null;
    name.value = '';
  }
});

function handleSubmit() {
  if (!appId.value || !name.value) return;
  emit('submit', { appId: parseInt(appId.value), name: name.value });
}
</script>
