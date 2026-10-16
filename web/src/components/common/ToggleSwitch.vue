<template>
  <div class="toggle-wrapper" :title="title">
    <span v-if="activeText || inactiveText" class="toggle-status-label" :class="{ 'is-active': modelValue }">
      {{ modelValue ? activeText : inactiveText }}
    </span>
    <label class="switch">
      <input 
        type="checkbox" 
        :checked="modelValue" 
        @change="$emit('update:modelValue', $event.target.checked); $emit('change', $event.target.checked)"
      >
      <span class="slider"></span>
    </label>
  </div>
</template>

<script setup>
defineProps({
  modelValue: {
    type: Boolean,
    default: false,
  },
  activeText: {
    type: String,
    default: '',
  },
  inactiveText: {
    type: String,
    default: '',
  },
  title: {
    type: String,
    default: '',
  },
});

defineEmits(['update:modelValue', 'change']);
</script>

<style scoped>
.toggle-wrapper {
  display: inline-flex;
  flex-direction: column;
  align-items: flex-end;
  justify-content: center;
  flex-shrink: 0;
}

.toggle-status-label {
  font-size: 0.72rem;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.03em;
  transition: color 0.2s ease;
}

.toggle-status-label.is-active {
  color: var(--accent);
}
</style>
