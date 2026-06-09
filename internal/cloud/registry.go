package cloud

import (
	"fmt"
	"sync"

	"github.com/rinfra/rinfra/internal/domain"
)

// registry holds the available cloud providers, keyed by type. Each adapter
// package registers itself via Register (typically from an init function or
// explicit wiring in main).
var (
	mu       sync.RWMutex
	registry = map[domain.CloudProviderType]CloudProvider{}
)

// Register makes a CloudProvider available for use. Panics on duplicate
// registration to catch wiring mistakes early.
func Register(p CloudProvider) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[p.Type()]; dup {
		panic(fmt.Sprintf("cloud: provider already registered: %s", p.Type()))
	}
	registry[p.Type()] = p
}

// Get returns the provider for a cloud type.
func Get(t domain.CloudProviderType) (CloudProvider, error) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[t]
	if !ok {
		return nil, fmt.Errorf("cloud: no provider registered for %q", t)
	}
	return p, nil
}
