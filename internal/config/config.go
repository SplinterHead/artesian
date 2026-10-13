package config

import (
	"os"
	"time"
)

// Config encapsulates all configuration options for Artesian
type Config struct {
	Server    ServerConfig    `json:"server" yaml:"server"`
	DNS       DNSConfig       `json:"dns" yaml:"dns"`
	Proxy     ProxyConfig     `json:"proxy" yaml:"proxy"`
	Cache     CacheConfig     `json:"cache" yaml:"cache"`
	Database  DatabaseConfig  `json:"database" yaml:"database"`
	Services  ServicesConfig  `json:"services" yaml:"services"`
	Scheduler SchedulerConfig `json:"scheduler" yaml:"scheduler"`
}

// ServerConfig defines the web management UI & API settings
type ServerConfig struct {
	ListenAddr string `json:"listen_addr" yaml:"listen_addr"` // e.g. ":8080"
	EnableAuth bool   `json:"enable_auth" yaml:"enable_auth"`
	APIKey     string `json:"api_key" yaml:"api_key"`
}

// DNSConfig controls the embedded DNS forwarder and interception rules
type DNSConfig struct {
	Enabled       bool     `json:"enabled" yaml:"enabled"`
	ListenAddr    string   `json:"listen_addr" yaml:"listen_addr"`       // e.g. ":53"
	UpstreamDNS   []string `json:"upstream_dns" yaml:"upstream_dns"`     // e.g. ["1.1.1.1:53", "8.8.8.8:53"]
	InterceptIPv4 string   `json:"intercept_ipv4" yaml:"intercept_ipv4"` // Target proxy IP address
	InterceptIPv6 string   `json:"intercept_ipv6" yaml:"intercept_ipv6"`
}

// ProxyConfig defines the HTTP reverse proxy parameters
type ProxyConfig struct {
	ListenAddr      string        `json:"listen_addr" yaml:"listen_addr"` // e.g. ":80"
	ReadTimeout     time.Duration `json:"read_timeout" yaml:"read_timeout"`
	WriteTimeout    time.Duration `json:"write_timeout" yaml:"write_timeout"`
	MaxConnsPerHost int           `json:"max_conns_per_host" yaml:"max_conns_per_host"`
}

// CacheConfig controls disk storage, chunk layout, and retention
type CacheConfig struct {
	StorageDir   string `json:"storage_dir" yaml:"storage_dir"` // e.g. "./data/cache"
	MaxSizeBytes int64  `json:"max_size_bytes" yaml:"max_size_bytes"`
	MinFreeDisk  int64  `json:"min_free_disk_bytes" yaml:"min_free_disk_bytes"`
	ChunkSize    int64  `json:"chunk_size_bytes" yaml:"chunk_size_bytes"`
}

// DatabaseConfig specifies the SQLite persistence database
type DatabaseConfig struct {
	Path string `json:"path" yaml:"path"` // e.g. "./data/artesian.db"
}

// ServicesConfig lists enabled game platforms
type ServicesConfig struct {
	Steam     ServiceSettings `json:"steam" yaml:"steam"`
	EpicGames ServiceSettings `json:"epicgames" yaml:"epicgames"`
	Blizzard  ServiceSettings `json:"blizzard" yaml:"blizzard"`
	Riot      ServiceSettings `json:"riot" yaml:"riot"`
	Xbox      ServiceSettings `json:"xbox" yaml:"xbox"`
}

// ServiceSettings controls a specific platform's behavior
type ServiceSettings struct {
	Enabled      bool     `json:"enabled" yaml:"enabled"`
	Domains      []string `json:"domains" yaml:"domains"`
	AutoUpdate   bool     `json:"auto_update" yaml:"auto_update"`
	PrefillHours int      `json:"prefill_hours" yaml:"prefill_hours"`
}

// SchedulerConfig controls periodic tasks and cron timers
type SchedulerConfig struct {
	Enabled       bool   `json:"enabled" yaml:"enabled"`
	UpdateCron    string `json:"update_cron" yaml:"update_cron"`       // e.g. "0 */4 * * *"
	CacheSyncCron string `json:"cache_sync_cron" yaml:"cache_sync_cron"` // e.g. "0 2 * * *"
}

// DefaultConfig returns safe out-of-the-box defaults
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr: ":8080",
			EnableAuth: false,
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
			StorageDir:   "./data/cache",
			MaxSizeBytes: 2 * 1024 * 1024 * 1024 * 1024, // 2 TB
			MinFreeDisk:  50 * 1024 * 1024 * 1024,       // 50 GB
			ChunkSize:    1024 * 1024,                   // 1 MB
		},
		Database: DatabaseConfig{
			Path: "./data/artesian.db",
		},
		Services: ServicesConfig{
			Steam: ServiceSettings{
				Enabled: true,
				Domains: []string{
					"*.cs.steampowered.com",
					"*.content.steampowered.com",
					"*.hsar.steampowered.com",
					"content*.steampowered.com",
					"*.steamcontent.com",
				},
				AutoUpdate: true,
			},
			EpicGames: ServiceSettings{
				Enabled: true,
				Domains: []string{
					"*.download.epicgames.com",
					"*.ol.epicgames.com",
				},
				AutoUpdate: true,
			},
			Blizzard: ServiceSettings{
				Enabled: true,
				Domains: []string{
					"*.dist.blizzard.com",
					"*.cdn.blizzard.com",
				},
				AutoUpdate: false,
			},
			Riot: ServiceSettings{
				Enabled: true,
				Domains: []string{
					"*.dyn.riotcdn.net",
					"*.content.riotgames.com",
				},
				AutoUpdate: false,
			},
			Xbox: ServiceSettings{
				Enabled: true,
				Domains: []string{
					"*.assets1.xboxlive.com",
					"*.delivery.mp.microsoft.com",
				},
				AutoUpdate: false,
			},
		},
		Scheduler: SchedulerConfig{
			Enabled:       true,
			UpdateCron:    "0 */4 * * *", // Every 4 hours
			CacheSyncCron: "0 3 * * *",   // 3 AM daily
		},
	}
}

// LoadConfig loads configuration from path or returns defaults
func LoadConfig(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	// Future extension: unmarshal YAML/JSON config file
	return DefaultConfig(), nil
}
