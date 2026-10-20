package epic

import (
	"net/http"
	"strings"
)

// ID returns the unique system identifier for Epic Games
func (a *Adapter) ID() string {
	return "epic"
}

// Name returns the identifier for Epic Games
func (a *Adapter) Name() string {
	return "epic"
}

// DisplayName returns the brand display name
func (a *Adapter) DisplayName() string {
	return "Epic Games"
}

// Company returns the parent company
func (a *Adapter) Company() string {
	return "Epic Games"
}

// Description returns the service capabilities description
func (a *Adapter) Description() string {
	return "Epic Games Launcher OAuth2 API, binary manifest parsing, and chunk caching."
}

// Domains returns DNS interception patterns for Epic Games CDN endpoints
func (a *Adapter) Domains() []string {
	return a.domains
}

// MatchesHost determines if the given host belongs to an Epic Games CDN distribution host
func (a *Adapter) MatchesHost(host string) bool {
	host = strings.ToLower(host)

	// Direct match against known hosts
	if host == "download.epicgames.com" ||
		host == "fastly-download.epicgames.com" ||
		strings.HasSuffix(host, ".download.epicgames.com") {
		return true
	}

	// Match Akamai CDN hosts (e.g. epicgames-download1.akamaized.net)
	if strings.Contains(host, "epicgames-download") && strings.HasSuffix(host, ".akamaized.net") {
		return true
	}

	for _, pattern := range a.domains {
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(host, suffix) {
				return true
			}
		} else if host == pattern {
			return true
		}
	}

	return false
}

// IsCacheable verifies whether the HTTP request is a cacheable Epic chunk or manifest asset
func (a *Adapter) IsCacheable(req *http.Request) bool {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}
	path := strings.ToLower(req.URL.Path)
	return strings.HasSuffix(path, ".chunk") || strings.HasSuffix(path, ".manifest")
}
