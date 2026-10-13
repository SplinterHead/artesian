package services

import (
	"net/http"
	"strings"
)

// EpicService handles Epic Games Store CDN matching and chunk caching
type EpicService struct {
	domains []string
}

// NewEpicService initializes Epic platform service
func NewEpicService() *EpicService {
	return &EpicService{
		domains: []string{
			"*.download.epicgames.com",
			"*.ol.epicgames.com",
		},
	}
}

func (s *EpicService) Name() string {
	return "epic"
}

func (s *EpicService) Domains() []string {
	return s.domains
}

func (s *EpicService) MatchesHost(host string) bool {
	host = strings.ToLower(host)
	for _, p := range s.domains {
		if strings.HasPrefix(p, "*.") {
			if strings.HasSuffix(host, strings.TrimPrefix(p, "*")) {
				return true
			}
		} else if host == p {
			return true
		}
	}
	return false
}

func (s *EpicService) IsCacheable(req *http.Request) bool {
	return req.Method == http.MethodGet || req.Method == http.MethodHead
}
