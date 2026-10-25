<template>
  <div class="tab-view">
    <!-- Table 1: Monitored Game Library -->
    <div class="card mb-4">
      <div class="card-header">
        <div>
          <h3>Game Library</h3>
          <span class="text-xs text-muted"
            >Explicitly monitored games configured for automated caching and prefilling</span
          >
        </div>
        <button class="btn btn-primary btn-sm" @click="$emit('open-add-game')">+ Add Game</button>
      </div>

      <div class="table-wrap">
        <table class="data-table library-table">
          <colgroup>
            <col class="col-store" />
            <col class="col-title" />
            <col class="col-status" />
            <col class="col-autoupdate" />
            <col class="col-size" />
            <col class="col-sync" />
            <col class="col-actions" />
          </colgroup>
          <thead>
            <tr>
              <th class="col-store">Store</th>
              <th class="col-title">Title</th>
              <th class="col-status">Status</th>
              <th class="col-autoupdate">Auto-Update</th>
              <th class="col-size">Size</th>
              <th class="col-sync">Last Sync</th>
              <th class="col-actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="library.length === 0">
              <td colspan="7" class="empty-cell">
                No games added to library yet. Add a game above or select from cached games below.
              </td>
            </tr>
            <tr v-for="game in library" :key="(game.service || 'steam') + game.app_id">
              <td class="col-store">
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
              <td class="col-title">
                <div class="game-cell">
                  <strong class="game-title-text" :title="game.name">{{ game.name }}</strong>
                  <span v-if="game.platform" class="platform-tags-wrap">
                    <span
                      v-for="p in getPlatformList(game.platform)"
                      :key="p"
                      class="platform-tag"
                      :title="p"
                    >
                      <svg
                        v-if="isXboxPlatform(p)"
                        class="platform-tag-svg"
                        :viewBox="platformLogos.xbox.viewBox"
                        aria-hidden="true"
                      >
                        <path
                          :d="platformLogos.xbox.path"
                          :fill="platformLogos.xbox.fill || '#107c10'"
                        />
                      </svg>
                      <i v-else :class="getPlatformIcon(p)"></i>
                    </span>
                  </span>
                </div>
              </td>
              <td class="col-status">
                <span :class="['status-badge', game.status]">
                  {{ game.status.toUpperCase() }}
                </span>
              </td>
              <td class="col-autoupdate">
                <button
                  type="button"
                  class="badge-toggle-btn"
                  :title="
                    'Click to turn ' +
                    (game.auto_update ? 'off' : 'on') +
                    ' auto-update for this title'
                  "
                  @click="$emit('toggle-game-autoupdate', game)"
                >
                  <span class="badge" :class="game.auto_update ? 'badge-hit' : 'badge-secondary'">
                    {{ game.auto_update ? 'ENABLED' : 'OFF' }}
                  </span>
                </button>
              </td>
              <td class="col-size">
                <div class="size-cell">
                  <span class="depot-size">{{ formatBytes(game.size_bytes) }}</span>
                </div>
              </td>
              <td class="col-sync text-muted">{{ formatDate(game.last_checked) }}</td>
              <td class="col-actions">
                <div class="table-actions">
                  <button
                    class="btn btn-xs btn-outline-primary btn-icon-xs"
                    :disabled="
                      recheckingKey === getGameKey(game) || deletingKey === getGameKey(game)
                    "
                    title="Re-check and sync chunks against depot"
                    @click="$emit('recheck-game', game)"
                  >
                    <i
                      v-if="recheckingKey === getGameKey(game)"
                      class="fa-solid fa-circle-notch fa-spin"
                    ></i>
                    <i v-else class="fa-solid fa-rotate-right"></i>
                  </button>
                  <button
                    class="btn btn-xs btn-outline-danger btn-icon-xs"
                    :disabled="deletingKey === getGameKey(game)"
                    title="Remove game from library"
                    @click="$emit('remove-from-library', game)"
                  >
                    <i
                      v-if="deletingKey === getGameKey(game)"
                      class="fa-solid fa-circle-notch fa-spin"
                    ></i>
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
          <span class="text-xs text-muted"
            >Games intercepted through the proxy cache, not yet explicitly added to your
            library</span
          >
        </div>
        <button class="btn btn-secondary btn-xs" @click="$emit('refresh-cached')">
          <i class="fa-solid fa-rotate mr-1"></i> Refresh
        </button>
      </div>

      <div class="table-wrap">
        <table class="data-table cached-table">
          <colgroup>
            <col class="col-store" />
            <col class="col-title" />
            <col class="col-size" />
            <col class="col-sync" />
            <col class="col-actions" />
          </colgroup>
          <thead>
            <tr>
              <th class="col-store">Store</th>
              <th class="col-title">Title</th>
              <th class="col-size">Size</th>
              <th class="col-sync">Last Requested</th>
              <th class="col-actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="cachedGames.length === 0">
              <td colspan="5" class="empty-cell">
                No games cached yet, they'll appear here when you start downloading in Steam
              </td>
            </tr>
            <tr
              v-for="item in cachedGames"
              :key="(item.service || 'steam') + item.app_id + item.name"
            >
              <td class="col-store">
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
              <td class="col-title">
                <div class="game-cell">
                  <strong class="game-title-text" :title="item.name">{{ item.name }}</strong>
                  <span v-if="item.platform" class="platform-tags-wrap">
                    <span
                      v-for="p in getPlatformList(item.platform)"
                      :key="p"
                      class="platform-tag"
                      :title="p"
                    >
                      <svg
                        v-if="isXboxPlatform(p)"
                        class="platform-tag-svg"
                        :viewBox="platformLogos.xbox.viewBox"
                        aria-hidden="true"
                      >
                        <path
                          :d="platformLogos.xbox.path"
                          :fill="platformLogos.xbox.fill || '#107c10'"
                        />
                      </svg>
                      <i v-else :class="getPlatformIcon(p)"></i>
                    </span>
                  </span>
                </div>
              </td>
              <td class="col-size">
                <div class="size-cell">
                  <span class="depot-size">{{ formatBytes(item.cached_bytes) }}</span>
                </div>
              </td>
              <td class="col-sync text-muted">{{ formatDate(item.last_seen) }}</td>
              <td class="col-actions">
                <div class="table-actions">
                  <button
                    class="btn btn-xs btn-outline-primary btn-icon-xs"
                    title="Add to Library"
                    @click="$emit('add-to-library', item)"
                  >
                    <i class="fa-solid fa-plus"></i>
                  </button>
                  <button
                    class="btn btn-xs btn-outline-danger btn-icon-xs"
                    :disabled="deletingKey === getGameKey(item)"
                    title="Delete cached game and purge chunks"
                    @click="$emit('delete-cached-game', item)"
                  >
                    <i
                      v-if="deletingKey === getGameKey(item)"
                      class="fa-solid fa-circle-notch fa-spin"
                    ></i>
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
  getPlatformList,
  isXboxPlatform,
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

defineEmits([
  'open-add-game',
  'trigger-prefill',
  'recheck-game',
  'add-to-library',
  'remove-from-library',
  'refresh-cached',
  'delete-cached-game',
  'toggle-game-autoupdate',
]);

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
  flex-wrap: nowrap;
  min-width: 0;
}

.game-title-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: inline-block;
  max-width: 280px;
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
  flex-shrink: 0;
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
  flex-wrap: nowrap;
  flex-shrink: 0;
}

.size-cell {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  flex-wrap: nowrap;
  white-space: nowrap;
}

.depot-size {
  font-family: var(--font-mono, monospace);
  font-weight: 600;
  color: var(--text-secondary);
  white-space: nowrap;
}

.library-table,
.cached-table {
  table-layout: fixed;
  width: 100%;
}

.col-store {
  width: 56px;
  text-align: center;
}

.col-title {
  width: auto;
}

.col-status {
  width: 120px;
}

.col-autoupdate {
  width: 110px;
}

.col-size {
  width: 100px;
  white-space: nowrap;
}

.col-sync {
  width: 185px;
  white-space: nowrap;
}

.col-actions {
  width: 80px;
  text-align: right;
}

.table-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 0.4rem;
}

.btn-icon-xs {
  width: 28px;
  height: 28px;
  padding: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  flex-shrink: 0;
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

.store-logo-xbox {
  background: radial-gradient(circle at top left, rgba(16, 124, 16, 0.35), rgba(15, 23, 42, 0.8));
  color: #107c10;
  border: 1px solid rgba(16, 124, 16, 0.45);
}

.store-logo-generic {
  background: rgba(148, 163, 184, 0.15);
  color: #94a3b8;
  border: 1px solid rgba(148, 163, 184, 0.3);
}

.fallback-logo-icon {
  font-size: 0.9rem;
}

.platform-tag-svg {
  width: 12px;
  height: 12px;
  display: block;
}
</style>
