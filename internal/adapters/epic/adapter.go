package epic

import (
	"context"
	"hash/fnv"
	"net"
	"net/http"
	"sync"
	"time"

	"artesian/internal/cache"
	"artesian/internal/config"
	"artesian/internal/db"
	"artesian/internal/ports"
)

// EpicGameInfo caches metadata for an owned Epic asset/game
type EpicGameInfo struct {
	AppID         int64    `json:"app_id"`
	AppName       string   `json:"app_name"`
	CatalogItemID string   `json:"catalog_item_id"`
	Namespace     string   `json:"namespace"`
	Title         string   `json:"title"`
	BuildVersion  string   `json:"build_version"`
	Platforms     []string `json:"platforms"`
	ManifestURI   string   `json:"manifest_uri,omitempty"`
	BaseDir       string   `json:"base_dir,omitempty"`
	TotalSize     int64    `json:"total_size,omitempty"`
}

// Adapter implements ports.PlatformAdapter for Epic Games
type Adapter struct {
	db            *db.DB
	storage       *cache.Storage
	dnsCfg        *config.DNSConfig
	httpClient    *http.Client
	cdnHTTPClient *http.Client
	domains       []string

	// Auth state
	state        ports.AuthState
	username     string
	accountID    string
	accessToken  string
	refreshToken string
	tokenExpiry  time.Time
	errorMessage string
	authMu       sync.RWMutex

	// Game and manifest catalog caches
	gamesCache      map[int64]*EpicGameInfo
	gamesByName     map[string]*EpicGameInfo
	gamesMu         sync.RWMutex
	manifests       map[int64]*ParsedManifest
	manifestsMu     sync.RWMutex
	chunkURLs       map[string]string // chunkID -> full download URL
	chunkURLsMu     sync.RWMutex
}

var (
	_ ports.PlatformAdapter = (*Adapter)(nil)
)

// HashAppName creates a stable positive 63-bit int64 from an Epic appName
func HashAppName(appName string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(appName))
	val := int64(h.Sum64() & 0x7FFFFFFFFFFFFFFF)
	if val <= 0 {
		return 1
	}
	return val
}

// NewAdapter initializes the Epic Games platform adapter
func NewAdapter(database *db.DB, storage *cache.Storage, dnsCfg *config.DNSConfig) *Adapter {
	baseHTTPClient := &http.Client{
		Timeout: 20 * time.Second,
	}

	// CDN HTTP client configured with direct upstream DNS resolution to prevent interception loopbacks
	var cdnTransport *http.Transport
	if dnsCfg != nil && len(dnsCfg.UpstreamDNS) > 0 {
		upstreamServer := dnsCfg.UpstreamDNS[0]
		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			Resolver: &net.Resolver{
				PreferGo: true,
				Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
					d := net.Dialer{Timeout: 5 * time.Second}
					return d.DialContext(ctx, "udp", upstreamServer)
				},
			},
		}
		cdnTransport = &http.Transport{
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	} else {
		cdnTransport = &http.Transport{
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}

	cdnClient := &http.Client{
		Transport: cdnTransport,
		Timeout:   45 * time.Second,
	}

	adapter := &Adapter{
		db:            database,
		storage:       storage,
		dnsCfg:        dnsCfg,
		httpClient:    baseHTTPClient,
		cdnHTTPClient: cdnClient,
		domains: []string{
			"epicgames-download*.akamaized.net",
			"download.epicgames.com",
			"fastly-download.epicgames.com",
			"*.download.epicgames.com",
			"epicgames-download1.akamaized.net",
			"epicgames-download2.akamaized.net",
			"epicgames-download3.akamaized.net",
			"epicgames-download4.akamaized.net",
		},
		state:       ports.StateDisconnected,
		gamesCache:  make(map[int64]*EpicGameInfo),
		gamesByName: make(map[string]*EpicGameInfo),
		manifests:   make(map[int64]*ParsedManifest),
		chunkURLs:   make(map[string]string),
	}

	adapter.restoreSavedSession()
	return adapter
}
