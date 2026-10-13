<template>
  <BaseModal 
    :model-value="!!service" 
    :title="service ? `${service.display_name} (${service.company})` : ''" 
    @update:model-value="$emit('close')"
  >
    <div v-if="service">
      <p class="mb-3">{{ service.description }}</p>
      <div class="status-pill online mb-3">
        <span class="status-dot"></span>
        <span>DNS Interception & Reverse Proxy Caching Active</span>
      </div>
      <p class="text-sm text-muted">
        Platform account credential management and authenticated manifest prefetching for this ecosystem is scheduled for a future update.
      </p>
    </div>

    <template #footer>
      <button class="btn btn-primary" @click="$emit('close')">Got It</button>
    </template>
  </BaseModal>
</template>

<script setup>
import BaseModal from '../common/BaseModal.vue';

defineProps({
  service: {
    type: Object,
    default: null,
  },
});

defineEmits(['close']);
</script>
