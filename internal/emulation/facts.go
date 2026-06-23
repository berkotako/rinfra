package emulation

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/emulation/ttp"
)

// FactStore is the per-run knowledge base. It maps a fact key (e.g. "host.ip")
// to the values discovered for it during the run, so a later technique can
// reference what an earlier one collected — the chaining that makes multi-step
// emulation realistic. It is framework-agnostic: facts are parsed from the
// portable primitive's output, not from any one C2's wire format.
//
// This is the deliberately-small first increment of a fact model. Single-value
// substitution (first value wins) is implemented; multi-value fan-out (run a
// technique once per discovered value) and an autonomous planner are future
// work — the seams are here, the behaviour is intentionally not yet built.
type FactStore struct {
	m map[string][]string
}

// NewFactStore returns an empty fact store.
func NewFactStore() *FactStore { return &FactStore{m: map[string][]string{}} }

// Add records a value for a fact key, de-duplicating repeats.
func (f *FactStore) Add(key, value string) {
	if key == "" || value == "" {
		return
	}
	for _, v := range f.m[key] {
		if v == value {
			return
		}
	}
	f.m[key] = append(f.m[key], value)
}

// Has reports whether at least one value exists for the key.
func (f *FactStore) Has(key string) bool { return len(f.m[key]) > 0 }

// Values returns the collected values for a key (nil if none).
func (f *FactStore) Values(key string) []string { return f.m[key] }

// Keys returns the known fact keys, sorted — used for audit/debug detail.
func (f *FactStore) Keys() []string {
	keys := make([]string, 0, len(f.m))
	for k := range f.m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var factTokenRe = regexp.MustCompile(`\$\{([a-zA-Z0-9_.]+)\}`)

// Resolve substitutes ${key} tokens in s with the first collected value for
// each key. It returns the substituted string and the list of referenced keys
// that have no fact yet (so the caller can decide to skip rather than run a
// command with an unresolved placeholder).
func (f *FactStore) Resolve(s string) (out string, missing []string) {
	out = factTokenRe.ReplaceAllStringFunc(s, func(tok string) string {
		key := factTokenRe.FindStringSubmatch(tok)[1]
		if vals := f.m[key]; len(vals) > 0 {
			return vals[0]
		}
		missing = append(missing, key)
		return tok // leave the token in place so it's visible if mis-skipped
	})
	return out, missing
}

// Planner prepares techniques for a fact-aware sequential run (the "atomic
// planner": techniques run in scenario order, but each can consume facts the
// earlier ones produced and is gated on its declared requirements).
type Planner struct {
	Facts   *FactStore
	catalog *ttp.Catalog
}

// NewPlanner returns a Planner with an empty fact store and the built-in TTP
// catalog (used to find which primitive a technique compiles to, so its output
// can be parsed for facts).
func NewPlanner() *Planner {
	return &Planner{Facts: NewFactStore(), catalog: ttp.Default()}
}

// Prepare gates a technique on its requirements and resolves ${fact} references
// in its Inputs. skip=true means the technique must NOT run: either a declared
// requirement is uncollected, or an input references a fact that does not exist
// yet. reason explains why (recorded as the not_run Output). When skip=false the
// returned technique is a copy with its Inputs substituted.
func (p *Planner) Prepare(t domain.Technique) (prepared domain.Technique, skip bool, reason string) {
	for _, req := range t.Requires {
		if !p.Facts.Has(req) {
			return t, true, fmt.Sprintf("required fact %q not collected by an earlier technique", req)
		}
	}
	if len(t.Inputs) == 0 {
		return t, false, ""
	}
	newInputs := make(map[string]string, len(t.Inputs))
	for k, v := range t.Inputs {
		resolved, missing := p.Facts.Resolve(v)
		if len(missing) > 0 {
			return t, true, fmt.Sprintf("input %q references uncollected fact(s) %s", k, strings.Join(missing, ", "))
		}
		newInputs[k] = resolved
	}
	t.Inputs = newInputs
	return t, false, ""
}

// PrepareAll is the multi-value fan-out form of Prepare. When a technique's
// inputs reference a fact that has several collected values, it returns one
// prepared technique per value — the cartesian product across all referenced
// fact keys — so the technique runs once per discovered target (e.g. once per
// host.ip enumerated by an earlier step). With no fact references it returns a
// single technique, so the caller can always range over the result.
//
// skip/reason mirror Prepare: an unmet requirement, or a referenced fact with
// no values yet, yields skip=true and no techniques (recorded not_run upstream).
func (p *Planner) PrepareAll(t domain.Technique) (prepared []domain.Technique, skip bool, reason string) {
	for _, req := range t.Requires {
		if !p.Facts.Has(req) {
			return nil, true, fmt.Sprintf("required fact %q not collected by an earlier technique", req)
		}
	}
	keys := referencedKeys(t.Inputs)
	if len(keys) == 0 {
		return []domain.Technique{t}, false, ""
	}
	valuesByKey := make(map[string][]string, len(keys))
	for _, k := range keys {
		vals := p.Facts.Values(k)
		if len(vals) == 0 {
			return nil, true, fmt.Sprintf("input references uncollected fact %q", k)
		}
		valuesByKey[k] = vals
	}
	for _, assignment := range cartesian(keys, valuesByKey) {
		clone := t
		newInputs := make(map[string]string, len(t.Inputs))
		for ik, iv := range t.Inputs {
			newInputs[ik] = substituteAssignment(iv, assignment)
		}
		clone.Inputs = newInputs
		prepared = append(prepared, clone)
	}
	return prepared, false, ""
}

// referencedKeys returns the unique fact keys referenced by ${...} tokens across
// all input values, sorted for deterministic fan-out ordering.
func referencedKeys(inputs map[string]string) []string {
	set := map[string]bool{}
	for _, v := range inputs {
		for _, m := range factTokenRe.FindAllStringSubmatch(v, -1) {
			set[m[1]] = true
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// cartesian returns every assignment of one value to each key — the product of
// the per-key value lists. keys is iterated in order so the output is stable.
func cartesian(keys []string, valuesByKey map[string][]string) []map[string]string {
	combos := []map[string]string{{}}
	for _, k := range keys {
		var next []map[string]string
		for _, base := range combos {
			for _, v := range valuesByKey[k] {
				m := make(map[string]string, len(base)+1)
				for bk, bv := range base {
					m[bk] = bv
				}
				m[k] = v
				next = append(next, m)
			}
		}
		combos = next
	}
	return combos
}

// substituteAssignment replaces ${key} tokens using the given key→value
// assignment (every referenced key is present by construction).
func substituteAssignment(s string, assignment map[string]string) string {
	return factTokenRe.ReplaceAllStringFunc(s, func(tok string) string {
		key := factTokenRe.FindStringSubmatch(tok)[1]
		if v, ok := assignment[key]; ok {
			return v
		}
		return tok
	})
}

// Observe parses facts out of a successful technique's output and adds them to
// the store, so subsequent techniques can reference them. Non-success results
// and techniques with no catalog primitive contribute nothing.
func (p *Planner) Observe(t domain.Technique, res domain.Result) {
	if res.Status != domain.ExecSuccess || res.Output == "" {
		return
	}
	prim, ok, err := p.catalog.Compile(t)
	if !ok || err != nil {
		return
	}
	for key, vals := range parseFacts(prim.Kind, res.Output) {
		for _, v := range vals {
			p.Facts.Add(key, v)
		}
	}
}

var ipv4Re = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

// parseFacts extracts facts from a primitive's output. It is deliberately
// tolerant and framework-independent: it keys off the portable primitive kind,
// not any one C2's exact formatting. The current parser recognises routable
// IPv4 addresses (host.ip) in the output of the network/discovery primitives —
// a robust, format-agnostic signal. Richer parsers (usernames, share paths) are
// a small addition to this switch.
func parseFacts(kind c2.PrimitiveKind, output string) map[string][]string {
	facts := map[string][]string{}
	switch kind {
	case c2.PrimNetConfig, c2.PrimNetConnections, c2.PrimSysInfo,
		c2.PrimRemoteSystemDiscovery, c2.PrimShareDiscovery:
		for _, m := range ipv4Re.FindAllString(output, -1) {
			if isRoutableIPv4(m) {
				facts["host.ip"] = appendUnique(facts["host.ip"], m)
			}
		}
	}
	return facts
}

// isRoutableIPv4 reports whether s is a syntactically valid IPv4 address that is
// worth keeping as a target fact (excludes loopback, unspecified, link-local,
// multicast, and broadcast).
func isRoutableIPv4(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	if ip4.IsLoopback() || ip4.IsUnspecified() || ip4.IsLinkLocalUnicast() || ip4.IsMulticast() {
		return false
	}
	if ip4[0] == 255 && ip4[1] == 255 && ip4[2] == 255 && ip4[3] == 255 {
		return false
	}
	return true
}

func appendUnique(s []string, v string) []string {
	for _, e := range s {
		if e == v {
			return s
		}
	}
	return append(s, v)
}
