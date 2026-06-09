package c2

import (
	"fmt"
	"sort"
	"sync"
)

var (
	mu       sync.RWMutex
	registry = map[string]C2Provider{}
)

// Register makes a C2Provider available by Name(). Panics on duplicates.
func Register(p C2Provider) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[p.Name()]; dup {
		panic(fmt.Sprintf("c2: provider already registered: %s", p.Name()))
	}
	registry[p.Name()] = p
}

// Get returns the provider for a framework name.
func Get(name string) (C2Provider, error) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("c2: no provider registered for %q", name)
	}
	return p, nil
}

// List returns all registered providers, sorted by name — useful for rendering
// the framework selector with tier badges.
func List() []C2Provider {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]C2Provider, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
