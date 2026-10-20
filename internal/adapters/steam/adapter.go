package steam

import (
	"context"
	"fmt"
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

// Adapter implements ports.SteamPort providing all Steam subsystem capabilities
type Adapter struct {
	db            *db.DB
	storage       *cache.Storage
	dnsCfg        *config.DNSConfig
	httpClient    *http.Client
	cdnHTTPClient *http.Client
	domains       []string

	// Auth state
	state                 ports.AuthState
	username              string
	password              string
	clientID              string
	requestID             string
	steamID               string
	accessToken           string
	refreshToken          string
	sessionCookies        string
	confirmationType      int
	hasMobileConfirmation bool
	hasDeviceCode         bool
	hasEmailCode          bool
	associatedMsg         string
	cancelPoll            context.CancelFunc
	errorMessage          string
	authMu                sync.RWMutex

	// Metadata and platform caches
	depotInfoCache   map[int64]*ports.DepotInfo
	depotInfoCacheMu sync.RWMutex
	depotFetching    map[int64]bool
	depotFetchingMu  sync.Mutex

	platformCache   map[int64]cachedPlatformMeta
	platformCacheMu sync.RWMutex
}

var (
	_ ports.PlatformAdapter = (*Adapter)(nil)
)

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

// NewAdapter initializes the Steam platform adapter
func NewAdapter(database *db.DB, storage *cache.Storage, dnsCfg *config.DNSConfig) *Adapter {
	// Standard HTTP client for API endpoints using direct upstream DNS
	baseHTTPClient := &http.Client{
		Transport: createUpstreamTransport(dnsCfg),
		Timeout:   20 * time.Second,
	}

	// CDN HTTP client configured with direct upstream DNS resolution to prevent loopbacks
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
			"*.cs.steampowered.com",
			"*.content.steampowered.com",
			"*.hsar.steampowered.com",
			"content*.steampowered.com",
			"*.steamcontent.com",
		},
		state:          ports.StateDisconnected,
		depotInfoCache: make(map[int64]*ports.DepotInfo),
		depotFetching:  make(map[int64]bool),
		platformCache:  make(map[int64]cachedPlatformMeta),
	}

	adapter.restoreSavedSession()
	return adapter
}
