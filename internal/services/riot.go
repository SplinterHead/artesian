package services

import (
	"net/http"
	"strings"
)

// RiotService handles Riot Games CDN domain matching
type RiotService struct {
	domains []string
}

func NewRiotService() *RiotService {
	return &RiotService{
		domains: []string{
			"*.dyn.riotcdn.net",
			"*.content.riotgames.com",
		},
	}
}

func (s *RiotService) Name() string { return "riot" }
func (s *RiotService) Domains() []string { return s.domains }
func (s *RiotService) MatchesHost(host string) bool {
	host = strings.ToLower(host)
	for _, p := range s.domains {
		if strings.HasPrefix(p, "*.") {
			if strings.HasSuffix(host, strings.TrimPrefix(p, "*")) || host == strings.TrimPrefix(p, "*.") {
				return true
			}
		} else if host == p {
			return true
		}
	}
	return false
}
func (s *RiotService) IsCacheable(req *http.Request) bool {
	return req.Method == http.MethodGet || req.Method == http.MethodHead
}
