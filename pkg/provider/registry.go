// Package provider provides the provider registry mechanism.
package provider

import (
	"fmt"
	"sync"
)

// ProviderFactory is a function that creates a new Provider instance.
type ProviderFactory func() Provider

var (
	registryMu sync.RWMutex
	registry   = make(map[string]ProviderFactory)
)

// Register registers a provider factory with the given name.
func Register(name string, factory ProviderFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

// Get returns a provider instance for the given name.
func Get(name string) (Provider, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
	return factory(), nil
}

// List returns all registered provider names.
func List() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
