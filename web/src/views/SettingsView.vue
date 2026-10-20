<template>
  <div class="tab-view">
    <!-- DNS Interception & Upstream Resolvers -->
    <div class="card mb-4">
      <div class="card-header">
        <div>
          <h3>DNS Interception & Forwarding</h3>
          <p class="section-subtitle">
            Configure upstream resolvers and the target IP for local network game cache
            interception.
          </p>
        </div>
      </div>

      <div class="form-group mb-3">
        <label>Upstream Resolvers (comma-separated IP:port):</label>
        <input
          type="text"
          class="form-control"
          :value="dnsInput"
          @input="$emit('update:dnsInput', $event.target.value)"
          placeholder="1.1.1.1:53, 8.8.8.8:53"
        />
        <small class="text-muted"
          >Standard DNS forwarders used for resolving non-cached public Internet domain
          queries.</small
        >
      </div>

      <div class="form-group mb-3">
        <label>Interception Target IPv4 (Artesian Appliance IP):</label>
        <input
          type="text"
          class="form-control"
          :value="interceptIpInput"
          @input="$emit('update:interceptIpInput', $event.target.value)"
          placeholder="127.0.0.1 or LAN IP (e.g. 192.168.1.100)"
        />
        <small class="text-muted"
          >The IP address returned to LAN clients for game CDN hostnames to direct traffic to the
          reverse proxy.</small
        >
        <div v-if="interceptIpInput === '127.0.0.1'" class="warning-callout mt-2">
          <i class="fa-solid fa-triangle-exclamation mr-1"></i>
          <span
            ><strong>Notice:</strong> Currently set to loopback (<code>127.0.0.1</code>). For remote
            LAN devices to route through Artesian, enter the LAN IP of this server (e.g.
            <code>192.168.0.x</code>) so client machines don't redirect to their own
            localhost.</span
          >
        </div>
      </div>

      <div class="form-actions mt-3">
        <button class="btn btn-primary btn-sm" @click="$emit('save-dns')">
          <i class="fa-solid fa-floppy-disk mr-1"></i> Save DNS Settings
        </button>
      </div>
    </div>

    <!-- Storage Pool Mapping -->
    <div class="card mb-4">
      <div class="card-header">
        <div>
          <h3>Storage Pool Mapping</h3>
          <p class="section-subtitle">
            Content-addressable chunk cache storage directory and disk quota.
          </p>
        </div>
      </div>

      <div class="form-group mb-3">
        <label>Cache Storage Directory:</label>
        <input
          type="text"
          class="form-control"
          :value="storagePathInput"
          @input="$emit('update:storagePathInput', $event.target.value)"
          placeholder="/data/cache"
        />
        <small class="text-muted"
          >Absolute path where raw chunk blobs and manifest files are stored on disk.</small
        >
      </div>

      <div class="form-group mb-3">
        <label>Max Storage Capacity Quota (GB):</label>
        <input
          type="number"
          class="form-control"
          :value="maxStorageGbInput"
          @input="$emit('update:maxStorageGbInput', $event.target.value)"
          placeholder="2048"
          min="10"
        />
        <small class="text-muted"
          >Upper storage limit before automated cache LRU retention pruning engages.</small
        >
      </div>

      <div class="form-actions mt-3">
        <button class="btn btn-primary btn-sm" @click="$emit('save-storage')">
          <i class="fa-solid fa-floppy-disk mr-1"></i> Save Storage Path
        </button>
      </div>
    </div>

    <!-- Appliance Storage Paths Summary -->
    <div class="card">
      <div class="card-header">
        <h3>System Storage Paths</h3>
      </div>
      <div class="table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>Subsystem</th>
              <th>Mount Path</th>
              <th>Description</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td><strong>SQLite Database</strong></td>
              <td><code>/data/artesian.db</code></td>
              <td>
                Persistent state database containing credentials, settings, and chunk indices.
              </td>
            </tr>
            <tr>
              <td><strong>Chunk Storage</strong></td>
              <td><code>/data/cache</code></td>
              <td>Content-addressable storage for cached game depot chunks.</td>
            </tr>
            <tr>
              <td><strong>Manifest Pool</strong></td>
              <td><code>/data/manifests</code></td>
              <td>Decrypted and raw Steam manifest cache files.</td>
            </tr>
            <tr>
              <td><strong>Host Depotcache</strong></td>
              <td><code>/data/steam_depotcache</code></td>
              <td>Optional host Steam depotcache mount for instant offline manifest resolution.</td>
            </tr>
          </tbody>
        </table>
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
  interceptIpInput: {
    type: String,
    default: '127.0.0.1',
  },
  storagePathInput: {
    type: String,
    required: true,
  },
  maxStorageGbInput: {
    type: [Number, String],
    default: 2048,
  },
});

defineEmits([
  'update:dnsInput',
  'update:interceptIpInput',
  'update:storagePathInput',
  'update:maxStorageGbInput',
  'save-dns',
  'save-storage',
]);
</script>

<style scoped>
.section-subtitle {
  font-size: 0.8rem;
  color: var(--text-muted);
  margin-top: 0.2rem;
}

.warning-callout {
  display: flex;
  align-items: flex-start;
  gap: 0.5rem;
  padding: 0.65rem 0.85rem;
  background: rgba(245, 158, 11, 0.12);
  border: 1px solid rgba(245, 158, 11, 0.35);
  border-radius: 6px;
  color: #fbbf24;
  font-size: 0.82rem;
  line-height: 1.4;
}

.warning-callout i {
  margin-top: 0.15rem;
  font-size: 0.95rem;
  color: #f59e0b;
}

.warning-callout code {
  background: rgba(0, 0, 0, 0.25);
  padding: 0.1rem 0.3rem;
  border-radius: 4px;
  color: #fef3c7;
}
</style>
