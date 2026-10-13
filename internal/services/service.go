package services

import (
	"net/http"
	"strings"
	"sync"
)

// Service abstracts game distribution network behavior
type Service interface {
	Name() string
	MatchesHost(host string) bool
	Domains() []string
	IsCacheable(req *http.Request) bool
}

// Registry manages available game platform services
type Registry struct {
	services map[string]Service
	disabled map[string]bool
	mu       sync.RWMutex
}

// NewRegistry initializes service registry
func NewRegistry() *Registry {
	return &Registry{
		services: make(map[string]Service),
		disabled: make(map[string]bool),
	}
}

// Register adds a service to the registry
func (r *Registry) Register(svc Service) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[svc.Name()] = svc
}

// SetEnabled enables or disables CDN request monitoring for a service
func (r *Registry) SetEnabled(name string, enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled == nil {
		r.disabled = make(map[string]bool)
	}
	r.disabled[name] = !enabled
}

// IsEnabled returns whether CDN request monitoring is enabled for a service
func (r *Registry) IsEnabled(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.disabled == nil {
		return true
	}
	return !r.disabled[name]
}

// Get finds a service by name
func (r *Registry) Get(name string) (Service, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	svc, ok := r.services[name]
	return svc, ok
}

// All returns all registered services
func (r *Registry) All() []Service {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]Service, 0, len(r.services))
	for _, s := range r.services {
		list = append(list, s)
	}
	return list
}

// Identify matches an incoming host header against registered services
func (r *Registry) Identify(host string) Service {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cleaned := strings.Split(host, ":")[0] // Strip port if present

	for _, svc := range r.services {
		if r.disabled != nil && r.disabled[svc.Name()] {
			continue
		}
		if svc.MatchesHost(cleaned) {
			return svc
		}
	}
	return nil
}

// AllDomains returns an aggregated list of all active domain patterns across services
func (r *Registry) AllDomains() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var domains []string
	for _, svc := range r.services {
		if r.disabled != nil && r.disabled[svc.Name()] {
			continue
		}
		domains = append(domains, svc.Domains()...)
	}
	return domains
}
