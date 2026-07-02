// Package orchestration compiles a domain.Topology into a Pulumi automation-API
// program and drives stack.Up / stack.Destroy against a local-file backend (or a
// remote backend when PULUMI_BACKEND_URL is set), with bounded retries on
// transient failures.
//
// # Design
//
// The Engine is the only package in the repo that imports the Pulumi automation
// API or any Pulumi provider SDK. The service layer calls Engine.Deploy /
// Engine.Teardown; it never imports Pulumi directly. Cloud provider
// implementations (internal/cloud/*) also do NOT use Pulumi — they use the
// Pulumi SDKs exclusively through the Engine's inline program callback that
// each provider supplies via the PulumiProgram helper.
//
// # Stack naming
//
// One Pulumi stack per engagement AND provider: rinfra-<engagement-id>-<provider>.
// The provider is part of the name because Pulumi treats an inline program as the
// COMPLETE desired state of its stack; a mixed-cloud engagement builds one program
// per provider group, so sharing a stack name would make the second group's
// stack.Up delete the first group's resources (absent from the second program).
// Per-(engagement, provider) stacks keep each cloud's state isolated — matching
// the Terraform backend's per-provider working directory.
// State is stored in a local file backend rooted at PULUMI_BACKEND_DIR
// (default: $HOME/.rinfra/pulumi-state). PULUMI_CONFIG_PASSPHRASE (or
// PULUMI_CONFIG_PASSPHRASE_FILE) must be set; both are documented in
// cmd/rinfra-server.
//
// The service layer supplies the FULL desired node set for each provider (its
// pending nodes plus the already-standing ones), not just the newly-pending
// nodes, so an incremental re-deploy converges instead of destroying live nodes.
//
// Engagements provisioned before the per-provider rename kept their state in the
// old single stack (rinfra-<engagement-id>). Deploy/Teardown adopt that legacy
// stack into the new provider-suffixed name on first use (single-provider
// engagements only; see adoptLegacyStack), so they update existing infrastructure
// instead of creating duplicates. This is a no-op on any system with no legacy
// stack.
//
// # Resource tagging
//
// Every resource receives two tags / labels applied by the per-provider inline
// program:
//
//	rinfra:<engagement-id>   — top-level ownership tag used for sweeps
//	rinfra:node:<node-id>    — per-node tag so individual nodes can be
//	                           identified during teardown reconciliation
//
// # Teardown reconciliation
//
// After stack.Destroy, SweepOrphans asks each provider to list resources
// tagged rinfra:<engagement-id> and delete any that escaped Pulumi state.
// This is the "guaranteed teardown" promise from CLAUDE.md.
package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/retry"
)

// Retry policy for transient IaC failures (rate limits, throttling, 5xx, brief
// network blips). Pulumi Up/Destroy are declarative and converge, so retrying is
// safe.
const (
	deployAttempts = 3
	deployBackoff  = 2 * time.Second
)

// NodeResult is the per-node outcome returned by Deploy.
type NodeResult struct {
	NodeID      string
	ProviderRef string
	PublicIP    string
	Err         error
}

// ProgramBuilder is implemented by each real cloud provider to register
// its Pulumi resources into an inline program. The builder receives the
// engagement ID (for tagging), the creds, and the list of nodes to provision.
// It appends ctx.Export calls so the Engine can harvest ProviderRef / PublicIP.
//
// The interface is internal to the orchestration package — only cloud provider
// packages that live alongside the Engine implement it.
type ProgramBuilder interface {
	// BuildProgram returns a Pulumi RunFunc that provisions all nodes in the
	// topology using the supplied credentials. Outputs must be exported with the
	// keys NodeProviderRefKey(nodeID) and NodePublicIPKey(nodeID).
	BuildProgram(engagementID string, creds cloud.Credentials, nodes []domain.Node) pulumi.RunFunc
}

// NodeProviderRefKey returns the Pulumi output key used to export a node's
// provider reference (cloud resource ID).
func NodeProviderRefKey(nodeID string) string { return "providerRef:" + nodeID }

// NodePublicIPKey returns the Pulumi output key used to export a node's
// assigned public IP address.
func NodePublicIPKey(nodeID string) string { return "publicIP:" + nodeID }

// Engine compiles and executes Pulumi stacks for RInfra engagements.
type Engine struct {
	builders   map[domain.CloudProviderType]ProgramBuilder
	stateDir   string
	backendURL string
	log        *slog.Logger
}

// New returns a new Engine. stateDir is the root directory for local Pulumi
// state; if empty it defaults to $HOME/.rinfra/pulumi-state.
//
// If PULUMI_BACKEND_URL is set in the environment it is used verbatim as the
// state backend (e.g. s3://bucket, gs://bucket, azblob://container, or a Pulumi
// service URL), so state survives an ephemeral container instead of living on
// local disk. Otherwise state is the local file backend under stateDir.
func New(stateDir string, log *slog.Logger) *Engine {
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".rinfra", "pulumi-state")
	}
	backend := os.Getenv("PULUMI_BACKEND_URL")
	if backend == "" {
		backend = "file://" + stateDir
	}
	return &Engine{
		builders:   make(map[domain.CloudProviderType]ProgramBuilder),
		stateDir:   stateDir,
		backendURL: backend,
		log:        log,
	}
}

// RegisterBuilder associates a ProgramBuilder with a provider type. Each
// cloud provider package calls this once from its init() function (or from
// explicit wiring in main). Panics on duplicate registration to catch wiring
// errors early (same pattern as cloud.Register).
func (e *Engine) RegisterBuilder(providerType domain.CloudProviderType, b ProgramBuilder) {
	if _, dup := e.builders[providerType]; dup {
		panic(fmt.Sprintf("orchestration: builder already registered for %s", providerType))
	}
	e.builders[providerType] = b
}

// stackName returns the canonical Pulumi stack name for an engagement's nodes on
// a single cloud provider. The provider is part of the name because Pulumi treats
// an inline program as the COMPLETE desired state of its stack: a mixed-cloud
// engagement builds one program per provider group, so sharing a stack name would
// make the second group's stack.Up delete the first group's resources (they are
// absent from the second program). One stack per (engagement, provider) keeps each
// cloud's state isolated — mirroring the Terraform backend's per-provider workdir.
func stackName(engagementID string, providerType domain.CloudProviderType) string {
	return "rinfra-" + engagementID + "-" + string(providerType)
}

// buildEnvVars merges the provider credential env vars with the Pulumi
// backend URL env var. PULUMI_CONFIG_PASSPHRASE must already be present in
// the process environment; it is not injected here to avoid appearing in
// slog output (the operator sets it via the system environment).
func (e *Engine) buildEnvVars(creds cloud.Credentials) map[string]string {
	env := make(map[string]string, len(creds.Raw)+1)
	for k, v := range creds.Raw {
		env[k] = v
	}
	// Tell Pulumi which state backend to use (local file by default; a remote
	// s3://, gs://, azblob:// or Pulumi-service URL when PULUMI_BACKEND_URL is set).
	env["PULUMI_BACKEND_URL"] = e.backendURL
	return env
}

// Deploy provisions all nodes in the topology. It groups nodes by cloud
// provider, builds one Pulumi inline program per provider group, and runs
// stack.Up for each group. On success it returns one NodeResult per node with
// ProviderRef and PublicIP populated.
//
// Deploy is idempotent: if a stack already exists (e.g. from a previous
// partial deploy) UpsertStack is used so it is selected rather than
// recreated.
func (e *Engine) Deploy(ctx context.Context, engagementID string, nodes []domain.Node, creds map[domain.CloudProviderType]cloud.Credentials) ([]NodeResult, error) {
	if err := os.MkdirAll(e.stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("orchestration: create state dir: %w", err)
	}

	// Group nodes by provider.
	byProvider := groupByProvider(nodes)

	var results []NodeResult

	for providerType, providerNodes := range byProvider {
		builder, ok := e.builders[providerType]
		if !ok {
			for _, n := range providerNodes {
				results = append(results, NodeResult{NodeID: n.ID, Err: fmt.Errorf("no orchestration builder for cloud %q", providerType)})
			}
			continue
		}

		providerCreds, ok := creds[providerType]
		if !ok {
			for _, n := range providerNodes {
				results = append(results, NodeResult{NodeID: n.ID, Err: fmt.Errorf("no credentials for cloud %q", providerType)})
			}
			continue
		}

		program := builder.BuildProgram(engagementID, providerCreds, providerNodes)
		envVars := e.buildEnvVars(providerCreds)

		sName := stackName(engagementID, providerType)
		projectName := "rinfra"

		stack, err := auto.UpsertStackInlineSource(ctx, sName, projectName, program, auto.EnvVars(envVars))
		if err != nil {
			for _, n := range providerNodes {
				results = append(results, NodeResult{NodeID: n.ID, Err: fmt.Errorf("upsert stack: %w", err)})
			}
			continue
		}

		// Adopt a pre-rename engagement stack into this provider-suffixed stack, so
		// an engagement deployed before the per-provider rename updates its existing
		// infrastructure instead of creating duplicates. No-op on fresh systems.
		if err := e.adoptLegacyStack(ctx, stack, engagementID, providerType, len(byProvider) == 1); err != nil {
			for _, n := range providerNodes {
				results = append(results, NodeResult{NodeID: n.ID, Err: err})
			}
			continue
		}

		// Pulumi is declarative, so re-running Up after a transient failure is
		// safe (it converges to the same desired state).
		var upRes auto.UpResult
		err = retry.Do(ctx, deployAttempts, deployBackoff, retry.IsTransient, func() error {
			var e2 error
			upRes, e2 = stack.Up(ctx, optup.ProgressStreams(newLogWriter(e.log, engagementID)))
			return e2
		})
		if err != nil {
			for _, n := range providerNodes {
				results = append(results, NodeResult{NodeID: n.ID, Err: fmt.Errorf("stack up: %w", err)})
			}
			continue
		}

		// Harvest per-node outputs.
		for _, n := range providerNodes {
			r := NodeResult{NodeID: n.ID}
			if v, ok := upRes.Outputs[NodeProviderRefKey(n.ID)]; ok {
				r.ProviderRef = fmt.Sprintf("%v", v.Value)
			}
			if v, ok := upRes.Outputs[NodePublicIPKey(n.ID)]; ok {
				r.PublicIP = fmt.Sprintf("%v", v.Value)
			}
			results = append(results, r)
		}
	}

	return results, nil
}

// Teardown runs stack.Destroy for the engagement and then calls SweepOrphans
// to remove any tagged resources that escaped Pulumi state. It is safe to
// call even if no stack exists (idempotent).
//
// It returns a non-nil error if any provider's stack.Destroy or SweepOrphans
// failed: a failed teardown must NOT be reported as success, or the caller marks
// the nodes destroyed and completes the engagement over still-live cloud
// resources (the exact orphan the guaranteed-teardown promise exists to prevent).
// A missing stack is not an error — there is nothing to destroy.
func (e *Engine) Teardown(ctx context.Context, engagementID string, nodes []domain.Node, creds map[domain.CloudProviderType]cloud.Credentials) error {
	byProvider := groupByProvider(nodes)

	var errs []error
	for providerType, providerNodes := range byProvider {
		builder, ok := e.builders[providerType]
		if !ok {
			e.log.Warn("orchestration: no builder for teardown", "provider", providerType)
			errs = append(errs, fmt.Errorf("no builder for cloud %q", providerType))
			continue
		}

		providerCreds, ok := creds[providerType]
		if !ok {
			// Missing creds only matters if something was actually provisioned. When
			// no node has a ProviderRef (e.g. a deploy that failed on missing creds
			// before creating anything), there is no stack/resource to destroy, so
			// this is NOT a teardown failure — the caller can mark the nodes
			// destroyed. Erroring here would wedge every teardown/reaper attempt.
			if anyProvisioned(providerNodes) {
				e.log.Warn("orchestration: no creds for teardown of provisioned nodes", "provider", providerType)
				errs = append(errs, fmt.Errorf("no credentials for cloud %q (cannot destroy provisioned resources)", providerType))
			} else {
				e.log.Info("orchestration: no creds but nothing provisioned; nothing to destroy", "provider", providerType)
			}
			continue
		}

		program := builder.BuildProgram(engagementID, providerCreds, providerNodes)
		envVars := e.buildEnvVars(providerCreds)
		sName := stackName(engagementID, providerType)
		projectName := "rinfra"

		// Upsert (not Select) so a pre-rename engagement can adopt its legacy stack
		// into this name before destroy; on a fresh system this just opens an empty
		// stack and Destroy is a no-op.
		stack, err := auto.UpsertStackInlineSource(ctx, sName, projectName, program, auto.EnvVars(envVars))
		if err != nil {
			e.log.Error("orchestration: could not open stack for teardown", "engagement", engagementID, "provider", providerType, "err", err)
			errs = append(errs, fmt.Errorf("open stack for cloud %q: %w", providerType, err))
		} else if aerr := e.adoptLegacyStack(ctx, stack, engagementID, providerType, len(byProvider) == 1); aerr != nil {
			e.log.Error("orchestration: legacy stack adoption failed on teardown", "engagement", engagementID, "provider", providerType, "err", aerr)
			errs = append(errs, fmt.Errorf("adopt legacy stack for cloud %q: %w", providerType, aerr))
		} else {
			err = retry.Do(ctx, deployAttempts, deployBackoff, retry.IsTransient, func() error {
				_, e2 := stack.Destroy(ctx, optdestroy.ProgressStreams(newLogWriter(e.log, engagementID)))
				return e2
			})
			if err != nil {
				e.log.Error("orchestration: stack destroy error", "engagement", engagementID, "err", err)
				errs = append(errs, fmt.Errorf("destroy stack for cloud %q: %w", providerType, err))
				// Continue to sweep phase — best-effort cleanup — but the error is
				// still surfaced so the caller doesn't declare teardown a success.
			}
		}

		// Reconciliation sweep: delete any tagged stragglers.
		if sweeper, ok := cloud.GetSweeper(providerType); ok {
			if err := sweeper.SweepOrphans(ctx, providerCreds, engagementID); err != nil {
				e.log.Error("orchestration: sweep orphans error", "engagement", engagementID, "provider", providerType, "err", err)
				errs = append(errs, fmt.Errorf("sweep orphans for cloud %q: %w", providerType, err))
			}
		}
	}

	return errors.Join(errs...)
}

// legacyStackName is the pre–per-provider stack name (one stack per engagement,
// no provider suffix). Engagements provisioned before the rename have their state
// here; adoptLegacyStack migrates it into the provider-suffixed stack so the next
// deploy updates existing infrastructure instead of creating duplicates.
func legacyStackName(engagementID string) string { return "rinfra-" + engagementID }

// adoptLegacyStack migrates the pre-rename engagement stack (rinfra-<eng>) into
// the just-opened provider-suffixed stack. Guards keep it safe:
//   - single-provider deploys only — the legacy state unambiguously belongs to
//     this one provider (a legacy multi-cloud stack was already broken; it's left
//     for manual teardown rather than mis-imported into one provider's stack);
//   - only when the new stack has no resources yet — never clobber state that was
//     already migrated or freshly created.
//
// It is a no-op on any system without a legacy stack (every fresh deploy), so the
// normal path is unaffected. Errors are surfaced: a botched migration must fail
// the operation rather than silently duplicate or drop resources.
func (e *Engine) adoptLegacyStack(ctx context.Context, stack auto.Stack, engagementID string, providerType domain.CloudProviderType, singleProvider bool) error {
	legacy := legacyStackName(engagementID)
	newName := stackName(engagementID, providerType)
	if !singleProvider || legacy == newName {
		return nil
	}
	ws := stack.Workspace()
	summaries, err := ws.ListStacks(ctx)
	if err != nil {
		e.log.Warn("orchestration: could not list stacks for legacy migration; proceeding without it", "err", err)
		return nil
	}
	legacyFound, newCount := false, 0
	for _, s := range summaries {
		switch {
		case stackNameMatches(s.Name, legacy):
			legacyFound = true
		case stackNameMatches(s.Name, newName):
			if s.ResourceCount != nil {
				newCount = *s.ResourceCount
			}
		}
	}
	if !legacyFound || newCount > 0 {
		return nil // nothing to migrate, or the new stack already holds state
	}
	dep, err := ws.ExportStack(ctx, legacy)
	if err != nil {
		return fmt.Errorf("adopt legacy stack %q: export: %w", legacy, err)
	}
	if err := ws.ImportStack(ctx, newName, dep); err != nil {
		return fmt.Errorf("adopt legacy stack into %q: import: %w", newName, err)
	}
	if err := ws.RemoveStack(ctx, legacy); err != nil {
		// State is safe in the new stack; a lingering empty legacy stack is cosmetic.
		e.log.Warn("orchestration: migrated legacy stack but could not remove it", "legacy", legacy, "err", err)
	}
	e.log.Info("orchestration: migrated legacy engagement stack to per-provider stack", "from", legacy, "to", newName)
	return nil
}

// stackNameMatches reports whether a ListStacks summary name refers to the given
// short stack name. Backends may qualify names (org/project/stack), so match the
// exact name or a "/"-suffixed form.
func stackNameMatches(summaryName, want string) bool {
	return summaryName == want || strings.HasSuffix(summaryName, "/"+want)
}

// anyProvisioned reports whether any node in the group carries a ProviderRef,
// i.e. whether real cloud resources exist that a teardown must reach.
func anyProvisioned(nodes []domain.Node) bool {
	for _, n := range nodes {
		if n.ProviderRef != "" {
			return true
		}
	}
	return false
}

// groupByProvider partitions nodes by their Spec.Cloud value.
func groupByProvider(nodes []domain.Node) map[domain.CloudProviderType][]domain.Node {
	out := make(map[domain.CloudProviderType][]domain.Node)
	for _, n := range nodes {
		out[n.Spec.Cloud] = append(out[n.Spec.Cloud], n)
	}
	return out
}
