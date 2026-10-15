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
    const service = (item.service || item.store || 'generic').toLowerCase();
    if (service.includes('steam')) {
      const appID = item.app_id || (item.depot_id ? Math.floor(item.depot_id / 10) * 10 : 0);
      if (appID > 0) return `${ip}:steam:${appID}`;
    }
    const game = (item.game_title || 'game').toLowerCase();
    return `${ip}:${service}:${game}`;
  }

  function updateOrInsertSession(sess) {
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

      activeDownloads.value.splice(idx, 1);
    }
    activeDownloads.value.unshift(sess);
  }

  function bundleIncomingChunk(evt) {
    const key = getSessionKey(evt);
    const nowStr = new Date().toLocaleTimeString();
    const idx = activeDownloads.value.findIndex(d => getSessionKey(d) === key || d.key === key);

    if (idx !== -1) {
      const s = activeDownloads.value[idx];
      s._lastSeen = Date.now();
      s.last_active = new Date().toISOString();
      s.timestamp = evt.timestamp || nowStr;
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
      } else if (evt.service === 'epic' && evt.lookup_url && (!s.depot_links || s.depot_links.length === 0)) {
        s.depot_links = [{
          chunk_id: evt.chunk_id,
          lookup_url: evt.lookup_url,
        }];
      }

      if (evt.total_depot_size && evt.total_depot_size > 0) {
        s.total_depot_size = Math.max(s.total_depot_size || 0, evt.total_depot_size);
      }
      if (s.game_title && s.game_title.startsWith('Steam App ') && evt.game_title && !evt.game_title.startsWith('Steam App ')) {
        s.game_title = evt.game_title;
      }

      if (idx > 0) {
        activeDownloads.value.splice(idx, 1);
        activeDownloads.value.unshift(s);
      }
    } else {
      const baseApp = evt.app_id || (evt.depot_id ? Math.floor(evt.depot_id / 10) * 10 : 0);
      const newSession = {
        key: key,
        _lastSeen: Date.now(),
        last_active: new Date().toISOString(),
        client_ip: evt.client_ip,
        store: evt.store || (evt.service ? evt.service.toUpperCase() : 'Game CDN'),
        service: evt.service,
        game_title: evt.game_title || 'Game Asset',
        app_id: baseApp,
        platform: evt.platform || 'PC',
        depots: evt.depot_id ? [evt.depot_id] : [],
        depot_links: evt.depot_id ? [{ depot_id: evt.depot_id, lookup_url: evt.lookup_url }] : (evt.lookup_url ? [{ chunk_id: evt.chunk_id, lookup_url: evt.lookup_url }] : []),
        bytes_transferred: evt.bytes_transferred || 0,
        total_depot_size: (evt.total_depot_size && evt.total_depot_size > 0) ? evt.total_depot_size : 0,
        chunk_count: 1,
        timestamp: evt.timestamp || nowStr,
      };
      activeDownloads.value.unshift(newSession);
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
        activeDownloads.value = data.downloads.map(d => ({
          ...d,
          _lastSeen: d._lastSeen || (d.last_active ? new Date(d.last_active).getTime() : now),
        }));
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
