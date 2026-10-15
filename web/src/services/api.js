// Centralized API Client for Artesian Backend

async function request(url, options = {}) {
  const config = {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options.headers,
    },
  };

  const response = await fetch(url, config);
  if (!response.ok) {
    const errorText = await response.text();
    throw new Error(errorText || `Request failed with status ${response.status}`);
  }
  return response.json();
}

export const api = {
  // Stats
  async getStats() {
    return request('/api/v1/stats');
  },

  // Services
  async getServices() {
    return request('/api/v1/services');
  },
  async toggleService(id, enabled) {
    return request('/api/v1/services/toggle', {
      method: 'POST',
      body: JSON.stringify({ id, enabled }),
    });
  },

  // Steam Auth
  async getSteamStatus() {
    return request('/api/v1/steam/status');
  },
  async steamLogin(username, password) {
    return request('/api/v1/steam/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    });
  },
  async submitSteam2FA(code) {
    return request('/api/v1/steam/2fa', {
      method: 'POST',
      body: JSON.stringify({ code }),
    });
  },
  async steamLogout() {
    return request('/api/v1/steam/logout', { method: 'POST' });
  },

  // Library & Prefill
  async getLibrary() {
    return request('/api/v1/library');
  },
  async getCachedGames() {
    return request('/api/v1/library/cached');
  },
  async addGame({ appId, name, service = 'steam', autoUpdate = true }) {
    return request('/api/v1/library', {
      method: 'POST',
      body: JSON.stringify({
        app_id: appId,
        service,
        name,
        auto_update: autoUpdate,
      }),
    });
  },
  async removeGame(appId, service = 'steam') {
    return request(`/api/v1/library?app_id=${appId}&service=${service}`, {
      method: 'DELETE',
    });
  },
  async startPrefill({ appId, name, service = 'steam' }) {
    return request('/api/v1/prefill/start', {
      method: 'POST',
      body: JSON.stringify({
        app_id: appId,
        app_name: name,
        service,
      }),
    });
  },

  // Automation / Scheduler
  async getSchedulerRules() {
    return request('/api/v1/scheduler/rules');
  },
  async pollPICS() {
    return request('/api/v1/scheduler/pics/poll', { method: 'POST' });
  },
  async triggerSimulatedPICS(appId = 730) {
    return request(`/api/v1/scheduler/pics/trigger?app_id=${appId}`, { method: 'POST' });
  },

  // Settings / Config
  async getDNSConfig() {
    return request('/api/v1/config/dns');
  },
  async saveDNSConfig(upstreamDns) {
    return request('/api/v1/config/dns', {
      method: 'POST',
      body: JSON.stringify({ upstream_dns: upstreamDns }),
    });
  },
  async getStorageConfig() {
    return request('/api/v1/config/storage');
  },
  async saveStorageConfig(storagePath) {
    return request('/api/v1/config/storage', {
      method: 'POST',
      body: JSON.stringify({ storage_path: storagePath }),
    });
  },

  // Active Downloads & Traffic
  async getTraffic() {
    return request('/api/v1/traffic');
  },
  async clearTraffic() {
    return request('/api/v1/traffic', { method: 'DELETE' });
  },
};
