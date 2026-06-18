package provider

import (
	"fmt"
)

// Registry holds all registered providers.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry initializes a registry with default providers.
func NewRegistry() *Registry {
	r := &Registry{
		providers: make(map[string]Provider),
	}
	r.Register(NewCodexProvider())
	r.Register(NewClaudeProvider())
	r.Register(NewGeminiProvider())
	r.Register(NewAntigravityProvider())
	return r
}

// Register adds a provider to the registry.
func (r *Registry) Register(p Provider) {
	r.providers[p.Name()] = p
}

// Get retrieves a provider by name.
func (r *Registry) Get(name string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", name)
	}
	return p, nil
}

// All returns all registered providers.
func (r *Registry) All() []Provider {
	var list []Provider
	for _, p := range r.providers {
		list = append(list, p)
	}
	return list
}
