package ports

import (
	"context"
	"net/http"
	"time"
)

// AuthState describes the platform login status
type AuthState string

const (
	StateDisconnected AuthState = "disconnected"
	StateConnecting   AuthState = "connecting"
	StateAwaiting2FA  AuthState = "awaiting_2fa"
	StateConnected    AuthState = "connected"
	StateError        AuthState = "error"
)

// AuthStatus represents public platform account session info for the UI
type AuthStatus struct {
	State                 AuthState `json:"state"`
	Username              string    `json:"username"`
	AccountID             string    `json:"account_id,omitempty"`
	ConnectedAt           time.Time `json:"connected_at,omitempty"`
	TokenExpiry           time.Time `json:"token_expiry,omitempty"`
	HasValidAuth          bool      `json:"has_valid_auth"`
	ErrorMessage          string    `json:"error_message,omitempty"`
	HasMobileConfirmation bool      `json:"has_mobile_confirmation"`
	HasDeviceCode         bool      `json:"has_device_code"`
	HasEmailCode          bool      `json:"has_email_code"`
	AssociatedMessage     string    `json:"associated_message,omitempty"`
}

// AccountGame represents an owned game resolved for an account
type AccountGame struct {
	AppID     int64    `json:"app_id"`
	Service   string   `json:"service"`
	Name      string   `json:"name"`
	Platforms []string `json:"platforms"`
}

// AccountGamesResult contains the discovered owned games and account privacy status
type AccountGamesResult struct {
	Games             []AccountGame `json:"games"`
	TotalFound        int           `json:"total_found"`
	PrivacyRestricted bool          `json:"privacy_restricted"`
	Hint              string        `json:"hint,omitempty"`
}

// DepotInfo represents resolved metadata for a depot or game asset
type DepotInfo struct {
	DepotID    int64  `json:"depot_id"`
	AppID      int64  `json:"app_id"`
	GameTitle  string `json:"game_title"`
	Platform   string `json:"platform"`
	TotalSize  int64  `json:"total_size"`
	ManifestID int64  `json:"manifest_id"`
}

// ChunkMeta represents a chunk extracted from a manifest
type ChunkMeta struct {
	ChunkID   string `json:"chunk_id"`
	SizeBytes int64  `json:"size_bytes"`
}

// AppUpdateCheck represents the outcome of a periodic update check
type AppUpdateCheck struct {
	AppID           int64  `json:"app_id"`
	Name            string `json:"name"`
	OldChangeNumber uint64 `json:"old_change_number"`
	NewChangeNumber uint64 `json:"new_change_number"`
	Updated         bool   `json:"updated"`
	ManifestChanged bool   `json:"manifest_changed"`
}

// NetworkPort handles CDN domain matching, request cacheability, and platform identity
type NetworkPort interface {
	ID() string
	Name() string
	DisplayName() string
	Company() string
	Description() string
	Domains() []string
	MatchesHost(host string) bool
	IsCacheable(req *http.Request) bool
}

// AuthPort handles user authentication, 2FA challenges, and session persistence
type AuthPort interface {
	Status() AuthStatus
	Login(username, password string) error
	Submit2FA(code string) error
	Cancel2FA() error
	Logout() error
	GetAccessToken() string
	GetAccountID() string
}

// LibraryPort handles owned games and platform resolution
type LibraryPort interface {
	GetOwnedGames() (*AccountGamesResult, error)
	ResolveAppPlatforms(appID int64, defaultName string) (name string, platforms []string)
}

// MetadataPort handles depot/asset metadata, base app ID, update checks, and app info
type MetadataPort interface {
	ResolveDepot(depotID int64, userAgent string) DepotInfo
	ResolveBaseAppID(depotID int64) int64
	FetchAppInfo(appID int64) error
	CheckAppUpdate(appID int64, currentChangeNumber uint64) (*AppUpdateCheck, error)
}

// ContentPort handles CDN hosts, chunk URLs, manifest resolution, and chunk key parsing
type ContentPort interface {
	GetCDNHosts() []string
	BuildChunkURL(host string, depotID int64, chunkHash string) string
	ParseChunkKey(cacheKey string) (depotID int64, chunkHash string, ok bool)
	DepotLookupURL(depotID int64) string
	ResolveManifestChunks(ctx context.Context, appID, depotID, manifestID int64) ([]ChunkMeta, error)
}

// PlatformAdapter unites all capabilities required for a game platform adapter
type PlatformAdapter interface {
	NetworkPort
	AuthPort
	LibraryPort
	MetadataPort
	ContentPort
}
