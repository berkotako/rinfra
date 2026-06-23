// Package ttp is the portable technique→primitive catalog: it compiles a
// portable domain.Technique into a framework-agnostic c2.Primitive (plus
// resolved arguments), which each C2 adapter then renders into its native
// command(s).
//
// This replaces the per-framework `switch t.AttackID` tables — which duplicated
// the same ATT&CK IDs across every adapter — with a single, data-driven catalog
// (catalog.yaml, embedded so it versions with the binary). Adding a TTP that
// reuses an existing c2.PrimitiveKind is a one-entry YAML change with no Go
// edits; the technique then works on every framework that supports that
// primitive, and frameworks that don't report it unsupported.
//
// No payload content lives here — only the portable verb (the primitive) and
// the parameters the technique supplies.
package ttp

import (
	"embed"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
)

//go:embed catalog.yaml
var embedFS embed.FS

// argBinding describes how one primitive argument is resolved from a technique.
type argBinding struct {
	From     string `yaml:"from"`     // key in technique.Inputs to read
	Default  string `yaml:"default"`  // value when the input is absent/empty
	Required bool   `yaml:"required"` // error if unresolved
}

// entry is one technique→primitive mapping in the catalog.
type entry struct {
	AttackID  string                `yaml:"attack_id"`
	Name      string                `yaml:"name"`
	Tactic    string                `yaml:"tactic"`
	Primitive string                `yaml:"primitive"`
	Args      map[string]argBinding `yaml:"args"`
}

type catalogFile struct {
	Techniques []entry `yaml:"techniques"`
}

// Catalog is a loaded technique→primitive mapping.
type Catalog struct {
	entries map[string]entry
}

// validPrimitives is the closed set of primitive kinds the catalog may
// reference. It mirrors the c2.PrimitiveKind constants; an entry naming an
// unknown primitive fails to load (catches typos at startup, not at run time).
var validPrimitives = map[c2.PrimitiveKind]bool{
	c2.PrimPowerShell:     true,
	c2.PrimShell:          true,
	c2.PrimSysInfo:        true,
	c2.PrimProcessList:    true,
	c2.PrimNetConnections: true,
	c2.PrimNetConfig:      true,
	c2.PrimFileList:       true,
	c2.PrimDownload:       true,
	c2.PrimScheduledTask:  true,
	c2.PrimRegistryRunKey: true,
}

// builtin is the embedded catalog, loaded once at package init.
var builtin *Catalog

func init() {
	c, err := load(embedFS)
	if err != nil {
		panic(fmt.Sprintf("ttp: loading embedded catalog: %v", err))
	}
	builtin = c
}

// Default returns the built-in catalog loaded from the embedded YAML.
func Default() *Catalog { return builtin }

// load parses and validates catalog.yaml from fsys.
func load(fsys embed.FS) (*Catalog, error) {
	data, err := fsys.ReadFile("catalog.yaml")
	if err != nil {
		return nil, fmt.Errorf("read catalog.yaml: %w", err)
	}
	var cf catalogFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parse catalog.yaml: %w", err)
	}
	entries := make(map[string]entry, len(cf.Techniques))
	for _, e := range cf.Techniques {
		if e.AttackID == "" {
			return nil, fmt.Errorf("catalog entry %q has empty attack_id", e.Name)
		}
		if _, dup := entries[e.AttackID]; dup {
			return nil, fmt.Errorf("duplicate catalog entry for %s", e.AttackID)
		}
		if !validPrimitives[c2.PrimitiveKind(e.Primitive)] {
			return nil, fmt.Errorf("technique %s: unknown primitive %q", e.AttackID, e.Primitive)
		}
		entries[e.AttackID] = e
	}
	return &Catalog{entries: entries}, nil
}

// Compile resolves a technique to a portable primitive using the default
// catalog. See (*Catalog).Compile.
func Compile(t domain.Technique) (c2.Primitive, bool, error) {
	return builtin.Compile(t)
}

// Compile resolves a technique to a portable primitive. The bool reports
// whether the technique has a catalog mapping at all (false → no entry, the
// caller should record the technique as unsupported on every framework). A
// non-nil error means the entry exists but a required input is missing.
func (c *Catalog) Compile(t domain.Technique) (c2.Primitive, bool, error) {
	e, ok := c.entries[t.AttackID]
	if !ok {
		return c2.Primitive{}, false, nil
	}
	args := make(map[string]string, len(e.Args))
	for name, b := range e.Args {
		v := b.Default
		if b.From != "" {
			if iv, has := t.Inputs[b.From]; has && iv != "" {
				v = iv
			}
		}
		if v == "" {
			if b.Required {
				return c2.Primitive{}, true, fmt.Errorf("technique %s requires input %q", t.AttackID, b.From)
			}
			continue
		}
		args[name] = v
	}
	return c2.Primitive{Kind: c2.PrimitiveKind(e.Primitive), Args: args}, true, nil
}

// Has reports whether the catalog has a mapping for the given ATT&CK ID.
func (c *Catalog) Has(attackID string) bool {
	_, ok := c.entries[attackID]
	return ok
}

// AttackIDs returns the ATT&CK IDs the catalog maps, unsorted.
func (c *Catalog) AttackIDs() []string {
	ids := make([]string, 0, len(c.entries))
	for id := range c.entries {
		ids = append(ids, id)
	}
	return ids
}
