package domain

import "time"

// CloudProviderType enumerates the supported clouds. One cloud.CloudProvider
// implementation exists per value.
type CloudProviderType string

const (
	CloudAWS          CloudProviderType = "aws"
	CloudGCP          CloudProviderType = "gcp"
	CloudAzure        CloudProviderType = "azure"
	CloudDigitalOcean CloudProviderType = "digitalocean"
)

// NodeType is the role a node plays in an attack-infrastructure topology.
type NodeType string

const (
	NodeRedirector  NodeType = "redirector"   // HTTP/HTTPS/DNS traffic relay
	NodeC2Server    NodeType = "c2_server"    // hosts a deployed C2 framework
	NodePayloadHost NodeType = "payload_host" // staging / hosting
)

// NodeStatus tracks a node through its lifecycle. Surfaced to the UI as a pill.
type NodeStatus string

const (
	NodePending      NodeStatus = "pending"
	NodeProvisioning NodeStatus = "provisioning"
	NodeLive         NodeStatus = "live"
	NodeDraining     NodeStatus = "draining"
	NodeDestroyed    NodeStatus = "destroyed"
	NodeFailed       NodeStatus = "failed"
)

// Health is a coarse runtime health signal shown on each node card.
type Health string

const (
	HealthHealthy  Health = "healthy"
	HealthDegraded Health = "degraded"
	HealthUnknown  Health = "unknown"
)

// NodeSpec is the desired state for a node, produced by the canvas. For a
// C2Server, C2Framework selects which c2.C2Provider deploys onto it.
type NodeSpec struct {
	Type        NodeType
	Cloud       CloudProviderType
	Region      string
	Size        string // provider-specific instance size
	C2Framework string // only for NodeC2Server, e.g. "sliver", "mythic"
	ProfileName string // redirector/listener profile to apply
	// Subtype disambiguates within a NodeType, e.g. "https", "dns", "http" for redirectors.
	Subtype string
}

// NodeCanvas holds the canvas-level state for a node. These fields are
// persisted with the topology so the UI can round-trip the diagram faithfully.
type NodeCanvas struct {
	Name         string  // operator-assigned label shown on the canvas card
	Listener     string  // listener profile or bind address for C2 nodes
	FrontDomain  string  // categorized domain used for traffic fronting
	CostEstimate float64 // estimated monthly cloud cost in USD
	X            int     // canvas X position (pixels)
	Y            int     // canvas Y position (pixels)
}

// Node is a provisioned (or to-be-provisioned) piece of infrastructure.
type Node struct {
	ID           string
	EngagementID string
	Spec         NodeSpec
	Canvas       NodeCanvas
	Status       NodeStatus
	Health       Health
	PublicIP     string
	ProviderRef  string // opaque cloud resource identifier for reconciliation
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Edge represents directed traffic flow between two nodes (e.g. a redirector
// forwarding to a C2 server).
type Edge struct {
	FromNodeID string
	ToNodeID   string
}

// Topology is the full graph for an engagement — what the canvas compiles to
// IaC.
type Topology struct {
	EngagementID string
	Nodes        []Node
	Edges        []Edge
}

// Rule is an ingress firewall rule. NOTE: this is the abstract form; each
// cloud.CloudProvider translates it to security groups (AWS), VPC firewall
// rules (GCP), cloud firewalls (DO), or NSGs (Azure). This translation is the
// most divergent part of the cloud layer — implement it carefully per provider.
type Rule struct {
	Protocol   string // "tcp", "udp"
	Port       int
	SourceCIDR string
	Allow      bool
}

// Record is a DNS record managed for redirector/categorized-domain setups.
type Record struct {
	Zone  string
	Name  string
	Type  string // "A", "CNAME", "TXT"
	Value string
	TTL   int
}

// Profile describes how a redirector rewrites/relays traffic. Kept abstract;
// adapters render it to concrete reverse-proxy config. (No domain-fronting —
// assume reverse-proxy + categorized domains.)
type Profile struct {
	Name        string
	RewriteHost string
	PathRules   []string
}
