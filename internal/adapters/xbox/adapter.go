package xbox

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

// CachedPackage holds resolved package files and CDN endpoints for an Xbox game
type CachedPackage struct {
	ProductID            string   `json:"product_id"`
	ContentID            string   `json:"content_id"`
	WuCategoryID         string   `json:"wu_category_id"`
	Title                string   `json:"title"`
	Platforms            []string `json:"platforms"`
	CdnRoots             []string `json:"cdn_roots"`
	BasePackageURL       string   `json:"base_package_url"`        // .msixvc or .xvc relative path
	PieceHashFileURL     string   `json:"piece_hash_file_url"`     // .phf relative path
	ContentDescriptorURL string   `json:"content_descriptor_url"` // .ecd relative path
	PackageSize          int64    `json:"package_size"`            // Total byte size of the monolithic archive
	PhfSize              int64    `json:"phf_size"`
	EcdSize              int64    `json:"ecd_size"`
	ResolvedAt           time.Time
}

// Adapter implements ports.PlatformAdapter for Xbox and the Microsoft Store
type Adapter struct {
	db            *db.DB
	storage       *cache.Storage
	dnsCfg        *config.DNSConfig
	httpClient    *http.Client
	cdnHTTPClient *http.Client
	domains       []string

	// Auth state
	authMu        sync.RWMutex
	state         ports.AuthState
	gamertag      string
	xuid          string
	userHash      string // uhs
	msaToken      string // OAuth2 access token
	msaRefresh    string // OAuth2 refresh token
	xauToken      string // Xbox Live User Token
	mpXstsToken   string // Scoped to http://mp.microsoft.com/
	xblXstsToken  string // Scoped to http://xboxlive.com
	updateXstsToken string // Scoped to http://update.xboxlive.com
	tokenExpiry   time.Time
	userCode      string
	verificationURL string
	expiresIn     int
	cancelPoll    context.CancelFunc
	errorMessage  string

	// Package & Catalog Caches
	pkgMu       sync.RWMutex
	packageMap  map[int64]*CachedPackage // Keyed by appID (positive int64 hash)
	prodToAppID map[string]int64         // ProductID -> appID
	appToProdID map[int64]string         // appID -> ProductID
}

var (
	_ ports.PlatformAdapter  = (*Adapter)(nil)
	_ ports.CatalogFilterPort = (*Adapter)(nil)
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

// HashProductID generates a stable positive int64 for a Microsoft Product ID or Content ID.
// It is constrained to 53 bits (0x001FFFFFFFFFFFFF) to guarantee exact IEEE-754 integer precision in JavaScript (Number.MAX_SAFE_INTEGER = 9007199254740991).
func HashProductID(productID string) int64 {
	h := fnv.New64a()
	h.Write([]byte(strings.ToUpper(strings.TrimSpace(productID))))
	val := int64(h.Sum64() & 0x001FFFFFFFFFFFFF)
	if val == 0 {
		return 1
	}
	return val
}

// NewAdapter initializes the Xbox / Microsoft Store platform adapter
func NewAdapter(database *db.DB, storage *cache.Storage, dnsCfg *config.DNSConfig) *Adapter {
	baseHTTPClient := &http.Client{
		Transport: createUpstreamTransport(dnsCfg),
		Timeout:   25 * time.Second,
	}

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
			"*.assets1.xboxlive.com",
			"*.xvcf1.xboxlive.com",
			"*.assets2.xboxlive.com",
			"*.xvcf2.xboxlive.com",
			"assets1.xboxlive.com",
			"xvcf1.xboxlive.com",
			"assets2.xboxlive.com",
			"xvcf2.xboxlive.com",
			"*.dl.delivery.mp.microsoft.com",
			"dl.delivery.mp.microsoft.com",
			"tlu.dl.delivery.mp.microsoft.com",
			"*.delivery.mp.microsoft.com",
			"*.delivery.mp.microsoft.com.c.footprint.net",
			"*.update.microsoft.com",
			"*.windowsupdate.com",
			"*.mp.microsoft.com",
		},
		state:       ports.StateDisconnected,
		packageMap:  make(map[int64]*CachedPackage),
		prodToAppID: make(map[string]int64),
		appToProdID: make(map[int64]string),
	}

	adapter.restoreSavedSession()
	return adapter
}
