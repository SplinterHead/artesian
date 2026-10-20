import { ref, onMounted, onUnmounted } from 'vue';
import { api } from '../services/api';
import { cleanClientIP } from '../utils/formatters';

export function useDownloads() {
  const activeDownloads = ref([]);
  const totalFilesLogged = ref(0);

  let pruneInterval = null;
  let pollInterval = null;

  function getSessionKey(item) {
    if (item.key) return item.key;
    const ip = cleanClientIP(item.client_ip);
    const appID = item.app_id || (item.depot_id ? Math.floor(item.depot_id / 10) * 10 : 0);
    if (appID > 0) return `${ip}:steam:${appID}`;
    const game = (item.game_title || 'game').toLowerCase();
    return `${ip}:steam:${game}`;
  }

  function sortByFirstSeen(a, b) {
    const tA = a.first_seen ? new Date(a.first_seen).getTime() : (a._firstSeenMs || 0);
    const tB = b.first_seen ? new Date(b.first_seen).getTime() : (b._firstSeenMs || 0);
    if (tB !== tA) {
      return tB - tA; // Newest initial start time on top
    }
    return (a.key || '').localeCompare(b.key || '');
  }

  function updateOrInsertSession(sess) {
    if (cleanClientIP(sess.client_ip) === '127.0.0.1' || cleanClientIP(sess.client_ip) === '::1') {
      return;
    }
    sess._lastSeen = Date.now();
    if (!sess.last_active) {
      sess.last_active = new Date().toISOString();
    }
    const key = getSessionKey(sess);
    const idx = activeDownloads.value.findIndex(d => getSessionKey(d) === key || d.key === key);
    if (idx !== -1) {
      const existing = activeDownloads.value[idx];
      const mergedDepots = Array.from(new Set([...(existing.depots || []), ...(sess.depots || [])]));
      mergedDepots.sort((a, b) => a - b);
      const mergedLinksMap = new Map();
      (existing.depot_links || []).forEach(l => mergedLinksMap.set(l.depot_id || l.chunk_id, l));
      (sess.depot_links || []).forEach(l => mergedLinksMap.set(l.depot_id || l.chunk_id, l));

      sess.depots = mergedDepots;
      sess.depot_links = Array.from(mergedLinksMap.values());
      sess.chunk_count = Math.max(sess.chunk_count || 0, existing.chunk_count || 0);
      sess.bytes_transferred = Math.max(sess.bytes_transferred || 0, existing.bytes_transferred || 0);
      sess.total_depot_size = Math.max(sess.total_depot_size || 0, existing.total_depot_size || 0);

      if (existing.platform && existing.platform !== 'PC' && (!sess.platform || sess.platform === 'PC')) {
        sess.platform = existing.platform;
      }

      if (existing.timestamp) {
        sess.timestamp = existing.timestamp;
      }
      if (existing.first_seen && !sess.first_seen) {
        sess.first_seen = existing.first_seen;
      }
      sess._firstSeenMs = existing._firstSeenMs || (sess.first_seen ? new Date(sess.first_seen).getTime() : 0);

      const updated = { ...existing, ...sess };
      activeDownloads.value[idx] = updated;
    } else {
      const nowIso = new Date().toISOString();
      const nowMs = Date.now();
      if (!sess.first_seen) {
        sess.first_seen = nowIso;
      }
      sess._firstSeenMs = sess.first_seen ? new Date(sess.first_seen).getTime() : nowMs;
      if (!sess.timestamp) {
        sess.timestamp = new Date().toLocaleTimeString();
      }
      activeDownloads.value.push(sess);
      activeDownloads.value.sort(sortByFirstSeen);
    }
  }

  function bundleIncomingChunk(evt) {
    if (cleanClientIP(evt.client_ip) === '127.0.0.1' || cleanClientIP(evt.client_ip) === '::1') {
      return;
    }
    const key = getSessionKey(evt);
    const nowStr = new Date().toLocaleTimeString();
    const idx = activeDownloads.value.findIndex(d => getSessionKey(d) === key || d.key === key);

    if (idx !== -1) {
      const s = activeDownloads.value[idx];
      s._lastSeen = Date.now();
      s.last_active = new Date().toISOString();
      if (!s.timestamp) {
        s.timestamp = evt.timestamp || nowStr;
      }
      s.bytes_transferred = (s.bytes_transferred || 0) + (evt.bytes_transferred || 0);
      s.chunk_count = (s.chunk_count || 1) + 1;

      if (evt.depot_id && !(s.depots || []).includes(evt.depot_id)) {
        s.depots = s.depots || [];
        s.depots.push(evt.depot_id);
        s.depots.sort((a, b) => a - b);
        s.depot_links = s.depot_links || [];
        s.depot_links.push({
          depot_id: evt.depot_id,
          lookup_url: evt.lookup_url || `https://steamdb.info/depot/${evt.depot_id}`,
        });
      }

      if (evt.total_depot_size && evt.total_depot_size > 0) {
        s.total_depot_size = Math.max(s.total_depot_size || 0, evt.total_depot_size);
      }
      if (s.game_title && s.game_title.startsWith('Steam App ') && evt.game_title && !evt.game_title.startsWith('Steam App ')) {
        s.game_title = evt.game_title;
      }
      if (evt.platform && evt.platform !== 'PC' && (s.platform === 'PC' || !s.platform)) {
        s.platform = evt.platform;
      }
      // Row position remains locked in place
    } else {
      const baseApp = evt.app_id || (evt.depot_id ? Math.floor(evt.depot_id / 10) * 10 : 0);
      const nowIso = new Date().toISOString();
      const nowMs = Date.now();
      const newSession = {
        key: key,
        _lastSeen: nowMs,
        _firstSeenMs: nowMs,
        first_seen: evt.first_seen || nowIso,
        last_active: nowIso,
        client_ip: evt.client_ip,
        store: 'Steam',
        service: 'steam',
        game_title: evt.game_title || 'Steam Game Client',
        app_id: baseApp,
        platform: evt.platform || 'PC',
        depots: evt.depot_id ? [evt.depot_id] : [],
        depot_links: evt.depot_id ? [{ depot_id: evt.depot_id, lookup_url: evt.lookup_url || `https://steamdb.info/depot/${evt.depot_id}` }] : [],
        bytes_transferred: evt.bytes_transferred || 0,
        total_depot_size: (evt.total_depot_size && evt.total_depot_size > 0) ? evt.total_depot_size : 0,
        chunk_count: 1,
        timestamp: evt.timestamp || nowStr,
      };
      activeDownloads.value.push(newSession);
      activeDownloads.value.sort(sortByFirstSeen);
    }
  }

  function handleDownloadRemoved(key) {
    if (!key) return;
    const idx = activeDownloads.value.findIndex(d => d.key === key || getSessionKey(d) === key);
    if (idx !== -1) {
      activeDownloads.value.splice(idx, 1);
    }
  }

  function handleDownloadsCleared() {
    activeDownloads.value = [];
  }

  async function fetchTraffic() {
    try {
      const data = await api.getTraffic();
      if (Array.isArray(data.downloads)) {
        const now = Date.now();
        const incoming = data.downloads.filter(d => cleanClientIP(d.client_ip) !== '127.0.0.1' && cleanClientIP(d.client_ip) !== '::1');
        const incomingKeys = new Set(incoming.map(d => getSessionKey(d) || d.key));

        // 1. Remove sessions no longer active
        activeDownloads.value = activeDownloads.value.filter(d => incomingKeys.has(getSessionKey(d) || d.key));

        // 2. Update existing in-place or insert new
        let needsSort = false;
        for (const inc of incoming) {
          const key = getSessionKey(inc) || inc.key;
          const idx = activeDownloads.value.findIndex(d => (getSessionKey(d) || d.key) === key);
          if (idx !== -1) {
            const existing = activeDownloads.value[idx];
            activeDownloads.value[idx] = {
              ...existing,
              ...inc,
              first_seen: existing.first_seen || inc.first_seen,
              _firstSeenMs: existing._firstSeenMs || (inc.first_seen ? new Date(inc.first_seen).getTime() : 0),
              timestamp: existing.timestamp || inc.timestamp,
              _lastSeen: inc._lastSeen || (inc.last_active ? new Date(inc.last_active).getTime() : now),
            };
          } else {
            activeDownloads.value.push({
              ...inc,
              first_seen: inc.first_seen || new Date().toISOString(),
              _firstSeenMs: inc.first_seen ? new Date(inc.first_seen).getTime() : now,
              timestamp: inc.timestamp || new Date().toLocaleTimeString(),
              _lastSeen: inc._lastSeen || (inc.last_active ? new Date(inc.last_active).getTime() : now),
            });
            needsSort = true;
          }
        }

        if (needsSort) {
          activeDownloads.value.sort(sortByFirstSeen);
        }
      }
      totalFilesLogged.value = (data.traffic || []).length;
    } catch (e) {
      // Silently handle transient polling error
    }
  }

  async function clearTraffic() {
    activeDownloads.value = [];
    totalFilesLogged.value = 0;
    try {
      await api.clearTraffic();
    } catch (e) {
      console.error('Failed to clear traffic:', e);
    }
  }

  onMounted(() => {
    fetchTraffic();

    // Reconcile with server active downloads list every 4 seconds
    pollInterval = setInterval(fetchTraffic, 4000);

    // Auto-prune inactive, disconnected, cancelled, or completed downloads every second
    pruneInterval = setInterval(() => {
      const now = Date.now();
      activeDownloads.value = activeDownloads.value.filter(d => {
        let lastActiveMs = d._lastSeen;
        if (d.last_active) {
          const parsed = new Date(d.last_active).getTime();
          if (!isNaN(parsed)) {
            lastActiveMs = parsed;
          }
        }
        if (!lastActiveMs) {
          lastActiveMs = now;
        }

        const isComplete = d.total_depot_size > 0 && d.bytes_transferred >= d.total_depot_size;
        // Completed download (transferred full size): remove after 3s grace
        if (isComplete && (now - lastActiveMs) >= 3000) {
          return false;
        }
        // Inactive, disconnected, or cancelled download (no chunks in 12s): remove
        if ((now - lastActiveMs) >= 12000) {
          return false;
        }
        return true;
      });
    }, 1000);
  });

  onUnmounted(() => {
    if (pollInterval) clearInterval(pollInterval);
    if (pruneInterval) clearInterval(pruneInterval);
  });

  return {
    activeDownloads,
    totalFilesLogged,
    getSessionKey,
    updateOrInsertSession,
    bundleIncomingChunk,
    handleDownloadRemoved,
    handleDownloadsCleared,
    fetchTraffic,
    clearTraffic,
  };
}
