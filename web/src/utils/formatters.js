import { platformLogos } from './logos';

export function formatBytes(bytes) {
  if (!bytes || bytes <= 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  if (i === 0) return bytes + ' B';
  const val = bytes / Math.pow(k, i);
  return val.toFixed(2) + ' ' + sizes[i];
}

export function formatSpeed(bytesPerSec) {
  if (!bytesPerSec) return '0 KB/s';
  const mb = bytesPerSec / (1024 * 1024);
  if (mb >= 1) return mb.toFixed(1) + ' MB/s';
  const kb = bytesPerSec / 1024;
  return kb.toFixed(0) + ' KB/s';
}

export function formatETA(secs) {
  if (!secs || secs <= 0) return 'Complete';
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  return `${m}m ${s}s`;
}

export function formatDate(dt) {
  if (!dt) return 'Never';
  const d = new Date(dt);
  return d.toLocaleDateString() + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

export function cleanClientIP(ip) {
  if (!ip) return '';
  let clean = ip.trim();
  if (clean.startsWith('[')) {
    const end = clean.indexOf(']');
    return end !== -1 ? clean.substring(1, end) : clean;
  }
  const parts = clean.split(':');
  if (parts.length === 2 && !clean.includes('::')) {
    return parts[0];
  }
  return clean;
}

export function formatChunkID(id) {
  if (!id) return '';
  const s = String(id).trim();
  if (s.length > 14) {
    return s.substring(0, 10) + '…';
  }
  return s;
}

export function formatStore(service) {
  switch ((service || '').toLowerCase()) {
    case 'steam': return 'Steam';
    case 'epic': return 'Epic Games';
    case 'blizzard': return 'Battle.net';
    case 'riot': return 'Riot Games';
    case 'xbox': return 'Xbox';
    default: return service ? service.toUpperCase() : 'Game CDN';
  }
}

export function getLogoKey(service, store) {
  const s = (service || '').toLowerCase();
  if (platformLogos[s]) return s;
  const st = (store || '').toLowerCase();
  if (st.includes('steam')) return 'steam';
  if (st.includes('epic')) return 'epic';
  if (st.includes('battle') || st.includes('blizzard')) return 'blizzard';
  if (st.includes('riot') || st.includes('valorant')) return 'riot';
  if (st.includes('xbox')) return 'xbox';
  return null;
}

export function getPlatformIcon(platform) {
  const p = (platform || '').toLowerCase();
  if (p.includes('mac') || p.includes('apple') || p.includes('darwin') || p.includes('osx')) {
    return 'fa-brands fa-apple';
  }
  if (p.includes('linux')) {
    return 'fa-brands fa-linux';
  }
  if (p.includes('win') || p.includes('pc') || p.includes('xbox')) {
    return 'fa-brands fa-windows';
  }
  return 'fa-brands fa-windows';
}

export function getPlatformList(platform) {
  if (!platform) return [];
  if (Array.isArray(platform)) return platform;
  return String(platform)
    .split(',')
    .map(p => p.trim())
    .filter(Boolean);
}

