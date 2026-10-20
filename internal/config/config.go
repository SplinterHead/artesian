package config

import (
	"strconv"
	"strings"
	"time"

	"artesian/internal/db"
)

// Config encapsulates active configuration options for Artesian
type Config struct {
	Server ServerConfig `json:"server"`
	DNS    DNSConfig    `json:"dns"`
	Proxy  ProxyConfig  `json:"proxy"`
	Cache  CacheConfig  `json:"cache"`
}

// ServerConfig defines the web management UI & API settings
type ServerConfig struct {
	ListenAddr string `json:"listen_addr"` // e.g. ":8080"
}

// DNSConfig controls the embedded DNS forwarder and interception rules
type DNSConfig struct {
	Enabled       bool     `json:"enabled"`
	ListenAddr    string   `json:"listen_addr"`    // e.g. ":53"
	UpstreamDNS   []string `json:"upstream_dns"`   // e.g. ["1.1.1.1:53", "8.8.8.8:53"]
	InterceptIPv4 string   `json:"intercept_ipv4"` // Target proxy IP address
}

// ProxyConfig defines the HTTP reverse proxy parameters
type ProxyConfig struct {
	ListenAddr      string        `json:"listen_addr"` // e.g. ":80"
	ReadTimeout     time.Duration `json:"read_timeout"`
	WriteTimeout    time.Duration `json:"write_timeout"`
	MaxConnsPerHost int           `json:"max_conns_per_host"`
}

// CacheConfig controls disk storage, chunk layout, and retention
type CacheConfig struct {
	StorageDir    string `json:"storage_dir"`    // e.g. "/data/cache"
	ManifestDir   string `json:"manifest_dir"`   // e.g. "/data/manifests"
	DepotCacheDir string `json:"depotcache_dir"` // e.g. "/data/steam_depotcache"
	MaxSizeBytes  int64  `json:"max_size_bytes"`
	MinFreeDisk   int64  `json:"min_free_disk_bytes"`
}

// DefaultConfig returns safe out-of-the-box defaults
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr: ":8080",
		},
		DNS: DNSConfig{
			Enabled:       true,
			ListenAddr:    ":53",
			UpstreamDNS:   []string{"1.1.1.1:53", "8.8.8.8:53"},
			InterceptIPv4: "127.0.0.1",
		},
		Proxy: ProxyConfig{
			ListenAddr:      ":80",
			ReadTimeout:     60 * time.Second,
			WriteTimeout:    60 * time.Second,
			MaxConnsPerHost: 100,
		},
		Cache: CacheConfig{
			StorageDir:    "/data/cache",
			ManifestDir:   "/data/manifests",
			DepotCacheDir: "/data/steam_depotcache",
			MaxSizeBytes:  2 * 1024 * 1024 * 1024 * 1024, // 2 TB
			MinFreeDisk:   50 * 1024 * 1024 * 1024,       // 50 GB
		},
	}
}

// LoadFromDB loads configuration directly from SQLite database settings with safe fallbacks
func LoadFromDB(database *db.DB) *Config {
	cfg := DefaultConfig()
	if database == nil {
		return cfg
	}

	if upstreams := database.GetSetting("dns_upstreams", ""); upstreams != "" {
		parts := strings.Split(upstreams, ",")
		var cleaned []string
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				if !strings.Contains(s, ":") {
					s += ":53"
				}
				cleaned = append(cleaned, s)
			}
		}
		if len(cleaned) > 0 {
			cfg.DNS.UpstreamDNS = cleaned
		}
	}

	if listen := database.GetSetting("dns_listen_addr", ""); listen != "" {
		cfg.DNS.ListenAddr = listen
	}
	if enabled := database.GetSetting("dns_enabled", ""); enabled != "" {
		cfg.DNS.Enabled = (enabled == "true" || enabled == "1")
	}
	if ip := database.GetSetting("dns_intercept_ipv4", ""); ip != "" {
		cfg.DNS.InterceptIPv4 = ip
	}
	if storage := database.GetSetting("storage_path", ""); storage != "" {
		cfg.Cache.StorageDir = storage
	}
	if manifest := database.GetSetting("manifest_dir", ""); manifest != "" {
		cfg.Cache.ManifestDir = manifest
	}
	if depotcache := database.GetSetting("depotcache_dir", ""); depotcache != "" {
		cfg.Cache.DepotCacheDir = depotcache
	}
	if sListen := database.GetSetting("server_listen_addr", ""); sListen != "" {
		cfg.Server.ListenAddr = sListen
	}
	if pListen := database.GetSetting("proxy_listen_addr", ""); pListen != "" {
		cfg.Proxy.ListenAddr = pListen
	}
	if maxBytes := database.GetSetting("cache_max_size_bytes", ""); maxBytes != "" {
		if v, err := strconv.ParseInt(maxBytes, 10, 64); err == nil && v > 0 {
			cfg.Cache.MaxSizeBytes = v
		}
	}
	if minFree := database.GetSetting("cache_min_free_disk", ""); minFree != "" {
		if v, err := strconv.ParseInt(minFree, 10, 64); err == nil && v > 0 {
			cfg.Cache.MinFreeDisk = v
		}
	}

	return cfg
}
