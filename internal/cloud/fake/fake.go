// Package fake provides a simulated CloudProvider for development and testing.
// It registers under the id "fake" with domain.CloudProviderType "fake".
//
// Behaviour:
//   - ProvisionNode returns deterministic IPs from the TEST-NET-3 range
//     (203.0.113.x) using an atomic counter. It sleeps for ProvisionDelay
//     (default 1.5 s, overridable to 0 for tests) to simulate real latency.
//   - An in-memory "actual cloud state" keyed by ProviderRef lets Destroy be
//     idempotent and allows reconciliation tests via ListActual.
//   - ConfigureIngress, AssignStaticIP, ManageDNS are no-ops that record calls.
package fake

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

// CloudProviderTypeFake is the domain.CloudProviderType used by this provider.
const CloudProviderTypeFake domain.CloudProviderType = "fake"

func init() { cloud.Register(New(Option{})) }

// Option configures the fake provider.
type Option struct {
	// ProvisionDelay is how long ProvisionNode sleeps per node. Zero means no
	// sleep, which is ideal for unit tests. Default: 1.5 seconds.
	ProvisionDelay time.Duration
}

// Resource is a record of a node provisioned into the fake "cloud".
type Resource struct {
	ProviderRef  string
	EngagementID string
	Node         domain.Node
}

// IngressCall records a single ConfigureIngress invocation.
type IngressCall struct {
	ProviderRef string
	Rules       []domain.Rule
}

// Provider is the fake CloudProvider.
type Provider struct {
	delay time.Duration

	mu        sync.RWMutex
	resources map[string]Resource // keyed by ProviderRef
	ingress   []IngressCall
	dns       []domain.Record

	counter atomic.Uint32
}

// New returns a new fake Provider with the given options.
func New(opt Option) *Provider {
	delay := opt.ProvisionDelay
	if delay == 0 && opt == (Option{}) {
		delay = 1500 * time.Millisecond
	}
	return &Provider{
		delay:     delay,
		resources: make(map[string]Resource),
	}
}

var _ cloud.CloudProvider = (*Provider)(nil)

// Type implements cloud.CloudProvider.
func (p *Provider) Type() domain.CloudProviderType { return CloudProviderTypeFake }

// ProvisionNode simulates node creation and returns a node with a deterministic
// IP and a UUID ProviderRef. It sleeps for ProvisionDelay before returning.
func (p *Provider) ProvisionNode(ctx context.Context, creds cloud.Credentials, spec domain.NodeSpec) (domain.Node, error) {
	if p.delay > 0 {
		select {
		case <-time.After(p.delay):
		case <-ctx.Done():
			return domain.Node{}, ctx.Err()
		}
	}

	n := p.counter.Add(1)
	ip := fmt.Sprintf("203.0.113.%d", n%256)
	ref := "fake-" + uuid.NewString()

	node := domain.Node{
		Spec:        spec,
		Status:      domain.NodeLive,
		Health:      domain.HealthHealthy,
		PublicIP:    ip,
		ProviderRef: ref,
	}

	p.mu.Lock()
	p.resources[ref] = Resource{
		ProviderRef: ref,
		Node:        node,
	}
	p.mu.Unlock()

	return node, nil
}

// ConfigureIngress records the call but does nothing.
func (p *Provider) ConfigureIngress(_ context.Context, _ cloud.Credentials, node domain.Node, rules []domain.Rule) error {
	p.mu.Lock()
	p.ingress = append(p.ingress, IngressCall{ProviderRef: node.ProviderRef, Rules: rules})
	p.mu.Unlock()
	return nil
}

// AssignStaticIP returns the node's existing PublicIP. No-op for the fake.
func (p *Provider) AssignStaticIP(_ context.Context, _ cloud.Credentials, node domain.Node) (string, error) {
	return node.PublicIP, nil
}

// ManageDNS records the DNS record but does nothing.
func (p *Provider) ManageDNS(_ context.Context, _ cloud.Credentials, rec domain.Record) error {
	p.mu.Lock()
	p.dns = append(p.dns, rec)
	p.mu.Unlock()
	return nil
}

// Destroy removes the resource from the fake state. Idempotent.
func (p *Provider) Destroy(_ context.Context, _ cloud.Credentials, node domain.Node) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.resources, node.ProviderRef)
	return nil
}

// ListActual returns all resources currently tracked by the fake provider for
// the given engagement ID. Used by reconciliation tests.
func (p *Provider) ListActual(engagementID string) []Resource {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var out []Resource
	for _, r := range p.resources {
		if r.EngagementID == engagementID || engagementID == "" {
			out = append(out, r)
		}
	}
	return out
}

// RecordEngagement associates a ProviderRef with an engagementID so that
// ListActual filters correctly. Called by InfraService after provisioning.
func (p *Provider) RecordEngagement(providerRef, engagementID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r, ok := p.resources[providerRef]; ok {
		r.EngagementID = engagementID
		p.resources[providerRef] = r
	}
}

// IngressCalls returns a snapshot of recorded ConfigureIngress calls.
func (p *Provider) IngressCalls() []IngressCall {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]IngressCall, len(p.ingress))
	copy(out, p.ingress)
	return out
}

// PerNodeDestroy marks the fake provider's Destroy as node-scoped (see cloud.PerNodeDestroyer).
func (p *Provider) PerNodeDestroy() {}
