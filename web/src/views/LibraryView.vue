<template>
  <div class="tab-view">
    <!-- Table 1: Monitored Game Library -->
    <div class="card mb-4">
      <div class="card-header">
        <div>
          <h3>Game Library</h3>
          <span class="text-xs text-muted">Explicitly monitored games configured for automated caching and prefilling</span>
        </div>
        <button class="btn btn-primary btn-sm" @click="$emit('open-add-game')">
          + Add Game
        </button>
      </div>

      <div class="table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th style="width: 50px;">Store</th>
              <th>Title</th>
              <th>Status</th>
              <th>Auto-Update</th>
              <th>Size</th>
              <th>Last Sync</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="library.length === 0">
              <td colspan="7" class="empty-cell">
                No games added to library yet. Add a game above or select from cached games below.
              </td>
            </tr>
            <tr v-for="game in library" :key="(game.service || 'steam') + game.app_id">
              <td>
                <div 
                  class="store-logo-box" 
                  :class="'store-logo-' + (getLogoKey(game.service) || 'generic')"
                  :title="game.service ? game.service.toUpperCase() : 'Steam'"
                >
                  <svg 
                    v-if="platformLogos[getLogoKey(game.service)]" 
                    class="store-svg-logo" 
                    :viewBox="platformLogos[getLogoKey(game.service)].viewBox" 
                    fill="currentColor"
                  >
                    <path :d="platformLogos[getLogoKey(game.service)].path" />
                  </svg>
                  <i v-else class="fa-solid fa-gamepad fallback-logo-icon"></i>
                </div>
              </td>
              <td>
                <div class="game-cell">
                  <strong>{{ game.name }}</strong>
                  <span v-if="game.platform" class="platform-tags-wrap">
                    <span 
                      v-for="p in getPlatformList(game.platform)" 
                      :key="p" 
                      class="platform-tag" 
                      :title="p"
                    >
                      <i :class="getPlatformIcon(p)"></i>
                    </span>
                  </span>
                </div>
              </td>
              <td>
                <span :class="['status-badge', game.status]">
                  {{ game.status.toUpperCase() }}
                </span>
              </td>
              <td>
                <button 
                  type="button"
                  class="badge-toggle-btn"
                  :title="'Click to turn ' + (game.auto_update ? 'off' : 'on') + ' auto-update for this title'"
                  @click="$emit('toggle-game-autoupdate', game)"
                >
                  <span class="badge" :class="game.auto_update ? 'badge-hit' : 'badge-secondary'">
                    {{ game.auto_update ? 'ENABLED' : 'OFF' }}
                  </span>
                </button>
              </td>
              <td>
                <div class="size-cell">
                  <span class="depot-size">{{ formatBytes(game.size_bytes) }}</span>
                </div>
              </td>
              <td class="text-muted">{{ formatDate(game.last_checked) }}</td>
              <td>
                <div class="table-actions">
                  <button 
                    class="btn btn-xs btn-outline-secondary"
                    :disabled="recheckingKey === getGameKey(game) || deletingKey === getGameKey(game)"
                    title="Re-check and sync chunks against depot"
                    @click="$emit('recheck-game', game)"
                  >
                    <i v-if="recheckingKey === getGameKey(game)" class="fa-solid fa-circle-notch fa-spin"></i>
                    <i v-else class="fa-solid fa-rotate-right"></i>
                  </button>
                  <button 
                    class="btn btn-xs btn-outline-danger"
                    :disabled="deletingKey === getGameKey(game)"
                    title="Remove game from library"
                    @click="$emit('remove-from-library', game)"
                  >
                    <i v-if="deletingKey === getGameKey(game)" class="fa-solid fa-circle-notch fa-spin"></i>
                    <i v-else class="fa-solid fa-trash"></i>
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Table 2: Intercepted & Cached Games (Underneath Library) -->
    <div class="card">
      <div class="card-header">
        <div>
          <h3>Cached Games</h3>
          <span class="text-xs text-muted">Games intercepted through the proxy cache, not yet explicitly added to your library</span>
        </div>
        <button class="btn btn-secondary btn-xs" @click="$emit('refresh-cached')">
          <i class="fa-solid fa-rotate mr-1"></i> Refresh
        </button>
      </div>

      <div class="table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th style="width: 50px;">Store</th>
              <th>App ID</th>
              <th>Title</th>
              <th>Size</th>
              <th>Last Requested</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="cachedGames.length === 0">
              <td colspan="6" class="empty-cell">
                No games cached yet, they'll appear here when you start downloading in Steam or Epic Games
              </td>
            </tr>
            <tr v-for="item in cachedGames" :key="(item.service || 'steam') + item.app_id + item.name">
              <td>
                <div 
                  class="store-logo-box" 
                  :class="'store-logo-' + (getLogoKey(item.service) || 'generic')"
                  :title="item.service ? item.service.toUpperCase() : 'Steam'"
                >
                  <svg 
                    v-if="platformLogos[getLogoKey(item.service)]" 
                    class="store-svg-logo" 
                    :viewBox="platformLogos[getLogoKey(item.service)].viewBox" 
                    fill="currentColor"
                  >
                    <path :d="platformLogos[getLogoKey(item.service)].path" />
                  </svg>
                  <i v-else class="fa-solid fa-gamepad fallback-logo-icon"></i>
                </div>
              </td>
              <td><code>{{ item.app_id || '—' }}</code></td>
              <td>
                <div class="game-cell">
                  <strong>{{ item.name }}</strong>
                  <span v-if="item.platform" class="platform-tag" :title="item.platform">
                    <i :class="getPlatformIcon(item.platform)"></i>
                  </span>
                </div>
              </td>
              <td>
                <div class="size-cell">
                  <span class="depot-size">{{ formatBytes(item.cached_bytes) }}</span>
                  <span v-if="item.cached_chunks" class="chunk-badge">{{ item.cached_chunks }} chunks</span>
                  <span v-if="item.total_size > 0 && item.total_size !== item.cached_bytes" class="text-xs text-muted block w-full mt-0.5">
                    Depot: {{ formatBytes(item.total_size) }}
                  </span>
                </div>
              </td>
              <td class="text-muted">{{ formatDate(item.last_seen) }}</td>
              <td>
                <div class="table-actions">
                  <button 
                    class="btn btn-xs btn-primary add-library-btn"
                    title="Add this intercepted game to your managed library"
                    @click="$emit('add-to-library', item)"
                  >
                    <i class="fa-solid fa-plus mr-1"></i> Add to Library
                  </button>
                  <button 
                    class="btn btn-xs btn-outline-danger"
                    :disabled="deletingKey === getGameKey(item)"
                    title="Delete cached game and purge chunks"
                    @click="$emit('delete-cached-game', item)"
                  >
                    <i v-if="deletingKey === getGameKey(item)" class="fa-solid fa-circle-notch fa-spin"></i>
                    <i v-else class="fa-solid fa-trash"></i>
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
import { 
  formatBytes, 
  formatDate, 
  getLogoKey, 
  getPlatformIcon,
  getPlatformList
} from '../utils/formatters';
import { platformLogos } from '../utils/logos';

defineProps({
  library: {
    type: Array,
    required: true,
  },
  cachedGames: {
    type: Array,
    default: () => [],
  },
  deletingKey: {
    type: String,
    default: null,
  },
  recheckingKey: {
    type: String,
    default: null,
  },
});

defineEmits(['open-add-game', 'trigger-prefill', 'recheck-game', 'add-to-library', 'remove-from-library', 'refresh-cached', 'delete-cached-game', 'toggle-game-autoupdate']);

function getGameKey(game) {
  if (!game) return '';
  return `${game.service || 'steam'}:${game.app_id || 0}:${game.name || ''}`;
}
</script>

<style scoped>
.badge-toggle-btn {
  background: transparent;
  border: none;
  padding: 0;
  cursor: pointer;
  outline: none;
}

.badge-toggle-btn:hover .badge {
  opacity: 0.85;
  transform: scale(1.05);
}

.empty-cell {
  text-align: center;
  padding: 2.5rem 1rem !important;
  color: var(--text-muted);
  font-style: italic;
}

.game-cell {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
}

.platform-tag {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  background: rgba(15, 23, 42, 0.85);
  border: 1px solid var(--border);
  color: var(--text-secondary);
  font-size: 0.75rem;
  border-radius: 5px;
  transition: all 0.2s ease;
  cursor: default;
}

.platform-tag:hover {
  color: var(--text-primary);
  border-color: rgba(255, 255, 255, 0.25);
  background: rgba(30, 41, 59, 0.9);
}

.platform-tags-wrap {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  flex-wrap: wrap;
}

.size-cell {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  flex-wrap: wrap;
}

.depot-size {
  font-family: var(--font-mono, monospace);
  font-weight: 600;
  color: var(--text-secondary);
}

.chunk-badge {
  font-size: 0.7rem;
  padding: 0.12rem 0.45rem;
  border-radius: 9999px;
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: var(--text-secondary);
  font-family: var(--font-mono, monospace);
}

.table-actions {
  display: flex;
  align-items: center;
  gap: 0.4rem;
}

.add-library-btn {
  font-weight: 600;
  display: inline-flex;
  align-items: center;
}

.store-logo-box {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: 7px;
  cursor: default;
}

.store-svg-logo {
  width: 17px;
  height: 17px;
}

.store-logo-steam {
  background: radial-gradient(circle at top left, rgba(102, 192, 244, 0.25), rgba(23, 26, 38, 0.8));
  color: #66c0f4;
  border: 1px solid rgba(102, 192, 244, 0.35);
}

.store-logo-epic {
  background: radial-gradient(circle at top left, rgba(255, 255, 255, 0.2), rgba(30, 30, 30, 0.8));
  color: #f8fafc;
  border: 1px solid rgba(255, 255, 255, 0.3);
}

.store-logo-blizzard {
  background: radial-gradient(circle at top left, rgba(0, 174, 255, 0.25), rgba(15, 23, 42, 0.8));
  color: #00aeff;
  border: 1px solid rgba(0, 174, 255, 0.35);
}

.store-logo-riot {
  background: radial-gradient(circle at top left, rgba(235, 0, 41, 0.25), rgba(20, 10, 15, 0.8));
  color: #eb0029;
  border: 1px solid rgba(235, 0, 41, 0.35);
}

.store-logo-xbox {
  background: radial-gradient(circle at top left, rgba(44, 162, 67, 0.25), rgba(10, 25, 15, 0.8));
  color: #2ca243;
  border: 1px solid rgba(44, 162, 67, 0.35);
}

.store-logo-generic {
  background: rgba(148, 163, 184, 0.15);
  color: #94a3b8;
  border: 1px solid rgba(148, 163, 184, 0.3);
}

.fallback-logo-icon {
  font-size: 0.9rem;
}
</style>
