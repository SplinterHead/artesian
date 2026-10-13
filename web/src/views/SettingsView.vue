<template>
  <div class="tab-view">
    <div class="card mb-4">
      <div class="card-header">
        <h3>Upstream DNS Forwarding</h3>
      </div>
      <div class="form-group">
        <label>Upstream Resolvers (comma-separated IP:port):</label>
        <input 
          type="text" 
          class="form-control" 
          :value="dnsInput" 
          @input="$emit('update:dnsInput', $event.target.value)"
          placeholder="1.1.1.1:53, 8.8.8.8:53"
        />
      </div>
      <div class="form-actions mt-3">
        <button class="btn btn-primary btn-sm" @click="$emit('save-dns')">Save DNS Settings</button>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <h3>Storage Pool Mapping (The Tank)</h3>
      </div>
      <div class="form-group">
        <label>Storage Path:</label>
        <input 
          type="text" 
          class="form-control" 
          :value="storagePathInput" 
          @input="$emit('update:storagePathInput', $event.target.value)"
          placeholder="/data/cache"
        />
      </div>
      <div class="form-actions mt-3">
        <button class="btn btn-primary btn-sm" @click="$emit('save-storage')">Save Storage Path</button>
      </div>
    </div>
  </div>
</template>

<script setup>
defineProps({
  dnsInput: {
    type: String,
    required: true,
  },
  storagePathInput: {
    type: String,
    required: true,
  },
});

defineEmits(['update:dnsInput', 'update:storagePathInput', 'save-dns', 'save-storage']);
</script>
