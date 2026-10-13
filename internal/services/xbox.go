package services

import (
	"net/http"
	"strings"
)

// XboxService handles Xbox / Microsoft Store CDN domain matching
type XboxService struct {
	domains []string
}

func NewXboxService() *XboxService {
	return &XboxService{
		domains: []string{
			"*.assets1.xboxlive.com",
			"*.delivery.mp.microsoft.com",
		},
	}
}

func (s *XboxService) Name() string { return "xbox" }
func (s *XboxService) Domains() []string { return s.domains }
func (s *XboxService) MatchesHost(host string) bool {
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
func (s *XboxService) IsCacheable(req *http.Request) bool {
	return req.Method == http.MethodGet || req.Method == http.MethodHead
}
