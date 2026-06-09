// Package payload defines the abstraction over initial-access artifact
// generators (e.g. msfvenom). These tools produce a stager that calls back to a
// deployed C2 listener.
//
// POSTURE: exactly as with C2 frameworks, RInfra COMPOSES existing, publicly
// available tooling — a Generator implementation shells out to the upstream
// binary the operator has installed. RInfra authors no payload bytes,
// shellcode, encoders, or evasion logic. Generation is an engagement-bound,
// audited action and callers MUST verify domain.Engagement.CanDeploy() before
// invoking a Generator, the same gate used before cloud provisioning.
package payload

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Callback is where a generated artifact phones home — the deployed C2 listener.
type Callback struct {
	Host string // LHOST / listener host (typically a redirector)
	Port int    // LPORT / listener port
	URL  string // optional full listener URL for HTTP(S) stagers
}

// Spec is the desired artifact, expressed in terms the upstream tool understands.
type Spec struct {
	Platform string            // "windows", "linux", "macos"
	Arch     string            // "x64", "x86"
	Format   string            // upstream output format, e.g. "exe", "elf", "raw"
	Callback Callback          // where the stager connects back to
	Extra    map[string]string // passthrough options for the upstream tool
}

// Artifact is the produced stager, written to a payload host. Only metadata is
// retained by RInfra; the bytes live on the host and are referenced by path.
type Artifact struct {
	Path   string // location of the generated artifact on the payload host
	SHA256 string // integrity + burn tracking
	Format string
}

// Generator is implemented once per upstream payload tool (msfvenom, and later
// e.g. native sliver/mythic generation).
type Generator interface {
	// Name is the generator identifier (e.g. "msfvenom").
	Name() string

	// PairsWith reports which C2 framework(s) this generator produces stagers
	// for, so the UI can offer it only alongside a compatible deployed C2.
	PairsWith() []string

	// Generate produces an artifact by invoking the upstream tool. It does not
	// author payload content — it parameterizes and runs the public binary.
	Generate(ctx context.Context, spec Spec) (Artifact, error)
}

var (
	mu       sync.RWMutex
	registry = map[string]Generator{}
)

// Register makes a Generator available by Name(). Panics on duplicates.
func Register(g Generator) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[g.Name()]; dup {
		panic(fmt.Sprintf("payload: generator already registered: %s", g.Name()))
	}
	registry[g.Name()] = g
}

// Get returns the generator for a name.
func Get(name string) (Generator, error) {
	mu.RLock()
	defer mu.RUnlock()
	g, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("payload: no generator registered for %q", name)
	}
	return g, nil
}

// List returns all registered generators, sorted by name.
func List() []Generator {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Generator, 0, len(registry))
	for _, g := range registry {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
