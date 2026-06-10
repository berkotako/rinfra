package cloud

import (
	"context"
	"fmt"
	"sync"

	"github.com/rinfra/rinfra/internal/domain"
)

// Sweeper is implemented by cloud providers that can list and delete resources
// by engagement tag. It is used during teardown reconciliation to remove any
// resources that escaped Pulumi state (e.g. from a crashed deploy).
//
// Not all providers need to implement this interface; if a provider does not,
// the reconciliation sweep is skipped for that provider with a warning.
type Sweeper interface {
	// SweepOrphans lists all resources tagged with rinfra:<engagementID> in
	// the provider's API and deletes any that are not already destroyed. Must
	// be idempotent and safe to call with an empty engagement (returns nil).
	SweepOrphans(ctx context.Context, creds Credentials, engagementID string) error
}

var (
	swMu     sync.RWMutex
	sweepers = map[domain.CloudProviderType]Sweeper{}
)

// RegisterSweeper registers a Sweeper for a provider type. May be called once
// per provider; panics on duplicate.
func RegisterSweeper(t domain.CloudProviderType, s Sweeper) {
	swMu.Lock()
	defer swMu.Unlock()
	if _, dup := sweepers[t]; dup {
		panic(fmt.Sprintf("cloud: sweeper already registered for %s", t))
	}
	sweepers[t] = s
}

// GetSweeper returns the Sweeper for a cloud type and a bool indicating
// whether one is registered.
func GetSweeper(t domain.CloudProviderType) (Sweeper, bool) {
	swMu.RLock()
	defer swMu.RUnlock()
	s, ok := sweepers[t]
	return s, ok
}
