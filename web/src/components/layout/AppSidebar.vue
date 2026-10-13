<template>
  <aside class="sidebar">
    <div class="brand">
      <div class="logo-orb"></div>
      <div>
        <h2>Artesian</h2>
        <span class="badge">v1.0.0</span>
      </div>
    </div>

    <nav class="nav-menu">
      <button 
        v-for="tab in tabs" 
        :key="tab.id"
        :class="['nav-item', { active: currentTab === tab.id }]"
        @click="$emit('update:currentTab', tab.id)"
      >
        <span class="nav-icon">
          <i :class="tab.icon"></i>
        </span>
        <span>{{ tab.label }}</span>
      </button>
    </nav>

    <!-- Connection Status Pointers -->
    <div class="sidebar-status-group">
      <div class="status-pill" :class="{ online: wsConnected, offline: !wsConnected }">
        <span class="status-dot"></span>
        <span>WebSocket {{ wsConnected ? 'Live' : 'Offline' }}</span>
      </div>
      <div class="status-pill online">
        <span class="status-dot"></span>
        <span>Proxy Port :80 & DNS :53</span>
      </div>
    </div>
  </aside>
</template>

<script setup>
defineProps({
  tabs: {
    type: Array,
    required: true,
  },
  currentTab: {
    type: String,
    required: true,
  },
  wsConnected: {
    type: Boolean,
    default: false,
  },
});

defineEmits(['update:currentTab']);
</script>

<style scoped>
.sidebar {
  width: 260px;
  background-color: var(--bg-surface);
  border-right: 1px solid var(--border);
  padding: 1.5rem;
  display: flex;
  flex-direction: column;
}

.brand {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 2rem;
}

.logo-orb {
  width: 36px;
  height: 36px;
  border-radius: 8px;
  background: linear-gradient(135deg, var(--accent), #0284c7);
  box-shadow: 0 0 16px var(--accent-glow);
}

.brand h2 {
  font-size: 1.25rem;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.nav-menu {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  flex: 1;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 0.85rem;
  padding: 0.75rem 1rem;
  border-radius: 8px;
  background: transparent;
  border: none;
  color: var(--text-secondary);
  font-size: 0.95rem;
  font-weight: 500;
  cursor: pointer;
  text-align: left;
  transition: all 0.15s ease;
}

.nav-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  font-size: 1.05rem;
  color: #94a3b8;
  transition: all 0.15s ease;
}

.nav-item:hover {
  background: rgba(255, 255, 255, 0.04);
  color: var(--text-primary);
}

.nav-item:hover .nav-icon {
  color: var(--accent);
}

.nav-item.active {
  background: var(--bg-card);
  color: var(--accent);
  border-left: 3px solid var(--accent);
}

.nav-item.active .nav-icon {
  color: var(--accent);
  filter: drop-shadow(0 0 6px var(--accent-glow));
}

.sidebar-status-group {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
</style>
