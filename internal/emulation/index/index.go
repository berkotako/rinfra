// Package index parses Security Risk Advisors "Index" YAML documents (the
// SecurityRiskAdvisors/indexes / VECTR "merged YAML" format) into RInfra's
// portable domain types, so a published benchmark index can be imported as a
// scenario plus its techniques.
//
// The upstream shape is a mapping whose top-level keys are ATT&CK tactic names
// ("Defense Evasion", "Credential Access", …) plus a "metadata" key. Each tactic
// maps to a list of test cases:
//
//	Defense Evasion:
//	  - name: "Disable Windows Firewall"
//	    description: "..."
//	    guidance:
//	      - "CMD> netsh advfirewall set allprofiles state off"
//	    block:  ["..."]
//	    detect: ["..."]
//	    metadata: { id: <uuid>, tid: T1562.004, tactic: TA0005 }
package index

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rinfra/rinfra/internal/domain"
)

type testCase struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Guidance    []string `yaml:"guidance"`
	Metadata    struct {
		ID     string `yaml:"id"`
		TID    string `yaml:"tid"`
		Tactic string `yaml:"tactic"`
	} `yaml:"metadata"`
}

type bundleMeta struct {
	Prefix string `yaml:"prefix"`
	Name   string `yaml:"name"`
	Bundle struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"bundle"`
}

// Parse converts an SRA index YAML document into a Scenario and its ordered
// techniques (document order is preserved). It returns an error for malformed
// YAML or a document with no recognizable test cases.
func Parse(data []byte) (domain.Scenario, []domain.Technique, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return domain.Scenario{}, nil, fmt.Errorf("parse index yaml: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return domain.Scenario{}, nil, fmt.Errorf("parse index yaml: expected a top-level mapping")
	}
	root := doc.Content[0]

	name := "Imported Index"
	var techs []domain.Technique

	// MappingNode content is [key, value, key, value, …] in document order.
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		val := root.Content[i+1]

		if strings.EqualFold(key, "metadata") {
			var m bundleMeta
			if err := val.Decode(&m); err == nil {
				switch {
				case m.Bundle.Name != "":
					name = m.Bundle.Name
				case m.Name != "":
					name = m.Name
				case m.Prefix != "":
					name = "Index " + m.Prefix
				}
			}
			continue
		}

		// Otherwise the key is a tactic and the value is a list of test cases.
		var cases []testCase
		if err := val.Decode(&cases); err != nil {
			continue // skip non-list sections defensively
		}
		for _, c := range cases {
			tid := strings.TrimSpace(c.Metadata.TID)
			if tid == "" || strings.TrimSpace(c.Name) == "" {
				continue
			}
			techs = append(techs, domain.Technique{
				AttackID:    tid,
				Name:        strings.TrimSpace(c.Name),
				Tactic:      key,
				Description: strings.TrimSpace(c.Description),
				Commands:    cleanGuidance(c.Guidance),
				Source:      domain.SourceAtomicRedTeam,
			})
		}
	}

	if len(techs) == 0 {
		return domain.Scenario{}, nil, fmt.Errorf("parse index yaml: no test cases with an ATT&CK id found")
	}

	sc := domain.Scenario{
		Name:             name,
		AdversaryProfile: "benchmark-index",
		Description:      fmt.Sprintf("Imported benchmark index — %d techniques across the year's top threat actors.", len(techs)),
		Techniques:       techs,
	}
	return sc, techs, nil
}

// cleanGuidance strips the SRA shell-prefix annotations ("CMD> ", "PS> ", "SH> ")
// from procedure lines, leaving the runnable command.
func cleanGuidance(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		for _, p := range []string{"CMD> ", "PS> ", "SH> ", "BASH> "} {
			if strings.HasPrefix(l, p) {
				l = strings.TrimPrefix(l, p)
				break
			}
		}
		out = append(out, l)
	}
	return out
}
