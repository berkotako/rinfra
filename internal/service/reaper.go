package service

import (
	"context"
	"fmt"
	"time"

	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/domain"
)

// reaperActor is the audit actor recorded for automatic teardowns.
const reaperActor = "system:reaper"

// ReapExpired performs one auto-teardown sweep: it tears down infrastructure for
// every engagement whose permitted activity window has closed (Engagement.
// WindowExpired) but that still has live/provisioning infrastructure. This
// enforces the teardown invariant in time — CanDeploy already blocks NEW deploys
// after the window closes, but without this, infra provisioned earlier would
// linger past authorization (cost, exposure, ToS risk).
//
// `now` is injected for testability. The sweep is best-effort: a failure on one
// engagement (e.g. a deploy/teardown job already running) is logged and skipped,
// not fatal to the rest. Returns the engagement IDs for which a teardown was
// started.
func (s *InfraService) ReapExpired(ctx context.Context, now time.Time) ([]string, error) {
	engs, err := s.engagements.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("reaper: list engagements: %w", err)
	}
	var reaped []string
	for _, e := range engs {
		if !e.WindowExpired(now) {
			continue
		}
		nodes, err := s.infra.NodesForEngagement(ctx, e.ID)
		if err != nil {
			s.log.Warn("reaper: list nodes failed", "engagement", e.ID, "err", err)
			continue
		}
		reapable := countReapableInfra(nodes)
		if reapable == 0 {
			continue
		}
		_ = s.audit.Record(ctx, audit.Event{
			EngagementID: e.ID,
			Actor:        reaperActor,
			Action:       "infra.auto_teardown",
			Target:       e.ID,
			Detail:       fmt.Sprintf("activity window closed (status=%s); reaping %d standing node(s)", e.Status, reapable),
			At:           now.UTC(),
		})
		if _, err := s.Teardown(ctx, e.ID, reaperActor); err != nil {
			// e.g. a job is already running for this engagement — try again next sweep.
			s.log.Warn("reaper: teardown failed", "engagement", e.ID, "err", err)
			continue
		}
		reaped = append(reaped, e.ID)
	}
	return reaped, nil
}

// StartReaper launches a background sweep on the given interval until ctx is
// cancelled. interval <= 0 disables the reaper (the sweep can still be invoked
// directly via ReapExpired). It is safe to call once at startup.
func (s *InfraService) StartReaper(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		s.log.Info("infra reaper disabled (interval <= 0)")
		return
	}
	s.log.Info("infra reaper started", "interval", interval.String())
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				reaped, err := s.ReapExpired(ctx, time.Now())
				if err != nil {
					s.log.Warn("reaper sweep failed", "err", err)
					continue
				}
				if len(reaped) > 0 {
					s.log.Info("reaper tore down expired engagements", "count", len(reaped), "engagements", reaped)
				}
			}
		}
	}()
}

// countReapableInfra counts nodes that may own standing cloud resources and so
// warrant a teardown pass when the window closes: live/provisioning/draining,
// AND failed. A failed node matters because a partial IaC deploy can create real
// resources before erroring — the node is marked failed but Pulumi/Terraform
// state (and the tag-based sweep) can still destroy them; skipping failed nodes
// would leave exactly the orphans the reaper exists to prevent. Pending (never
// provisioned) and destroyed nodes own nothing and are ignored. Teardown itself
// is idempotent/best-effort, so an over-inclusive count is safe.
func countReapableInfra(nodes []domain.Node) int {
	n := 0
	for _, node := range nodes {
		switch node.Status {
		case domain.NodeLive, domain.NodeProvisioning, domain.NodeDraining, domain.NodeFailed:
			n++
		}
	}
	return n
}
