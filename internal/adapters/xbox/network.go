package xbox

import (
	"net/http"
	"strings"
)

// ID returns the unique platform adapter identifier
func (a *Adapter) ID() string {
	return "xbox"
}

// Name returns the service machine key
func (a *Adapter) Name() string {
	return "xbox"
}

// DisplayName returns the human-friendly store title
func (a *Adapter) DisplayName() string {
	return "Xbox / Microsoft Store"
}

// Company returns the platform owner
func (a *Adapter) Company() string {
	return "Microsoft"
}

// Description describes the platform content subsystem
func (a *Adapter) Description() string {
	return "Xbox network & Microsoft Store content delivery network"
}

// Domains returns active wildcard CDN host patterns
func (a *Adapter) Domains() []string {
	return a.domains
}

// MatchesHost checks whether an incoming request host targets Microsoft/Xbox CDN
func (a *Adapter) MatchesHost(host string) bool {
	cleaned := strings.ToLower(strings.TrimSpace(host))
	if idx := strings.Index(cleaned, ":"); idx != -1 {
		cleaned = cleaned[:idx]
	}

	for _, d := range a.domains {
		if strings.HasPrefix(d, "*.") {
			suffix := d[1:] // e.g. ".assets1.xboxlive.com"
			if strings.HasSuffix(cleaned, suffix) {
				return true
			}
		} else if strings.EqualFold(cleaned, d) {
			return true
		}
	}
	return false
}

// IsCacheable checks if an HTTP request is a game package archive, slice, or metadata file
func (a *Adapter) IsCacheable(req *http.Request) bool {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}

	path := strings.ToLower(req.URL.Path)

	// Game archives and piece hash descriptors
	if strings.HasSuffix(path, ".msixvc") ||
		strings.HasSuffix(path, ".xvc") ||
		strings.HasSuffix(path, ".phf") ||
		strings.HasSuffix(path, ".ecd") ||
		strings.HasSuffix(path, ".appx") ||
		strings.HasSuffix(path, ".msix") ||
		strings.HasSuffix(path, ".cab") ||
		strings.HasSuffix(path, ".esd") ||
		strings.HasSuffix(path, ".psf") {
		return true
	}

	// Delivery Optimization and package stream endpoints
	if strings.Contains(path, "/filestream/") ||
		strings.Contains(path, "/tlu/") ||
		strings.Contains(path, "/pvd/") ||
		strings.Contains(path, "/msdownload/") {
		return true
	}

	return false
}
