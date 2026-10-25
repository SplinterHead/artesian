package epic

import (
	"context"
	"fmt"
	"hash/fnv"
	"net"
	"net/http"
	"strings"
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
	gamesCache  map[int64]*EpicGameInfo
	gamesByName map[string]*EpicGameInfo
	gamesMu     sync.RWMutex
	manifests   map[int64]*ParsedManifest
	manifestsMu sync.RWMutex
	chunkURLs   map[string]string // chunkID -> full download URL
	chunkURLsMu sync.RWMutex
}

var (
	_ ports.PlatformAdapter = (*Adapter)(nil)
)

// HashAppName creates a stable positive 53-bit int64 from an Epic appName safe for JavaScript JSON numbers
func HashAppName(appName string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(appName))
	val := int64(h.Sum64() & 0x001FFFFFFFFFFFFF)
	if val <= 0 {
		return 1
	}
	return val
}

func createUpstreamTransport(dnsCfg *config.DNSConfig) *http.Transport {
	if dnsCfg != nil && len(dnsCfg.UpstreamDNS) > 0 {
		upstreams := dnsCfg.UpstreamDNS
		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			Resolver: &net.Resolver{
				PreferGo: true,
				Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
					if network == "" {
						network = "udp"
					}
					var lastErr error
					for _, u := range upstreams {
						target := strings.TrimSpace(u)
						if target == "" {
							continue
						}
						if !strings.Contains(target, ":") {
							target += ":53"
						}
						d := net.Dialer{Timeout: 3 * time.Second}
						conn, err := d.DialContext(ctx, network, target)
						if err == nil {
							return conn, nil
						}
						lastErr = err
					}
					if lastErr != nil {
						return nil, lastErr
					}
					return nil, fmt.Errorf("no upstream DNS servers available")
				},
			},
		}
		return &http.Transport{
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}

	return &http.Transport{
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// NewAdapter initializes the Epic Games platform adapter
func NewAdapter(database *db.DB, storage *cache.Storage, dnsCfg *config.DNSConfig) *Adapter {
	// Outgoing API HTTP client configured with direct upstream DNS resolution to bypass any LAN DNS loops
	baseHTTPClient := &http.Client{
		Transport: createUpstreamTransport(dnsCfg),
		Timeout:   25 * time.Second,
	}

	// CDN HTTP client configured with direct upstream DNS resolution
	cdnClient := &http.Client{
		Transport: createUpstreamTransport(dnsCfg),
		Timeout:   45 * time.Second,
	}

	adapter := &Adapter{
		db:            database,
		storage:       storage,
		dnsCfg:        dnsCfg,
		httpClient:    baseHTTPClient,
		cdnHTTPClient: cdnClient,
		domains: []string{
			"download*.epicgames.com",
			"*.download.epicgames.com",
			"cdn*.epicgames.com",
			"*.epicgamescdn.com",
			"epicgames-download*.akamaized.net",
			"*download*.fastly-edge.com",
			"fastly-download.epicgames.com",
			"*.unrealengine.com",
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
