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
  async toggleServiceAutoUpdate(id, autoUpdate) {
    return request('/api/v1/services/auto-update', {
      method: 'POST',
      body: JSON.stringify({ id, auto_update: autoUpdate }),
    });
  },
  async getAvailableGames(service = 'steam', includeGamePass = false) {
    let url = `/api/v1/services/available-games?service=${encodeURIComponent(service)}`;
    if (includeGamePass) {
      url += '&include_game_pass=true';
    }
    return request(url);
  },
  async getGamePlatforms(service, appId) {
    return request(
      `/api/v1/games/platforms?service=${encodeURIComponent(service)}&app_id=${encodeURIComponent(appId)}`
    );
  },

  // Platform Authentication
  async getAuthStatus(service = 'steam') {
    return request(`/api/v1/auth/status?service=${encodeURIComponent(service)}`);
  },
  async login(service = 'steam', username, password) {
    return request('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ service, username, password }),
    });
  },
  async submit2FA(service = 'steam', code) {
    return request('/api/v1/auth/2fa', {
      method: 'POST',
      body: JSON.stringify({ service, code }),
    });
  },
  async logout(service = 'steam') {
    return request('/api/v1/auth/logout', {
      method: 'POST',
      body: JSON.stringify({ service }),
    });
  },
  // Backward convenience methods for Steam
  async getSteamStatus() {
    return this.getAuthStatus('steam');
  },
  async steamLogin(username, password) {
    return this.login('steam', username, password);
  },
  async submitSteam2FA(code) {
    return this.submit2FA('steam', code);
  },
  async steamLogout() {
    return this.logout('steam');
  },

  // Convenience methods for Xbox
  async getXboxStatus() {
    return this.getAuthStatus('xbox');
  },
  async xboxLogin(token = '') {
    return this.login('xbox', '', token);
  },
  async xboxLogout() {
    return this.logout('xbox');
  },

  // Library & Prefill
  async getLibrary() {
    return request('/api/v1/library');
  },
  async getCachedGames() {
    return request('/api/v1/library/cached');
  },
  async deleteCachedGame(appId, service = 'steam') {
    return request(`/api/v1/library/cached?app_id=${appId}&service=${service}`, {
      method: 'DELETE',
    });
  },
  async addGame({
    appId,
    name,
    service = 'steam',
    platform = '',
    autoUpdate = true,
    replacePlatforms = false,
  }) {
    return request('/api/v1/library', {
      method: 'POST',
      body: JSON.stringify({
        app_id: appId,
        service,
        name,
        platform,
        auto_update: autoUpdate,
        replace_platforms: replacePlatforms,
      }),
    });
  },
  async removeGame(appId, service = 'steam') {
    return request(`/api/v1/library?app_id=${appId}&service=${service}`, {
      method: 'DELETE',
    });
  },
  async updateGameAutoUpdate(appId, service = 'steam', autoUpdate) {
    return request('/api/v1/library', {
      method: 'PATCH',
      body: JSON.stringify({
        app_id: appId,
        service,
        auto_update: autoUpdate,
      }),
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
  async updateSchedulerRule(id, cronExpression, enabled) {
    return request('/api/v1/scheduler/rules', {
      method: 'POST',
      body: JSON.stringify({
        id,
        cron_expression: cronExpression,
        enabled,
      }),
    });
  },
  async checkUpdates() {
    return request('/api/v1/scheduler/check', { method: 'POST' });
  },
  async checkSteamUpdates() {
    return this.checkUpdates();
  },
  async getUpdateLogs() {
    return request('/api/v1/updates/log');
  },

  // Settings / Config
  async getDNSConfig() {
    return request('/api/v1/config/dns');
  },
  async saveDNSConfig(upstreamDns, interceptIpv4) {
    return request('/api/v1/config/dns', {
      method: 'POST',
      body: JSON.stringify({
        upstream_dns: upstreamDns,
        intercept_ipv4: interceptIpv4,
      }),
    });
  },
  async getStorageConfig() {
    return request('/api/v1/config/storage');
  },
  async saveStorageConfig(storagePath, maxSizeBytes) {
    return request('/api/v1/config/storage', {
      method: 'POST',
      body: JSON.stringify({
        storage_path: storagePath,
        max_size_bytes: maxSizeBytes,
      }),
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
