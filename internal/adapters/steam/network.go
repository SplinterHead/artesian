package steam

import (
	"net/http"
	"strings"
)

// ID returns the unique system identifier for Steam
func (a *Adapter) ID() string {
	return "steam"
}

// Name returns the identifier for Steam
func (a *Adapter) Name() string {
	return "steam"
}

// DisplayName returns the human-readable brand name
func (a *Adapter) DisplayName() string {
	return "Steam"
}

// Company returns the parent company
func (a *Adapter) Company() string {
	return "Valve"
}

// Description returns the service capabilities description
func (a *Adapter) Description() string {
	return "Depot manifest decryption, PICS change polling, and high-throughput chunk caching."
}

// Domains returns DNS interception patterns for Steam CDNs
func (a *Adapter) Domains() []string {
	return a.domains
}

// MatchesHost determines if the given host belongs to Steam CDN
func (a *Adapter) MatchesHost(host string) bool {
	host = strings.ToLower(host)
	for _, pattern := range a.domains {
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(host, suffix) || host == strings.TrimPrefix(pattern, "*.") {
				return true
			}
		} else if strings.HasPrefix(pattern, "content*.") {
			if strings.HasPrefix(host, "content") && strings.HasSuffix(host, ".steampowered.com") {
				return true
			}
		} else if host == pattern {
			return true
		}
	}
	return false
}

// IsCacheable verifies whether the HTTP request is a cacheable Steam chunk or manifest asset
func (a *Adapter) IsCacheable(req *http.Request) bool {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}
	path := req.URL.Path
	return strings.Contains(path, "/depot/") ||
		strings.Contains(path, "/chunk/") ||
		strings.Contains(path, "/content/")
}
