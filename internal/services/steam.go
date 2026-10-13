package services

import (
	"net/http"
	"strings"
)

// SteamService handles Steam game content delivery interception and caching
type SteamService struct {
	domains []string
}

// NewSteamService initializes Steam platform service
func NewSteamService() *SteamService {
	return &SteamService{
		domains: []string{
			"*.cs.steampowered.com",
			"*.content.steampowered.com",
			"*.hsar.steampowered.com",
			"content*.steampowered.com",
			"*.steamcontent.com",
		},
	}
}

// Name returns the identifier for Steam
func (s *SteamService) Name() string {
	return "steam"
}

// Domains returns DNS interception patterns
func (s *SteamService) Domains() []string {
	return s.domains
}

// MatchesHost determines if the given host belongs to Steam CDN
func (s *SteamService) MatchesHost(host string) bool {
	host = strings.ToLower(host)
	for _, pattern := range s.domains {
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(host, suffix) {
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

// IsCacheable verifies whether the HTTP request is a cacheable Steam chunk/asset
func (s *SteamService) IsCacheable(req *http.Request) bool {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}
	// Steam chunk downloads typically target /depot/<id>/chunk/<hash>
	// or /content/<id>/...
	path := req.URL.Path
	return strings.Contains(path, "/depot/") ||
		strings.Contains(path, "/chunk/") ||
		strings.Contains(path, "/content/")
}
