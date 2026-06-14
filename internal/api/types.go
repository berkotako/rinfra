package api

import (
	"fmt"
	"time"

	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/domain"
)

// ---------- Request types ----------

type createEngagementRequest struct {
	Client         string   `json:"client"`
	Codename       string   `json:"codename"`
	ProjectID      string   `json:"projectId"`
	LeadOperator   string   `json:"leadOperator"`
	EngagementType string   `json:"engagementType"`
	Targets        []string `json:"targets"`
	Exclusions     []string `json:"exclusions"`
	ScopeNotes     string   `json:"scopeNotes"`
	RoEDocRef      string   `json:"roeDocRef"`
	WindowStart    string   `json:"windowStart"`
	WindowEnd      string   `json:"windowEnd"`
	Constraints    []string `json:"constraints"`
}

func (r createEngagementRequest) toDomain() (domain.Engagement, error) {
	e := domain.Engagement{
		Client:         r.Client,
		Codename:       r.Codename,
		ProjectID:      r.ProjectID,
		LeadOperator:   r.LeadOperator,
		EngagementType: domain.EngagementType(r.EngagementType),
		Status:         domain.EngagementDraft,
		Scope: domain.Scope{
			AllowedTargets: r.Targets,
			Exclusions:     r.Exclusions,
			Notes:          r.ScopeNotes,
		},
		RoE: domain.RulesOfEngagement{
			DocumentRef: r.RoEDocRef,
			Constraints: r.Constraints,
		},
	}
	if r.WindowStart != "" {
		t, err := time.Parse(time.RFC3339, r.WindowStart)
		if err != nil {
			return domain.Engagement{}, fmt.Errorf("windowStart must be an RFC3339 timestamp")
		}
		e.RoE.WindowStart = t
	}
	if r.WindowEnd != "" {
		t, err := time.Parse(time.RFC3339, r.WindowEnd)
		if err != nil {
			return domain.Engagement{}, fmt.Errorf("windowEnd must be an RFC3339 timestamp")
		}
		e.RoE.WindowEnd = t
	}
	if e.EngagementType == "" {
		e.EngagementType = domain.EngagementTypeRedTeam
	}
	return e, nil
}

type patchEngagementRequest struct {
	Status        string              `json:"status"`
	Authorization *authorizationPatch `json:"authorization"`
}

type authorizationPatch struct {
	AuthorizedBy string `json:"authorizedBy"`
	DocumentRef  string `json:"documentRef"`
	GrantedAt    string `json:"grantedAt"`
	ExpiresAt    string `json:"expiresAt"`
}

type nodeRequest struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"`
	Cloud        string  `json:"provider"`
	Region       string  `json:"region"`
	Size         string  `json:"size"`
	C2Framework  string  `json:"framework"`
	Subtype      string  `json:"subtype"`
	ProfileName  string  `json:"profileName"`
	Name         string  `json:"name"`
	Listener     string  `json:"listener"`
	FrontDomain  string  `json:"domain"`
	CostEstimate float64 `json:"cost"`
	X            int     `json:"x"`
	Y            int     `json:"y"`
}

func (n nodeRequest) toDomain() domain.Node {
	return domain.Node{
		ID: n.ID,
		Spec: domain.NodeSpec{
			Type:        domain.NodeType(n.Type),
			Cloud:       domain.CloudProviderType(n.Cloud),
			Region:      n.Region,
			Size:        n.Size,
			C2Framework: n.C2Framework,
			Subtype:     n.Subtype,
			ProfileName: n.ProfileName,
		},
		Canvas: domain.NodeCanvas{
			Name:         n.Name,
			Listener:     n.Listener,
			FrontDomain:  n.FrontDomain,
			CostEstimate: n.CostEstimate,
			X:            n.X,
			Y:            n.Y,
		},
	}
}

type edgeRequest struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

type topologyRequest struct {
	Nodes []nodeRequest `json:"nodes"`
	Edges []edgeRequest `json:"edges"`
}

func (t topologyRequest) toDomain(engagementID string) domain.Topology {
	nodes := make([]domain.Node, 0, len(t.Nodes))
	for _, n := range t.Nodes {
		nodes = append(nodes, n.toDomain())
	}
	edges := make([]domain.Edge, 0, len(t.Edges))
	for _, e := range t.Edges {
		edges = append(edges, domain.Edge{FromNodeID: e.From, ToNodeID: e.To})
	}
	return domain.Topology{
		EngagementID: engagementID,
		Nodes:        nodes,
		Edges:        edges,
	}
}

// credentialsRequest carries the credential key/value pairs for a provider.
// The Values map mirrors cloud.Credentials.Raw — provider-specific key names
// documented in each provider's package (e.g. "DIGITALOCEAN_TOKEN" for DO,
// "AWS_ACCESS_KEY_ID"/"AWS_SECRET_ACCESS_KEY"/"AWS_REGION" for AWS).
//
// Wire format: {"DIGITALOCEAN_TOKEN":"dop_v1_..."} for DigitalOcean.
// Deprecated: the legacy {"value":"..."} shape is no longer accepted.
type credentialsRequest struct {
	Values map[string]string `json:"values"`
}

type startRunRequest struct {
	ScenarioID string `json:"scenarioId"`
}

// ---------- Response types (JSON serialisation) ----------

func engagementToJSON(e domain.Engagement) map[string]any {
	return map[string]any{
		"id":             e.ID,
		"projectId":      e.ProjectID,
		"client":         e.Client,
		"codename":       e.Codename,
		"leadOperator":   e.LeadOperator,
		"engagementType": string(e.EngagementType),
		"status":         string(e.Status),
		"scope": map[string]any{
			"targets":    e.Scope.AllowedTargets,
			"exclusions": e.Scope.Exclusions,
			"notes":      e.Scope.Notes,
		},
		"roe": map[string]any{
			"documentRef": e.RoE.DocumentRef,
			"windowStart": nullableTime(e.RoE.WindowStart),
			"windowEnd":   nullableTime(e.RoE.WindowEnd),
			"constraints": e.RoE.Constraints,
		},
		"authorization": map[string]any{
			"authorizedBy": e.Authorization.AuthorizedBy,
			"documentRef":  e.Authorization.DocumentRef,
			"grantedAt":    nullableTime(e.Authorization.GrantedAt),
			"expiresAt":    nullableTime(e.Authorization.ExpiresAt),
		},
		"createdAt": e.CreatedAt,
		"updatedAt": e.UpdatedAt,
	}
}

func engagementsToJSON(engs []domain.Engagement) []map[string]any {
	out := make([]map[string]any, 0, len(engs))
	for _, e := range engs {
		out = append(out, engagementToJSON(e))
	}
	return out
}

func nodeToJSON(n domain.Node) map[string]any {
	return map[string]any{
		"id":          n.ID,
		"type":        string(n.Spec.Type),
		"provider":    string(n.Spec.Cloud),
		"region":      n.Spec.Region,
		"size":        n.Spec.Size,
		"framework":   n.Spec.C2Framework,
		"subtype":     n.Spec.Subtype,
		"profileName": n.Spec.ProfileName,
		"name":        n.Canvas.Name,
		"listener":    n.Canvas.Listener,
		"domain":      n.Canvas.FrontDomain,
		"cost":        n.Canvas.CostEstimate,
		"x":           n.Canvas.X,
		"y":           n.Canvas.Y,
		"status":      string(n.Status),
		"health":      string(n.Health),
		"ip":          n.PublicIP,
		"providerRef": n.ProviderRef,
	}
}

func topologyToJSON(t domain.Topology) map[string]any {
	nodes := make([]map[string]any, 0, len(t.Nodes))
	for _, n := range t.Nodes {
		nodes = append(nodes, nodeToJSON(n))
	}
	edges := make([]map[string]any, 0, len(t.Edges))
	for _, e := range t.Edges {
		edges = append(edges, map[string]any{
			"from": e.FromNodeID,
			"to":   e.ToNodeID,
		})
	}
	return map[string]any{
		"engagementId": t.EngagementID,
		"nodes":        nodes,
		"edges":        edges,
	}
}

func credentialMetaToJSON(m domain.CredentialMeta) map[string]any {
	return map[string]any{
		"id":           m.ID,
		"engagementId": m.EngagementID,
		"provider":     m.Provider,
		"keyId":        m.KeyID,
		"createdAt":    m.CreatedAt,
		"lastUsedAt":   m.LastUsedAt,
	}
}

func auditEventsToJSON(events []audit.Event) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		out = append(out, map[string]any{
			"id":           e.ID,
			"engagementId": e.EngagementID,
			"actor":        e.Actor,
			"action":       e.Action,
			"target":       e.Target,
			"detail":       e.Detail,
			"at":           e.At,
		})
	}
	return out
}

func runToJSON(r domain.ScenarioRun) map[string]any {
	results := make([]map[string]any, 0, len(r.Results))
	for _, res := range r.Results {
		results = append(results, map[string]any{
			"techniqueId": res.TechniqueAttackID,
			"status":      string(res.Status),
			"output":      res.Output,
			"startedAt":   res.StartedAt,
			"finishedAt":  res.FinishedAt,
			"err":         res.Err,
		})
	}
	return map[string]any{
		"id":           r.ID,
		"engagementId": r.EngagementID,
		"scenarioId":   r.ScenarioID,
		"status":       string(r.Status),
		"results":      results,
		"startedAt":    r.StartedAt,
		"finishedAt":   r.FinishedAt,
	}
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
