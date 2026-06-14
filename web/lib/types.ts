// Domain-aligned types matching internal/domain/*.go

// ---------- Cloud ----------
export type CloudProvider = "aws" | "gcp" | "azure" | "digitalocean";

export interface ProviderMeta {
  id: CloudProvider;
  name: string;
  label: string;
  color: string;
  short: string;
}

// ---------- Nodes ----------
// node type aligned to domain.NodeType
export type NodeType = "redirector" | "c2_server" | "payload_host";

// node status aligned to domain.NodeStatus
export type NodeStatus =
  | "pending"
  | "provisioning"
  | "live"
  | "draining"
  | "destroyed"
  | "failed";

export interface CanvasNode {
  id: string;
  type: NodeType;
  subtype: string; // HTTPS | HTTP | DNS | Sliver | Mythic | Staging etc.
  name: string;
  provider: CloudProvider;
  region: string;
  status: NodeStatus;
  health: number; // 0-100
  ip: string;
  framework?: string; // c2_server only
  listener?: string;  // c2_server only
  domain?: string;    // redirector / payload_host
  x: number;
  y: number;
  cost: number;
}

export interface CanvasEdge {
  id: string;
  from: string; // node id
  to: string;
}

// ---------- Engagement ----------
export type EngagementStatus =
  | "draft"
  | "authorized"
  | "active"
  | "provisioning" // computed infra state, surfaced on dashboard
  | "completed"
  | "archived";

export type AuthStatus = "authorized" | "pending" | "expired";

export interface Engagement {
  id: string;
  client: string;
  codename: string;
  scope: string;
  status: EngagementStatus;
  auth: AuthStatus;
  authBy: string;
  start: string; // ISO date
  end: string;
  assets: number;
  live: number;
  cost: number; // $/hr
  lead: string;
  targets: string[];
  frameworks: string[];
}

// ---------- C2 Frameworks ----------
export type C2Tier = "orchestrated" | "scripted" | "fronted";

export interface C2Framework {
  id: string;
  name: string;
  tier: C2Tier;
  tierLabel: string;
  note: string;
  gated: boolean;
  listeners: string[];
  lang: string;
}

// How RInfra drives a deployed teamserver: an automated operator API ("live"),
// partial scripting ("scripted"), or human-driven only ("manual").
export type OperatorMode = "live" | "scripted" | "manual";

// ManualAccess mirrors the control plane's c2.ManualAccess descriptor: how an
// operator connects a native client to a deployed teamserver by hand.
export interface ManualAccess {
  client: string; // native operator client, e.g. "sliver-client"
  protocol: string; // grpc-mtls | https | web-ui | tcp
  operatorPort: number;
  sshCommand: string; // ready-to-run ssh -L local-forward
  instructions: string;
}

// OperatorSession is an active implant/agent session reported by the operator API.
export interface OperatorSession {
  id: string;
  host: string;
  user: string;
  os: string;
}

// DeployedC2 is a provisioned teamserver surfaced in the console: both its
// automated-operator status and its manual-access path.
export interface DeployedC2 {
  nodeId: string;
  name: string;
  ip: string;
  status: NodeStatus;
  framework: string; // C2Framework id
  frameworkName: string;
  tier: C2Tier;
  listener: string;
  operatorMode: OperatorMode;
  liveClient: string; // label of the automated operator client ("" if manual-only)
  manual: ManualAccess;
  sessions: OperatorSession[];
}

// ---------- Emulation ----------
export interface Technique {
  id: string; // e.g. T1566.002
  name: string;
  tactic: string;
}

export interface Scenario {
  id: string;
  name: string;
  actor: string;
  desc: string;
  techniques: Technique[];
}

// ---------- Users, roles & projects ----------
// Aligned to internal/domain/user.go and project.go.
export type Role = "admin" | "lead" | "operator";

export interface User {
  id: string;
  username: string;
  displayName: string;
  email: string;
  role: Role;
  managerId: string;
  disabled: boolean;
  createdAt?: string;
  updatedAt?: string;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  clientName: string;
  leadId: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface ProjectMember {
  projectId: string;
  userId: string;
  addedAt?: string;
}

// ---------- Preferences ----------
export type AccentId = "indigo" | "slate" | "peri" | "steel";
export type NodeStyle = "soft" | "compact" | "outline";

export interface Preferences {
  theme: "light" | "dark";
  accentId: AccentId;
  nodeStyle: NodeStyle;
}

// ---------- Toasts ----------
export type ToastKind = "ok" | "warn" | "info" | "danger";

export interface Toast {
  id: number;
  msg: string;
  kind: ToastKind;
}

// ---------- Node templates (palette) ----------
export interface NodeTemplate {
  type: NodeType;
  subtype: string;
  label: string;
  icon: string;
  desc: string;
}
