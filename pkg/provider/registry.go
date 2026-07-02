package provider

import (
	"fmt"
)

// Registry holds all registered providers.
type Registry struct {
	providers map[string]Provider
	order     []string
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
	if _, exists := r.providers[p.Name()]; !exists {
		r.order = append(r.order, p.Name())
	}
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

// All returns all registered providers in their fixed registration order, so
// dump/resume fallback routing and listing are deterministic.
func (r *Registry) All() []Provider {
	list := make([]Provider, 0, len(r.order))
	for _, name := range r.order {
		list = append(list, r.providers[name])
	}
	return list
}
