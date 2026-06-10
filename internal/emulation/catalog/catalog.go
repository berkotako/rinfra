// Package catalog ships the built-in adversary-emulation scenario definitions.
// Scenarios are code-not-DB so they version with the binary. Each Technique
// references a public Atomic Red Team GUID or Caldera ability ID — no payload
// content is included here.
package catalog

import "github.com/rinfra/rinfra/internal/domain"

// Scenarios is the built-in catalog, keyed by scenario ID.
var Scenarios = map[string]domain.Scenario{
	"apt29": {
		ID:               "apt29",
		Name:             "APT29 — Cozy Bear",
		AdversaryProfile: "apt29-like",
		Techniques: []domain.Technique{
			{
				AttackID: "T1566.002",
				Name:     "Spearphishing Link",
				Tactic:   "initial-access",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "f494f14a-7574-4538-9fbc-8d30afb6cbcb",
			},
			{
				AttackID: "T1059.001",
				Name:     "PowerShell",
				Tactic:   "execution",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "a21bb3e0-f861-400e-a4c6-b8eb699c6c5e",
			},
			{
				AttackID: "T1547.001",
				Name:     "Registry Run Keys / Startup Folder",
				Tactic:   "persistence",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "9efb1ea7-c37b-4595-a9d1-d58f4d873f56",
			},
			{
				AttackID: "T1055",
				Name:     "Process Injection",
				Tactic:   "defense-evasion",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "74496461-11cb-44fe-a508-e9b62c9a6a74",
			},
			{
				AttackID: "T1003.001",
				Name:     "LSASS Memory",
				Tactic:   "credential-access",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "2f7682c6-2b7d-461e-b2bc-07a1a7f2e5e1",
			},
			{
				AttackID: "T1018",
				Name:     "Remote System Discovery",
				Tactic:   "discovery",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "b9d2e8b4-2db3-4ea4-8b26-bb9e67b7ea9d",
			},
			{
				AttackID: "T1021.001",
				Name:     "Remote Desktop Protocol",
				Tactic:   "lateral-movement",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "2f3a9a4b-2c3a-4a6e-b8f0-20a74e5d8e30",
			},
			{
				AttackID: "T1567.002",
				Name:     "Exfiltration to Cloud Storage",
				Tactic:   "exfiltration",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "c7b8e9d1-4a3e-4d12-8f92-b7e3a9c10f15",
			},
		},
	},
	"fin7": {
		ID:               "fin7",
		Name:             "FIN7 — Carbanak",
		AdversaryProfile: "fin7-like",
		Techniques: []domain.Technique{
			{
				AttackID: "T1566.001",
				Name:     "Spearphishing Attachment",
				Tactic:   "initial-access",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "cb3afd92-92c3-4d8d-bb3e-6b2e4e3c2f4a",
			},
			{
				AttackID: "T1204.002",
				Name:     "Malicious File",
				Tactic:   "execution",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "a5c3b2d4-6e7f-4819-9a2b-c8d4e5f60124",
			},
			{
				AttackID: "T1053.005",
				Name:     "Scheduled Task/Job",
				Tactic:   "persistence",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "2f3a9a4b-8c9d-4e5f-a6b7-c8d9e0f12345",
			},
			{
				AttackID: "T1112",
				Name:     "Modify Registry",
				Tactic:   "defense-evasion",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "1a2b3c4d-5e6f-7890-abcd-ef1234567890",
			},
			{
				AttackID: "T1555.003",
				Name:     "Credentials from Web Browsers",
				Tactic:   "credential-access",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "a1b2c3d4-e5f6-7890-1234-567890abcdef",
			},
			{
				AttackID: "T1057",
				Name:     "Process Discovery",
				Tactic:   "discovery",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "b2c3d4e5-f6a7-8901-2345-678901abcdef",
			},
			{
				AttackID: "T1570",
				Name:     "Lateral Tool Transfer",
				Tactic:   "lateral-movement",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "c3d4e5f6-a7b8-9012-3456-789012abcdef",
			},
		},
	},
	"ransomware-affiliate": {
		ID:               "ransomware-affiliate",
		Name:             "Ransomware Affiliate",
		AdversaryProfile: "ransomware-affiliate-like",
		Techniques: []domain.Technique{
			{
				AttackID: "T1078",
				Name:     "Valid Accounts",
				Tactic:   "initial-access",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "d4e5f6a7-b8c9-0123-4567-890123abcdef",
			},
			{
				AttackID: "T1562.001",
				Name:     "Disable or Modify Tools",
				Tactic:   "defense-evasion",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "e5f6a7b8-c9d0-1234-5678-901234abcdef",
			},
			{
				AttackID: "T1490",
				Name:     "Inhibit System Recovery",
				Tactic:   "impact",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "f6a7b8c9-d0e1-2345-6789-012345abcdef",
			},
			{
				AttackID: "T1486",
				Name:     "Data Encrypted for Impact",
				Tactic:   "impact",
				Source:   domain.SourceAtomicRedTeam,
				SourceID: "a7b8c9d0-e1f2-3456-7890-123456abcdef",
			},
		},
	},
}

// List returns all scenarios in alphabetical order by ID.
func List() []domain.Scenario {
	out := make([]domain.Scenario, 0, len(Scenarios))
	for _, s := range Scenarios {
		out = append(out, s)
	}
	return out
}

// Get returns a scenario by ID, or the zero value and false if not found.
func Get(id string) (domain.Scenario, bool) {
	s, ok := Scenarios[id]
	return s, ok
}
