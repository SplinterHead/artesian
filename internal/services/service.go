package services

import (
	"strings"
	"sync"

	"artesian/internal/ports"
)

// Service is an alias for ports.PlatformAdapter
type Service = ports.PlatformAdapter

// Registry manages available game platform adapters
type Registry struct {
	adapters map[string]ports.PlatformAdapter
	disabled map[string]bool
	mu       sync.RWMutex
}

// NewRegistry initializes platform adapter registry
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]ports.PlatformAdapter),
		disabled: make(map[string]bool),
	}
}

// Register adds a platform adapter to the registry
func (r *Registry) Register(adapter ports.PlatformAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.ToLower(adapter.ID())
	r.adapters[key] = adapter
}

// SetEnabled enables or disables CDN request monitoring for a service
func (r *Registry) SetEnabled(name string, enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled == nil {
		r.disabled = make(map[string]bool)
	}
	r.disabled[strings.ToLower(name)] = !enabled
}

// IsEnabled returns whether CDN request monitoring is enabled for a service
func (r *Registry) IsEnabled(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.disabled == nil {
		return true
	}
	return !r.disabled[strings.ToLower(name)]
}

// Get finds a platform adapter by ID or name
func (r *Registry) Get(id string) (ports.PlatformAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[strings.ToLower(id)]
	return a, ok
}

// All returns all registered platform adapters
func (r *Registry) All() []ports.PlatformAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]ports.PlatformAdapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		list = append(list, a)
	}
	return list
}

// Identify matches an incoming host header against registered platform adapters
func (r *Registry) Identify(host string) ports.PlatformAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cleaned := strings.Split(host, ":")[0] // Strip port if present

	for _, adapter := range r.adapters {
		if r.disabled != nil && r.disabled[adapter.ID()] {
			continue
		}
		if adapter.MatchesHost(cleaned) {
			return adapter
		}
	}
	return nil
}

// AllDomains returns an aggregated list of all active domain patterns across adapters
func (r *Registry) AllDomains() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var domains []string
	for _, adapter := range r.adapters {
		if r.disabled != nil && r.disabled[adapter.ID()] {
			continue
		}
		domains = append(domains, adapter.Domains()...)
	}
	return domains
}
