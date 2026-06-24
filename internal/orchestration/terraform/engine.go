// Package terraform is an alternative IaC backend to the Pulumi engine. It
// compiles a topology into Terraform JSON (*.tf.json), then drives
// `terraform init/apply/destroy` against a local working directory per
// engagement+provider, mirroring orchestration.Engine's contract.
//
// # Design
//
// This package is the only place that shells out to the Terraform CLI. Cloud
// provider packages implement the Builder interface to emit their provider's
// Terraform JSON (the same resources their Pulumi ProgramBuilder creates). The
// Engine writes that JSON, runs Terraform, and harvests per-node outputs.
//
// Both this Engine and orchestration.Engine satisfy the service's Provisioner
// interface, so the IaC backend is swappable at runtime.
//
// # Working directory & state
//
// One dir per engagement+provider under stateDir:
//
//	<stateDir>/<engagement-id>/<provider>/main.tf.json
//
// Terraform's default local backend keeps state (terraform.tfstate) alongside
// it. The Terraform CLI must be on PATH for any deploy/teardown.
//
// # Teardown reconciliation
//
// After `terraform destroy`, the Engine calls the provider's cloud.Sweeper to
// remove any tagged resources that escaped Terraform state — the same
// guaranteed-teardown promise as the Pulumi engine.
package terraform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration"
	"github.com/rinfra/rinfra/internal/retry"
)

// Retry policy for transient terraform failures; apply/destroy are idempotent.
const (
	deployAttempts = 3
	deployBackoff  = 2 * time.Second
)

// Config is a Terraform JSON document — the contents of a main.tf.json file.
// Builders populate the blocks they need; empty maps are omitted on marshal.
type Config struct {
	Terraform map[string]any `json:"terraform,omitempty"`
	Provider  map[string]any `json:"provider,omitempty"`
	Resource  map[string]any `json:"resource,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Output    map[string]any `json:"output,omitempty"`
}

// Builder is implemented by each cloud provider to emit its Terraform JSON for
// a group of nodes. It is the Terraform analogue of orchestration.ProgramBuilder.
// Outputs must be registered under ProviderRefOutput(nodeID) and
// PublicIPOutput(nodeID) so the Engine can harvest them.
type Builder interface {
	BuildConfig(engagementID string, creds cloud.Credentials, nodes []domain.Node) (*Config, error)
}

var tfNameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// tfSan makes an arbitrary id safe for a Terraform identifier.
func tfSan(s string) string { return tfNameUnsafe.ReplaceAllString(s, "_") }

// SafeName exposes the Terraform-identifier sanitizer to Builder implementations
// so resource block names stay valid.
func SafeName(s string) string { return tfSan(s) }

// ProviderRefOutput / PublicIPOutput are the Terraform output names a Builder
// must use for each node (Terraform output names cannot contain ':').
func ProviderRefOutput(nodeID string) string { return "providerref_" + tfSan(nodeID) }
func PublicIPOutput(nodeID string) string    { return "publicip_" + tfSan(nodeID) }

// Engine compiles and executes Terraform configs for RInfra engagements.
type Engine struct {
	builders map[domain.CloudProviderType]Builder
	stateDir string
	bin      string // terraform binary (default "terraform")
	log      *slog.Logger
}

// New returns a new Terraform Engine. stateDir roots the per-engagement working
// directories; if empty it defaults to $HOME/.rinfra/terraform-state.
func New(stateDir string, log *slog.Logger) *Engine {
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".rinfra", "terraform-state")
	}
	return &Engine{
		builders: make(map[domain.CloudProviderType]Builder),
		stateDir: stateDir,
		bin:      "terraform",
		log:      log,
	}
}

// RegisterBuilder associates a Builder with a provider type (mirrors
// orchestration.Engine.RegisterBuilder).
func (e *Engine) RegisterBuilder(providerType domain.CloudProviderType, b Builder) {
	if _, dup := e.builders[providerType]; dup {
		panic(fmt.Sprintf("terraform: builder already registered for %s", providerType))
	}
	e.builders[providerType] = b
}

func (e *Engine) workDir(engagementID string, pt domain.CloudProviderType) string {
	return filepath.Join(e.stateDir, engagementID, string(pt))
}

// Deploy provisions all nodes, grouped by provider. For each group it writes the
// provider's Terraform JSON, runs init+apply, and harvests per-node outputs.
func (e *Engine) Deploy(ctx context.Context, engagementID string, nodes []domain.Node, creds map[domain.CloudProviderType]cloud.Credentials) ([]orchestration.NodeResult, error) {
	var results []orchestration.NodeResult
	for providerType, providerNodes := range groupByProvider(nodes) {
		builder, ok := e.builders[providerType]
		if !ok {
			results = append(results, errResults(providerNodes, fmt.Errorf("no terraform builder for cloud %q", providerType))...)
			continue
		}
		providerCreds, ok := creds[providerType]
		if !ok {
			results = append(results, errResults(providerNodes, fmt.Errorf("no credentials for cloud %q", providerType))...)
			continue
		}
		cfg, err := builder.BuildConfig(engagementID, providerCreds, providerNodes)
		if err != nil {
			results = append(results, errResults(providerNodes, fmt.Errorf("build terraform config: %w", err))...)
			continue
		}
		dir := e.workDir(engagementID, providerType)
		if err := e.writeConfig(dir, cfg); err != nil {
			results = append(results, errResults(providerNodes, err)...)
			continue
		}
		env := credEnv(providerCreds)
		if err := e.run(ctx, dir, env, "init", "-input=false", "-no-color"); err != nil {
			results = append(results, errResults(providerNodes, fmt.Errorf("terraform init: %w", err))...)
			continue
		}
		// terraform apply is idempotent (it converges), so retry transient failures.
		if err := retry.Do(ctx, deployAttempts, deployBackoff, retry.IsTransient, func() error {
			return e.run(ctx, dir, env, "apply", "-auto-approve", "-input=false", "-no-color")
		}); err != nil {
			results = append(results, errResults(providerNodes, fmt.Errorf("terraform apply: %w", err))...)
			continue
		}
		outputs, err := e.outputs(ctx, dir, env)
		if err != nil {
			results = append(results, errResults(providerNodes, fmt.Errorf("terraform output: %w", err))...)
			continue
		}
		for _, n := range providerNodes {
			r := orchestration.NodeResult{NodeID: n.ID}
			if v, ok := outputs[ProviderRefOutput(n.ID)]; ok {
				r.ProviderRef = fmt.Sprintf("%v", v)
			}
			if v, ok := outputs[PublicIPOutput(n.ID)]; ok {
				r.PublicIP = fmt.Sprintf("%v", v)
			}
			results = append(results, r)
		}
	}
	return results, nil
}

// Teardown runs `terraform destroy` per provider group, then sweeps tagged
// orphans via the provider's cloud.Sweeper. Idempotent.
func (e *Engine) Teardown(ctx context.Context, engagementID string, nodes []domain.Node, creds map[domain.CloudProviderType]cloud.Credentials) error {
	for providerType, providerNodes := range groupByProvider(nodes) {
		providerCreds, ok := creds[providerType]
		if !ok {
			e.log.Warn("terraform: no creds for teardown", "provider", providerType)
			continue
		}
		dir := e.workDir(engagementID, providerType)
		env := credEnv(providerCreds)
		// destroy is best-effort; a missing dir/state means nothing to destroy.
		if _, err := os.Stat(filepath.Join(dir, "main.tf.json")); err == nil {
			if err := retry.Do(ctx, deployAttempts, deployBackoff, retry.IsTransient, func() error {
				return e.run(ctx, dir, env, "destroy", "-auto-approve", "-input=false", "-no-color")
			}); err != nil {
				e.log.Error("terraform: destroy error", "engagement", engagementID, "provider", providerType, "err", err)
			}
		} else {
			e.log.Info("terraform: no config for teardown (already destroyed?)", "engagement", engagementID, "provider", providerType)
		}
		if sweeper, ok := cloud.GetSweeper(providerType); ok {
			if err := sweeper.SweepOrphans(ctx, providerCreds, engagementID); err != nil {
				e.log.Error("terraform: sweep orphans error", "engagement", engagementID, "provider", providerType, "err", err)
			}
		}
		_ = providerNodes
	}
	return nil
}

// writeConfig serializes cfg to <dir>/main.tf.json.
func (e *Engine) writeConfig(dir string, cfg *Config) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("terraform: create work dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("terraform: marshal config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf.json"), data, 0o600); err != nil {
		return fmt.Errorf("terraform: write config: %w", err)
	}
	return nil
}

// run executes a terraform subcommand in dir with the given extra env.
func (e *Engine) run(ctx context.Context, dir string, env []string, args ...string) error {
	if _, err := exec.LookPath(e.bin); err != nil {
		return fmt.Errorf("terraform CLI not found on PATH (install Terraform to enable this backend): %w", err)
	}
	cmd := exec.CommandContext(ctx, e.bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append(env, "TF_IN_AUTOMATION=1")...)
	// Tee output to the logger AND a buffer so the returned error carries the
	// provider's message (e.g. "429", "throttling", "timeout"). Without this the
	// error is just exec's "exit status 1" and retry.IsTransient can't tell a
	// transient failure from a permanent one.
	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(newLogWriter(e.log, "terraform"), &buf)
	cmd.Stderr = io.MultiWriter(newLogWriter(e.log, "terraform"), &buf)
	if err := cmd.Run(); err != nil {
		if out := strings.TrimSpace(tail(buf.String(), 4096)); out != "" {
			return fmt.Errorf("%w: %s", err, out)
		}
		return err
	}
	return nil
}

// tail returns the last n bytes of s (the part most likely to contain the error
// message), so a noisy run can't balloon the wrapped error.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// outputs runs `terraform output -json` and returns name->value.
func (e *Engine) outputs(ctx context.Context, dir string, env []string) (map[string]any, error) {
	cmd := exec.CommandContext(ctx, e.bin, "output", "-json", "-no-color")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = newLogWriter(e.log, "terraform")
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		return nil, fmt.Errorf("parse outputs: %w", err)
	}
	res := make(map[string]any, len(raw))
	for k, v := range raw {
		res[k] = v.Value
	}
	return res, nil
}

func groupByProvider(nodes []domain.Node) map[domain.CloudProviderType][]domain.Node {
	out := make(map[domain.CloudProviderType][]domain.Node)
	for _, n := range nodes {
		out[n.Spec.Cloud] = append(out[n.Spec.Cloud], n)
	}
	return out
}

func errResults(nodes []domain.Node, err error) []orchestration.NodeResult {
	out := make([]orchestration.NodeResult, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, orchestration.NodeResult{NodeID: n.ID, Err: err})
	}
	return out
}

// credEnv turns the credential Raw map into KEY=VALUE env entries. Terraform's
// AWS/Google/azurerm/digitalocean providers read the same env vars the Pulumi
// providers do.
func credEnv(creds cloud.Credentials) []string {
	env := make([]string, 0, len(creds.Raw))
	for k, v := range creds.Raw {
		env = append(env, k+"="+v)
	}
	return env
}
