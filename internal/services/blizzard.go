package services

import (
	"net/http"
	"strings"
)

// BlizzardService handles Battle.net CDN domain matching
type BlizzardService struct {
	domains []string
}

func NewBlizzardService() *BlizzardService {
	return &BlizzardService{
		domains: []string{
			"*.dist.blizzard.com",
			"*.cdn.blizzard.com",
			"level3.blizzard.com",
		},
	}
}

func (s *BlizzardService) Name() string { return "blizzard" }
func (s *BlizzardService) Domains() []string { return s.domains }
func (s *BlizzardService) MatchesHost(host string) bool {
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
func (s *BlizzardService) IsCacheable(req *http.Request) bool {
	return req.Method == http.MethodGet || req.Method == http.MethodHead
}
