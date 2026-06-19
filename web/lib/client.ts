// RInfraClient interface — seam for REST and mock implementations.
// When NEXT_PUBLIC_RINFRA_API is set the RestClient is selected automatically;
// otherwise MockClient is used so the static demo build keeps working unchanged.
import type {
  Engagement,
  CanvasNode,
  CanvasEdge,
  C2Framework,
  Scenario,
  Technique,
  Coverage,
  Advisory,
  NodeStatus,
  EngagementStatus,
  User,
  Project,
  ProjectMember,
  Role,
  DeployedC2,
  C2Tier,
} from "./types";
import {
  ENGAGEMENTS,
  INITIAL_NODES,
  INITIAL_EDGES,
  C2_FRAMEWORKS,
  SCENARIOS,
  C2_OPERATOR_ACCESS,
  deployedC2FromNode,
  buildMockCoverage,
  buildImportedIndex,
  trmFromCounts,
  BUNDLED_ADVISORIES,
  MOCK_ADVISORY_SOURCES,
} from "./data";

function mapAdvisoryFromApi(a: Record<string, unknown>): Advisory {
  const ttps = Array.isArray(a["suggestedTtps"])
    ? (a["suggestedTtps"] as Record<string, unknown>[]).map((t) => ({
        attackId: String(t["attackId"] ?? ""),
        name: String(t["name"] ?? ""),
        tactic: String(t["tactic"] ?? ""),
        confidence: String(t["confidence"] ?? ""),
      }))
    : [];
  return {
    id: String(a["id"] ?? ""),
    source: String(a["source"] ?? ""),
    title: String(a["title"] ?? ""),
    vendor: String(a["vendor"] ?? ""),
    product: String(a["product"] ?? ""),
    published: String(a["published"] ?? ""),
    summary: String(a["summary"] ?? ""),
    url: String(a["url"] ?? ""),
    ransomware: Boolean(a["ransomware"]),
    suggestedTtps: ttps,
  };
}

// Builds an ATT&CK Navigator layer from a coverage rollup (used by MockClient;
// the REST backend produces its own).
function navigatorLayerFromCoverage(c: Coverage): unknown {
  const colors = ["", "#ffd966", "#6aa84f", "#274e13"];
  const techniques = c.tactics.flatMap((t) =>
    t.techniques.map((te) => ({
      techniqueID: te.attackID,
      score: te.level,
      color: colors[te.level] ?? "",
      enabled: te.level > 0,
    }))
  );
  return {
    name: "RInfra Coverage Export",
    versions: { attack: "14", navigator: "4.9", layer: "4.5" },
    domain: "enterprise-attack",
    description: `RInfra coverage export for engagement ${c.engagementId}`,
    techniques,
    gradient: { colors: ["#ffffff", "#ffd966", "#6aa84f", "#274e13"], minValue: 0, maxValue: 3 },
  };
}

// ---------- Typed error codes coming from the API error envelope ----------

export type ApiErrorCode =
  | "authorization_required"
  | "auth_expired"
  | "outside_window"
  | "empty_scope"
  | "job_running"
  | "not_found"
  | "bad_request"
  | "unauthorized"
  | "invalid_credentials"
  | "forbidden"
  | "username_taken"
  | "invalid_request"
  | "internal_error";

// ---------- Bearer-token session storage ----------
//
// The REST control plane authenticates with opaque bearer tokens (see
// internal/api/middleware.go). We persist the token in localStorage so a
// session survives a page reload; the RestClient attaches it as a
// Authorization header on every request.

const TOKEN_KEY = "rinfra-token";

let _token: string | null = null;

export function getAuthToken(): string | null {
  if (_token) return _token;
  if (typeof window === "undefined") return null;
  try {
    _token = localStorage.getItem(TOKEN_KEY);
  } catch {
    _token = null;
  }
  return _token;
}

export function setAuthToken(token: string | null): void {
  _token = token;
  if (typeof window === "undefined") return;
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {
    // ignore (private mode etc.)
  }
}

export class ApiError extends Error {
  readonly code: ApiErrorCode;
  readonly status: number;

  constructor(code: ApiErrorCode, message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }

  /** Human-readable hint suitable for a toast notification. */
  toastMessage(): string {
    switch (this.code) {
      case "authorization_required":
        return "Authorization required — engagement must be authorized before deploying.";
      case "auth_expired":
        return "Authorization has expired — re-authorize the engagement.";
      case "outside_window":
        return "Outside authorized window — cannot deploy outside the RoE window.";
      case "empty_scope":
        return "Empty scope — define targets before deploying.";
      case "job_running":
        return "A deploy or teardown job is already running for this engagement.";
      case "not_found":
        return "Resource not found.";
      default:
        return this.message;
    }
  }
}

// ---------- SSE event payloads ----------

export interface NodeStatusEvent {
  nodeId: string;
  status: NodeStatus;
  health: string;
  publicIp: string;
  providerRef: string;
}

export interface JobStatusEvent {
  jobId: string;
  status: "pending" | "running" | "done" | "failed";
}

export interface RunStatusEvent {
  runId: string;
  /** Present when a single technique completes. */
  techniqueId?: string;
  status: string;
}

export type SseEvent =
  | { kind: "node_status"; data: NodeStatusEvent }
  | { kind: "job_status"; data: JobStatusEvent }
  | { kind: "run_status"; data: RunStatusEvent };

export type SseHandler = (event: SseEvent) => void;

// ---------- Credential metadata ----------

export interface CredentialMeta {
  id: string;
  engagementId: string;
  provider: string;
  keyId: string;
  createdAt: string;
  lastUsedAt: string | null;
}

// ---------- Validation result ----------

export interface ValidationResult {
  valid: boolean;
  problems: string[];
}

// ---------- Run ----------

export interface TechniqueResult {
  techniqueId: string;
  status: string;
  output: string;
  startedAt: string | null;
  finishedAt: string | null;
  err: string;
}

export interface ScenarioRun {
  id: string;
  engagementId: string;
  scenarioId: string;
  status: string;
  results: TechniqueResult[];
  startedAt: string | null;
  finishedAt: string | null;
}

// ---------- Audit ----------

export interface AuditEvent {
  id: string;
  engagementId: string;
  actor: string;
  action: string;
  target: string;
  detail: string;
  at: string;
}

// ---------- The client interface ----------

export interface RInfraClient {
  // Authentication
  login(username: string, password: string): Promise<{ token: string; user: User }>;
  logout(): Promise<void>;
  me(): Promise<User>;

  // User administration
  listUsers(): Promise<User[]>;
  createUser(params: CreateUserParams): Promise<User>;
  updateUser(id: string, patch: UpdateUserParams): Promise<User>;
  changePassword(id: string, newPassword: string, currentPassword?: string): Promise<void>;

  // Projects & membership
  listProjects(): Promise<Project[]>;
  createProject(params: ProjectParams): Promise<Project>;
  updateProject(id: string, params: ProjectParams): Promise<Project>;
  deleteProject(id: string): Promise<void>;
  listProjectMembers(id: string): Promise<ProjectMember[]>;
  addProjectMember(id: string, userId: string): Promise<void>;
  removeProjectMember(id: string, userId: string): Promise<void>;
  listProjectEngagements(id: string): Promise<Engagement[]>;

  // Core read operations
  listEngagements(): Promise<Engagement[]>;
  getEngagement(id: string): Promise<Engagement>;
  createEngagement(params: CreateEngagementParams): Promise<Engagement>;
  patchEngagement(id: string, patch: PatchEngagementParams): Promise<Engagement>;

  // Topology
  getTopology(engagementId: string): Promise<{ nodes: CanvasNode[]; edges: CanvasEdge[] }>;
  putTopology(engagementId: string, nodes: CanvasNode[], edges: CanvasEdge[]): Promise<{ nodes: CanvasNode[]; edges: CanvasEdge[] }>;
  validateTopology(engagementId: string): Promise<ValidationResult>;

  // Provisioning
  deploy(engagementId: string): Promise<{ jobId: string }>;
  teardown(engagementId: string): Promise<{ jobId: string }>;

  // Credentials (write-only; GET returns metadata only)
  // values is a provider-specific key/value map (mirrors cloud.Credentials.Raw).
  // Examples: {"DIGITALOCEAN_TOKEN":"..."} for DO; {"AWS_ACCESS_KEY_ID":"...","AWS_SECRET_ACCESS_KEY":"...","AWS_REGION":"..."} for AWS.
  putCredentials(engagementId: string, provider: string, values: Record<string, string>): Promise<void>;
  getCredentialsMeta(engagementId: string, provider: string): Promise<CredentialMeta>;

  // SSE
  subscribeEvents(engagementId: string, handler: SseHandler): () => void;

  // Audit
  listAuditEvents(engagementId: string, limit?: number, offset?: number): Promise<AuditEvent[]>;

  // C2 frameworks
  listC2Frameworks(): Promise<C2Framework[]>;

  // Deployed teamservers: automated-operator status + manual-access path.
  listDeployedC2(engagementId?: string): Promise<DeployedC2[]>;

  // Scenarios & runs
  listScenarios(): Promise<Scenario[]>;
  createScenario(scenario: Scenario): Promise<Scenario>;
  importIndex(yaml: string): Promise<Scenario>;
  updateScenario(scenario: Scenario): Promise<Scenario>;
  deleteScenario(id: string): Promise<void>;
  startRun(engagementId: string, scenarioId: string): Promise<{ runId: string }>;
  getRun(runId: string): Promise<ScenarioRun>;
  // Purple-team scoring: record the defender outcome for a technique in a run.
  recordDetection(runId: string, techniqueId: string, outcome: string): Promise<void>;

  // TTP library — operator-authored techniques
  listTechniques(): Promise<Technique[]>;
  createTechnique(t: Technique): Promise<Technique>;
  updateTechnique(t: Technique): Promise<Technique>;
  deleteTechnique(id: string): Promise<void>;

  // Coverage & ATT&CK Navigator export
  getCoverage(engagementId: string): Promise<Coverage>;
  getNavigatorLayer(engagementId: string): Promise<unknown>;

  // Threat advisories (CISA KEV etc.) with suggested TTPs.
  listAdvisories(): Promise<Advisory[]>;
  // Which advisory resources are configured/collected (display only).
  listAdvisorySources(): Promise<string[]>;
}

export interface CreateUserParams {
  username: string;
  displayName?: string;
  email?: string;
  role: Role;
  password: string;
}

export interface UpdateUserParams {
  displayName?: string;
  email?: string;
  role?: Role;
  disabled?: boolean;
  managerId?: string;
}

export interface ProjectParams {
  name: string;
  description?: string;
  clientName?: string;
  leadId?: string;
}

export interface CreateEngagementParams {
  client: string;
  codename: string;
  projectId?: string;
  leadOperator: string;
  engagementType: string;
  targets: string[];
  exclusions: string[];
  scopeNotes: string;
  roeDocRef: string;
  windowStart: string;
  windowEnd: string;
  constraints: string[];
}

export interface PatchEngagementParams {
  status?: string;
  authorization?: {
    authorizedBy: string;
    documentRef: string;
    grantedAt: string;
    expiresAt: string;
  };
}

// ---------- Operator identity constant ----------

export const OPERATOR_HEADER = "X-RInfra-Operator";
export const DEFAULT_OPERATOR = "console-user";

// ---------- Mock directory (demo build only) ----------

const MOCK_USERS: User[] = [
  { id: "u-admin", username: "admin", displayName: "Administrator", email: "", role: "admin", managerId: "", disabled: false },
  { id: "u-lead", username: "j.rivera", displayName: "Jordan Rivera", email: "jordan@redcell.example", role: "lead", managerId: "", disabled: false },
  { id: "u-op1", username: "k.shaw", displayName: "Kai Shaw", email: "kai@redcell.example", role: "operator", managerId: "u-lead", disabled: false },
  { id: "u-op2", username: "m.olsen", displayName: "Morgan Olsen", email: "morgan@redcell.example", role: "operator", managerId: "u-lead", disabled: false },
];

const MOCK_PROJECTS: Project[] = [
  { id: "p-acme", name: "Acme Q2 Red Team", description: "Quarterly external + internal red team.", clientName: "Acme Corp", leadId: "u-lead" },
  { id: "p-globex", name: "Globex Purple Team", description: "Detection validation engagement.", clientName: "Globex", leadId: "u-lead" },
];

const MOCK_MEMBERS: ProjectMember[] = [
  { projectId: "p-acme", userId: "u-op1" },
  { projectId: "p-acme", userId: "u-op2" },
  { projectId: "p-globex", userId: "u-op1" },
];

let _mockSeq = 1;

// ---------- MockClient ----------

export class MockClient implements RInfraClient {
  // ---------- Auth (demo) ----------

  async login(username: string, password: string): Promise<{ token: string; user: User }> {
    // The static demo accepts the seeded admin/admin (and any known mock user
    // with password "admin") so the directory screens are explorable offline.
    const user = MOCK_USERS.find((u) => u.username === username && !u.disabled);
    if (!user || password !== "admin") {
      throw new ApiError("invalid_credentials", "Invalid username or password", 401);
    }
    return { token: "mock-token-" + user.id, user };
  }

  async logout(): Promise<void> {
    /* no-op in demo */
  }

  async me(): Promise<User> {
    return MOCK_USERS[0];
  }

  // ---------- Users (demo) ----------

  async listUsers(): Promise<User[]> {
    return [...MOCK_USERS];
  }

  async createUser(params: CreateUserParams): Promise<User> {
    if (MOCK_USERS.some((u) => u.username === params.username)) {
      throw new ApiError("username_taken", "Username already taken", 409);
    }
    const u: User = {
      id: "u-" + _mockSeq++,
      username: params.username,
      displayName: params.displayName ?? "",
      email: params.email ?? "",
      role: params.role,
      managerId: "",
      disabled: false,
    };
    MOCK_USERS.push(u);
    return u;
  }

  async updateUser(id: string, patch: UpdateUserParams): Promise<User> {
    const u = MOCK_USERS.find((x) => x.id === id);
    if (!u) throw new ApiError("not_found", "User not found", 404);
    if (patch.displayName !== undefined) u.displayName = patch.displayName;
    if (patch.email !== undefined) u.email = patch.email;
    if (patch.role !== undefined) u.role = patch.role;
    if (patch.disabled !== undefined) u.disabled = patch.disabled;
    if (patch.managerId !== undefined) u.managerId = patch.managerId;
    return { ...u };
  }

  async changePassword(): Promise<void> {
    /* no-op in demo */
  }

  // ---------- Projects (demo) ----------

  async listProjects(): Promise<Project[]> {
    return [...MOCK_PROJECTS];
  }

  async createProject(params: ProjectParams): Promise<Project> {
    const p: Project = {
      id: "p-" + _mockSeq++,
      name: params.name,
      description: params.description ?? "",
      clientName: params.clientName ?? "",
      leadId: params.leadId ?? "u-lead",
    };
    MOCK_PROJECTS.unshift(p);
    return p;
  }

  async updateProject(id: string, params: ProjectParams): Promise<Project> {
    const p = MOCK_PROJECTS.find((x) => x.id === id);
    if (!p) throw new ApiError("not_found", "Project not found", 404);
    p.name = params.name;
    p.description = params.description ?? p.description;
    p.clientName = params.clientName ?? p.clientName;
    if (params.leadId) p.leadId = params.leadId;
    return { ...p };
  }

  async deleteProject(id: string): Promise<void> {
    const idx = MOCK_PROJECTS.findIndex((x) => x.id === id);
    if (idx >= 0) MOCK_PROJECTS.splice(idx, 1);
  }

  async listProjectMembers(id: string): Promise<ProjectMember[]> {
    return MOCK_MEMBERS.filter((m) => m.projectId === id);
  }

  async addProjectMember(id: string, userId: string): Promise<void> {
    if (!MOCK_MEMBERS.some((m) => m.projectId === id && m.userId === userId)) {
      MOCK_MEMBERS.push({ projectId: id, userId });
    }
  }

  async removeProjectMember(id: string, userId: string): Promise<void> {
    const idx = MOCK_MEMBERS.findIndex((m) => m.projectId === id && m.userId === userId);
    if (idx >= 0) MOCK_MEMBERS.splice(idx, 1);
  }

  async listProjectEngagements(): Promise<Engagement[]> {
    return ENGAGEMENTS;
  }

  async listEngagements(): Promise<Engagement[]> {
    return ENGAGEMENTS;
  }

  async getEngagement(id: string): Promise<Engagement> {
    const e = ENGAGEMENTS.find((e) => e.id === id);
    if (!e) throw new ApiError("not_found", `Engagement ${id} not found`, 404);
    return e;
  }

  async createEngagement(params: CreateEngagementParams): Promise<Engagement> {
    const e: Engagement = {
      id: "ENG-" + (2412 + Math.floor(Math.random() * 80)),
      client: params.client,
      codename: params.codename,
      scope: params.scopeNotes || "External perimeter",
      status: "draft",
      auth: "pending",
      authBy: "—",
      start: params.windowStart ? params.windowStart.slice(0, 10) : "2026-01-01",
      end: params.windowEnd ? params.windowEnd.slice(0, 10) : "2026-12-31",
      assets: 0,
      live: 0,
      cost: 0,
      lead: params.leadOperator,
      targets: params.targets,
      frameworks: [],
    };
    ENGAGEMENTS.unshift(e);
    return e;
  }

  async patchEngagement(id: string, patch: PatchEngagementParams): Promise<Engagement> {
    const idx = ENGAGEMENTS.findIndex((e) => e.id === id);
    if (idx < 0) throw new ApiError("not_found", `Engagement ${id} not found`, 404);
    const e = { ...ENGAGEMENTS[idx] };
    if (patch.status) e.status = patch.status as EngagementStatus;
    if (patch.authorization) {
      e.auth = "authorized";
      e.authBy = `${patch.authorization.authorizedBy}`;
    }
    ENGAGEMENTS[idx] = e;
    return e;
  }

  async getTopology(engagementId: string): Promise<{ nodes: CanvasNode[]; edges: CanvasEdge[] }> {
    void engagementId;
    return { nodes: INITIAL_NODES, edges: INITIAL_EDGES };
  }

  async putTopology(
    engagementId: string,
    nodes: CanvasNode[],
    edges: CanvasEdge[]
  ): Promise<{ nodes: CanvasNode[]; edges: CanvasEdge[] }> {
    void engagementId;
    return { nodes, edges };
  }

  async validateTopology(engagementId: string): Promise<ValidationResult> {
    void engagementId;
    return { valid: true, problems: [] };
  }

  async deploy(engagementId: string): Promise<{ jobId: string }> {
    void engagementId;
    return { jobId: "mock-job-" + Date.now() };
  }

  async teardown(engagementId: string): Promise<{ jobId: string }> {
    void engagementId;
    return { jobId: "mock-job-" + Date.now() };
  }

  async putCredentials(engagementId: string, provider: string, values: Record<string, string>): Promise<void> {
    void engagementId;
    void provider;
    void values;
  }

  async getCredentialsMeta(engagementId: string, provider: string): Promise<CredentialMeta> {
    return {
      id: "mock-cred",
      engagementId,
      provider,
      keyId: "mock-key",
      createdAt: new Date().toISOString(),
      lastUsedAt: null,
    };
  }

  subscribeEvents(engagementId: string, handler: SseHandler): () => void {
    void engagementId;
    void handler;
    // Mock mode: no real SSE; the store's local simulation runs instead.
    return () => undefined;
  }

  async listAuditEvents(
    engagementId: string,
    limit?: number,
    offset?: number
  ): Promise<AuditEvent[]> {
    void engagementId;
    void limit;
    void offset;
    return [];
  }

  async listC2Frameworks(): Promise<C2Framework[]> {
    return C2_FRAMEWORKS;
  }

  async listDeployedC2(engagementId?: string): Promise<DeployedC2[]> {
    void engagementId;
    return INITIAL_NODES.map(deployedC2FromNode).filter(
      (d): d is DeployedC2 => d !== null
    );
  }

  async listScenarios(): Promise<Scenario[]> {
    return SCENARIOS;
  }

  async createScenario(scenario: Scenario): Promise<Scenario> {
    // Mock mode: echo back; the store keeps it session-local.
    return scenario;
  }

  async importIndex(yaml: string): Promise<Scenario> {
    // Mock mode: the static demo can't run the Go YAML parser, so return the
    // bundled sample index's parsed equivalent (the real backend parses uploads).
    void yaml;
    return buildImportedIndex().scenario;
  }

  async updateScenario(scenario: Scenario): Promise<Scenario> {
    return scenario;
  }

  async deleteScenario(id: string): Promise<void> {
    void id;
  }

  async startRun(engagementId: string, scenarioId: string): Promise<{ runId: string }> {
    void engagementId;
    void scenarioId;
    return { runId: "mock-run-" + Date.now() };
  }

  async getRun(runId: string): Promise<ScenarioRun> {
    return {
      id: runId,
      engagementId: "",
      scenarioId: "",
      status: "done",
      results: [],
      startedAt: null,
      finishedAt: null,
    };
  }

  async recordDetection(runId: string, techniqueId: string, outcome: string): Promise<void> {
    void runId;
    void techniqueId;
    void outcome; // mock: scoring is session-local
  }

  // Mock mode: authored TTPs are kept session-local by the store.
  async listTechniques(): Promise<Technique[]> {
    return [];
  }
  async createTechnique(t: Technique): Promise<Technique> {
    return t;
  }
  async updateTechnique(t: Technique): Promise<Technique> {
    return t;
  }
  async deleteTechnique(id: string): Promise<void> {
    void id;
  }

  async getCoverage(engagementId: string): Promise<Coverage> {
    return buildMockCoverage(engagementId);
  }

  async getNavigatorLayer(engagementId: string): Promise<unknown> {
    return navigatorLayerFromCoverage(buildMockCoverage(engagementId));
  }

  async listAdvisories(): Promise<Advisory[]> {
    return BUNDLED_ADVISORIES;
  }

  async listAdvisorySources(): Promise<string[]> {
    return MOCK_ADVISORY_SOURCES;
  }
}

// ---------- RestClient ----------

function mapHealth(h: string): number {
  // The API returns domain.NodeHealth strings; map to 0-100 for the UI.
  switch (h) {
    case "healthy":
      return 95;
    case "degraded":
      return 50;
    case "unknown":
    default:
      return 0;
  }
}

function mapNodeFromApi(n: Record<string, unknown>): CanvasNode {
  return {
    id: String(n["id"] ?? ""),
    type: (n["type"] as CanvasNode["type"]) ?? "redirector",
    subtype: String(n["subtype"] ?? ""),
    name: String(n["name"] ?? ""),
    provider: (n["provider"] as CanvasNode["provider"]) ?? "aws",
    region: String(n["region"] ?? ""),
    status: (n["status"] as NodeStatus) || "pending",
    health: mapHealth(String(n["health"] ?? "unknown")),
    ip: String(n["ip"] ?? "—") || "—",
    framework: n["framework"] ? String(n["framework"]) : undefined,
    listener: n["listener"] ? String(n["listener"]) : undefined,
    domain: n["domain"] ? String(n["domain"]) : undefined,
    x: Number(n["x"] ?? 0),
    y: Number(n["y"] ?? 0),
    cost: Number(n["cost"] ?? 0),
  };
}

function mapEngagementFromApi(e: Record<string, unknown>): Engagement {
  const scope = e["scope"] as Record<string, unknown> | undefined;
  const roe = e["roe"] as Record<string, unknown> | undefined;
  const auth = e["authorization"] as Record<string, unknown> | undefined;

  // Derive auth status from authorization fields.
  let authStatus: Engagement["auth"] = "pending";
  if (auth?.authorizedBy) {
    const expiresAt = auth.expiresAt ? new Date(String(auth.expiresAt)) : null;
    if (expiresAt && expiresAt < new Date()) {
      authStatus = "expired";
    } else {
      authStatus = "authorized";
    }
  }

  const windowStart = roe?.windowStart ? String(roe.windowStart).slice(0, 10) : "";
  const windowEnd = roe?.windowEnd ? String(roe.windowEnd).slice(0, 10) : "";
  const targets = Array.isArray(scope?.targets) ? (scope.targets as string[]) : [];

  return {
    id: String(e["id"] ?? ""),
    client: String(e["client"] ?? ""),
    codename: String(e["codename"] ?? ""),
    scope: String(scope?.notes ?? targets.join(", ") ?? ""),
    status: (e["status"] as EngagementStatus) ?? "draft",
    auth: authStatus,
    authBy: auth?.authorizedBy ? String(auth.authorizedBy) : "—",
    start: windowStart,
    end: windowEnd,
    assets: 0, // Computed from topology; not in engagement response.
    live: 0,   // Computed from topology; not in engagement response.
    cost: 0,   // Computed from node costs; not in engagement response.
    lead: String(e["leadOperator"] ?? ""),
    targets,
    frameworks: [],
  };
}

function mapC2FrameworkFromApi(f: Record<string, unknown>): C2Framework {
  const tier = String(f["tier"] ?? "fronted") as C2Framework["tier"];
  const tierLabels: Record<string, string> = {
    orchestrated: "Fully orchestrated",
    scripted: "Scripted",
    fronted: "Deploy & operate manually",
  };
  return {
    id: String(f["id"] ?? ""),
    name: String(f["name"] ?? ""),
    tier,
    tierLabel: tierLabels[tier] ?? tier,
    note: "",
    gated: Boolean(f["gated"]),
    listeners: [],
    lang: "",
  };
}

// Backend framework name -> frontend C2Framework id (mostly identical).
const FRAMEWORK_ALIAS: Record<string, string> = { cobaltstrike: "cobalt" };

// Maps the control plane's c2.ManualAccessView JSON to a DeployedC2.
function mapManualAccessFromApi(b: Record<string, unknown>): DeployedC2 | null {
  const apiFw = String(b["framework"] ?? "");
  if (!apiFw) return null;
  const fwId = FRAMEWORK_ALIAS[apiFw] ?? apiFw;
  const fw = C2_FRAMEWORKS.find((f) => f.id === fwId);
  const tier: C2Tier = fw?.tier ?? "fronted";
  const mode: DeployedC2["operatorMode"] =
    tier === "orchestrated" ? "live" : tier === "scripted" ? "scripted" : "manual";
  const access = C2_OPERATOR_ACCESS[fwId];
  return {
    nodeId: String(b["nodeId"] ?? ""),
    name: fw?.name ?? apiFw,
    ip: String(b["host"] ?? ""),
    status: "live",
    framework: fwId,
    frameworkName: fw?.name ?? apiFw,
    tier,
    listener: fw?.listeners[0] ?? "",
    operatorMode: mode,
    liveClient: mode === "manual" ? "" : access?.liveClient ?? "operator API",
    manual: {
      client: String(b["client"] ?? access?.client ?? ""),
      protocol: String(b["protocol"] ?? ""),
      operatorPort: Number(b["operatorPort"] ?? 0),
      sshCommand: String(b["sshCommand"] ?? ""),
      instructions: String(b["instructions"] ?? ""),
    },
    sessions: [],
  };
}

function mapTechniqueFromApi(t: Record<string, unknown>): Technique {
  return {
    id: String(t["id"] ?? ""),
    name: String(t["name"] ?? ""),
    tactic: String(t["tactic"] ?? ""),
    description: t["description"] ? String(t["description"]) : undefined,
    commands: Array.isArray(t["commands"]) ? (t["commands"] as unknown[]).map(String) : undefined,
  };
}

function mapScenarioFromApi(s: Record<string, unknown>): Scenario {
  const techniques = Array.isArray(s["techniques"])
    ? (s["techniques"] as Record<string, unknown>[]).map((t) => ({
        id: String(t["id"] ?? ""),
        name: String(t["name"] ?? ""),
        tactic: String(t["tactic"] ?? ""),
      }))
    : [];
  return {
    id: String(s["id"] ?? ""),
    name: String(s["name"] ?? ""),
    actor: String(s["actor"] ?? ""),
    desc: String(s["desc"] ?? ""),
    techniques,
  };
}

function mapUserFromApi(u: Record<string, unknown>): User {
  return {
    id: String(u["id"] ?? ""),
    username: String(u["username"] ?? ""),
    displayName: String(u["displayName"] ?? ""),
    email: String(u["email"] ?? ""),
    role: (String(u["role"] ?? "operator") as Role),
    managerId: String(u["managerId"] ?? ""),
    disabled: Boolean(u["disabled"]),
    createdAt: u["createdAt"] ? String(u["createdAt"]) : undefined,
    updatedAt: u["updatedAt"] ? String(u["updatedAt"]) : undefined,
  };
}

function mapProjectFromApi(p: Record<string, unknown>): Project {
  return {
    id: String(p["id"] ?? ""),
    name: String(p["name"] ?? ""),
    description: String(p["description"] ?? ""),
    clientName: String(p["clientName"] ?? ""),
    leadId: String(p["leadId"] ?? ""),
    createdAt: p["createdAt"] ? String(p["createdAt"]) : undefined,
    updatedAt: p["updatedAt"] ? String(p["updatedAt"]) : undefined,
  };
}

function mapMemberFromApi(m: Record<string, unknown>): ProjectMember {
  return {
    projectId: String(m["projectId"] ?? ""),
    userId: String(m["userId"] ?? ""),
    addedAt: m["addedAt"] ? String(m["addedAt"]) : undefined,
  };
}

export class RestClient implements RInfraClient {
  private readonly base: string;
  private readonly operator: string;

  constructor(base: string, operator = DEFAULT_OPERATOR) {
    this.base = base.replace(/\/$/, "");
    this.operator = operator;
  }

  private headers(): Record<string, string> {
    const h: Record<string, string> = {
      "Content-Type": "application/json",
      [OPERATOR_HEADER]: this.operator,
    };
    const token = getAuthToken();
    if (token) h["Authorization"] = `Bearer ${token}`;
    return h;
  }

  private async fetch<T>(
    path: string,
    init?: RequestInit
  ): Promise<T> {
    const res = await fetch(`${this.base}/api/v1${path}`, {
      ...init,
      headers: {
        ...this.headers(),
        ...(init?.headers as Record<string, string> | undefined),
      },
    });

    if (res.status === 204) {
      return undefined as T;
    }

    // Parse defensively: a non-JSON body (5xx HTML, proxy error page, or a
    // plain-text error) must still surface as a typed ApiError, not a raw
    // SyntaxError from res.json().
    const text = await res.text();
    let body: Record<string, unknown> = {};
    if (text) {
      try {
        body = JSON.parse(text) as Record<string, unknown>;
      } catch {
        if (!res.ok) {
          throw new ApiError(
            "internal_error",
            text.slice(0, 200) || `Request failed (${res.status})`,
            res.status
          );
        }
        throw new ApiError(
          "internal_error",
          "Malformed response from server",
          res.status
        );
      }
    }

    if (!res.ok) {
      const err = body["error"] as Record<string, unknown> | undefined;
      const code = (err?.["code"] ?? "internal_error") as ApiErrorCode;
      const message = String(err?.["message"] ?? "An error occurred");
      throw new ApiError(code, message, res.status);
    }

    return body as T;
  }

  // ---------- Auth ----------

  async login(username: string, password: string): Promise<{ token: string; user: User }> {
    const body = await this.fetch<{ token: string; user: Record<string, unknown> }>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    });
    return { token: String(body.token ?? ""), user: mapUserFromApi(body.user ?? {}) };
  }

  async logout(): Promise<void> {
    await this.fetch<undefined>("/auth/logout", { method: "POST" });
  }

  async me(): Promise<User> {
    const body = await this.fetch<{ user: Record<string, unknown> }>("/auth/me");
    return mapUserFromApi(body.user ?? {});
  }

  // ---------- Users ----------

  async listUsers(): Promise<User[]> {
    const body = await this.fetch<{ users: Record<string, unknown>[] }>("/users");
    return (body.users ?? []).map(mapUserFromApi);
  }

  async createUser(params: CreateUserParams): Promise<User> {
    const body = await this.fetch<{ user: Record<string, unknown> }>("/users", {
      method: "POST",
      body: JSON.stringify(params),
    });
    return mapUserFromApi(body.user ?? {});
  }

  async updateUser(id: string, patch: UpdateUserParams): Promise<User> {
    const body = await this.fetch<{ user: Record<string, unknown> }>(`/users/${id}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    });
    return mapUserFromApi(body.user ?? {});
  }

  async changePassword(id: string, newPassword: string, currentPassword?: string): Promise<void> {
    await this.fetch<undefined>(`/users/${id}/password`, {
      method: "POST",
      body: JSON.stringify({ newPassword, currentPassword: currentPassword ?? "" }),
    });
  }

  // ---------- Projects ----------

  async listProjects(): Promise<Project[]> {
    const body = await this.fetch<{ projects: Record<string, unknown>[] }>("/projects");
    return (body.projects ?? []).map(mapProjectFromApi);
  }

  async createProject(params: ProjectParams): Promise<Project> {
    const body = await this.fetch<{ project: Record<string, unknown> }>("/projects", {
      method: "POST",
      body: JSON.stringify(params),
    });
    return mapProjectFromApi(body.project ?? {});
  }

  async updateProject(id: string, params: ProjectParams): Promise<Project> {
    const body = await this.fetch<{ project: Record<string, unknown> }>(`/projects/${id}`, {
      method: "PATCH",
      body: JSON.stringify(params),
    });
    return mapProjectFromApi(body.project ?? {});
  }

  async deleteProject(id: string): Promise<void> {
    await this.fetch<undefined>(`/projects/${id}`, { method: "DELETE" });
  }

  async listProjectMembers(id: string): Promise<ProjectMember[]> {
    const body = await this.fetch<{ members: Record<string, unknown>[] }>(`/projects/${id}/members`);
    return (body.members ?? []).map(mapMemberFromApi);
  }

  async addProjectMember(id: string, userId: string): Promise<void> {
    await this.fetch<undefined>(`/projects/${id}/members`, {
      method: "POST",
      body: JSON.stringify({ userId }),
    });
  }

  async removeProjectMember(id: string, userId: string): Promise<void> {
    await this.fetch<undefined>(`/projects/${id}/members/${userId}`, { method: "DELETE" });
  }

  async listProjectEngagements(id: string): Promise<Engagement[]> {
    const body = await this.fetch<{ engagements: Record<string, unknown>[] }>(`/projects/${id}/engagements`);
    return (body.engagements ?? []).map(mapEngagementFromApi);
  }

  // ---------- Engagements ----------

  async listEngagements(): Promise<Engagement[]> {
    const body = await this.fetch<{ engagements: Record<string, unknown>[] }>("/engagements");
    return (body.engagements ?? []).map(mapEngagementFromApi);
  }

  async getEngagement(id: string): Promise<Engagement> {
    const body = await this.fetch<{ engagement: Record<string, unknown> }>(`/engagements/${id}`);
    return mapEngagementFromApi(body.engagement);
  }

  async createEngagement(params: CreateEngagementParams): Promise<Engagement> {
    const body = await this.fetch<{ engagement: Record<string, unknown> }>("/engagements", {
      method: "POST",
      body: JSON.stringify(params),
    });
    return mapEngagementFromApi(body.engagement);
  }

  async patchEngagement(id: string, patch: PatchEngagementParams): Promise<Engagement> {
    const body = await this.fetch<{ engagement: Record<string, unknown> }>(`/engagements/${id}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    });
    return mapEngagementFromApi(body.engagement);
  }

  // ---------- Topology ----------

  async getTopology(engagementId: string): Promise<{ nodes: CanvasNode[]; edges: CanvasEdge[] }> {
    const body = await this.fetch<{
      nodes: Record<string, unknown>[];
      edges: Record<string, unknown>[];
    }>(`/engagements/${engagementId}/topology`);
    const nodes = (body.nodes ?? []).map(mapNodeFromApi);
    const edges: CanvasEdge[] = (body.edges ?? []).map((e, i) => ({
      id: String(e["id"] ?? `e${i}`),
      from: String(e["from"] ?? ""),
      to: String(e["to"] ?? ""),
    }));
    return { nodes, edges };
  }

  async putTopology(
    engagementId: string,
    nodes: CanvasNode[],
    edges: CanvasEdge[]
  ): Promise<{ nodes: CanvasNode[]; edges: CanvasEdge[] }> {
    // Map UI CanvasNode to the flat API wire format.
    const apiNodes = nodes.map((n) => ({
      id: n.id,
      type: n.type,
      provider: n.provider,
      region: n.region,
      size: "",
      framework: n.framework ?? "",
      subtype: n.subtype,
      profileName: "",
      name: n.name,
      listener: n.listener ?? "",
      domain: n.domain ?? "",
      cost: n.cost,
      x: n.x,
      y: n.y,
    }));
    const apiEdges = edges.map((e) => ({ id: e.id, from: e.from, to: e.to }));

    const body = await this.fetch<{
      nodes: Record<string, unknown>[];
      edges: Record<string, unknown>[];
    }>(`/engagements/${engagementId}/topology`, {
      method: "PUT",
      body: JSON.stringify({ nodes: apiNodes, edges: apiEdges }),
    });

    const outNodes = (body.nodes ?? []).map(mapNodeFromApi);
    const outEdges: CanvasEdge[] = (body.edges ?? []).map((e, i) => ({
      id: String(e["id"] ?? `e${i}`),
      from: String(e["from"] ?? ""),
      to: String(e["to"] ?? ""),
    }));
    return { nodes: outNodes, edges: outEdges };
  }

  async validateTopology(engagementId: string): Promise<ValidationResult> {
    const body = await this.fetch<{ valid: boolean; problems: string[] }>(
      `/engagements/${engagementId}/validate`,
      { method: "POST" }
    );
    return { valid: body.valid, problems: body.problems ?? [] };
  }

  // ---------- Deploy / Teardown ----------

  async deploy(engagementId: string): Promise<{ jobId: string }> {
    const body = await this.fetch<{ jobId: string }>(
      `/engagements/${engagementId}/deploy`,
      { method: "POST" }
    );
    return { jobId: body.jobId };
  }

  async teardown(engagementId: string): Promise<{ jobId: string }> {
    const body = await this.fetch<{ jobId: string }>(
      `/engagements/${engagementId}/teardown`,
      { method: "POST" }
    );
    return { jobId: body.jobId };
  }

  // ---------- Credentials ----------

  async putCredentials(engagementId: string, provider: string, values: Record<string, string>): Promise<void> {
    await this.fetch<undefined>(
      `/engagements/${engagementId}/credentials/${provider}`,
      { method: "PUT", body: JSON.stringify({ values }) }
    );
  }

  async getCredentialsMeta(engagementId: string, provider: string): Promise<CredentialMeta> {
    const body = await this.fetch<Record<string, unknown>>(
      `/engagements/${engagementId}/credentials/${provider}`
    );
    return {
      id: String(body["id"] ?? ""),
      engagementId: String(body["engagementId"] ?? engagementId),
      provider: String(body["provider"] ?? provider),
      keyId: String(body["keyId"] ?? ""),
      createdAt: String(body["createdAt"] ?? ""),
      lastUsedAt: body["lastUsedAt"] ? String(body["lastUsedAt"]) : null,
    };
  }

  // ---------- SSE ----------

  subscribeEvents(engagementId: string, handler: SseHandler): () => void {
    const url = `${this.base}/api/v1/engagements/${engagementId}/events`;
    const es = new EventSource(url);

    es.addEventListener("node_status", (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data as string) as NodeStatusEvent;
        handler({ kind: "node_status", data });
      } catch {
        // ignore malformed event
      }
    });

    es.addEventListener("job_status", (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data as string) as JobStatusEvent;
        handler({ kind: "job_status", data });
      } catch {
        // ignore malformed event
      }
    });

    es.addEventListener("run_status", (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data as string) as RunStatusEvent;
        handler({ kind: "run_status", data });
      } catch {
        // ignore malformed event
      }
    });

    return () => es.close();
  }

  // ---------- Audit ----------

  async listAuditEvents(
    engagementId: string,
    limit = 50,
    offset = 0
  ): Promise<AuditEvent[]> {
    const body = await this.fetch<{ events: Record<string, unknown>[] }>(
      `/engagements/${engagementId}/audit?limit=${limit}&offset=${offset}`
    );
    return (body.events ?? []).map((e) => ({
      id: String(e["id"] ?? ""),
      engagementId: String(e["engagementId"] ?? ""),
      actor: String(e["actor"] ?? ""),
      action: String(e["action"] ?? ""),
      target: String(e["target"] ?? ""),
      detail: String(e["detail"] ?? ""),
      at: String(e["at"] ?? ""),
    }));
  }

  // ---------- C2 Frameworks ----------

  async listC2Frameworks(): Promise<C2Framework[]> {
    const body = await this.fetch<{ frameworks: Record<string, unknown>[] }>("/c2/frameworks");
    return (body.frameworks ?? []).map(mapC2FrameworkFromApi);
  }

  async listDeployedC2(engagementId?: string): Promise<DeployedC2[]> {
    if (!engagementId) return [];
    try {
      const body = await this.fetch<{ teamservers: Record<string, unknown>[] }>(
        `/engagements/${engagementId}/c2/teamservers`
      );
      return (body.teamservers ?? [])
        .map(mapManualAccessFromApi)
        .filter((d): d is DeployedC2 => d !== null);
    } catch (e) {
      // No live C2 node yet (or not authorized) — surface an empty list.
      if (e instanceof ApiError && (e.status === 404 || e.code === "not_found")) {
        return [];
      }
      throw e;
    }
  }

  // ---------- Scenarios & Runs ----------

  async listScenarios(): Promise<Scenario[]> {
    const body = await this.fetch<{ scenarios: Record<string, unknown>[] }>("/scenarios");
    return (body.scenarios ?? []).map(mapScenarioFromApi);
  }

  async createScenario(scenario: Scenario): Promise<Scenario> {
    const body = await this.fetch<{ scenario: Record<string, unknown> }>("/scenarios", {
      method: "POST",
      body: JSON.stringify({
        name: scenario.name,
        actor: scenario.actor,
        desc: scenario.desc,
        techniques: scenario.techniques.map((t) => ({
          id: t.id,
          name: t.name,
          tactic: t.tactic,
        })),
      }),
    });
    return mapScenarioFromApi(body.scenario ?? {});
  }

  async importIndex(yaml: string): Promise<Scenario> {
    const body = await this.fetch<{ scenario: Record<string, unknown> }>("/scenarios/import", {
      method: "POST",
      headers: { "Content-Type": "application/x-yaml" },
      body: yaml,
    });
    return mapScenarioFromApi(body.scenario ?? {});
  }

  async updateScenario(scenario: Scenario): Promise<Scenario> {
    const body = await this.fetch<{ scenario: Record<string, unknown> }>(
      `/scenarios/${scenario.id}`,
      {
        method: "PUT",
        body: JSON.stringify({
          name: scenario.name,
          actor: scenario.actor,
          desc: scenario.desc,
          techniques: scenario.techniques.map((t) => ({ id: t.id, name: t.name, tactic: t.tactic })),
        }),
      }
    );
    return mapScenarioFromApi(body.scenario ?? {});
  }

  async deleteScenario(id: string): Promise<void> {
    await this.fetch<undefined>(`/scenarios/${id}`, { method: "DELETE" });
  }

  async startRun(engagementId: string, scenarioId: string): Promise<{ runId: string }> {
    const body = await this.fetch<{ runId: string }>(
      `/engagements/${engagementId}/runs`,
      { method: "POST", body: JSON.stringify({ scenarioId }) }
    );
    return { runId: body.runId };
  }

  async getRun(runId: string): Promise<ScenarioRun> {
    const body = await this.fetch<{ run: Record<string, unknown> }>(`/runs/${runId}`);
    const run = body.run;
    const results = Array.isArray(run["results"])
      ? (run["results"] as Record<string, unknown>[]).map((r) => ({
          techniqueId: String(r["techniqueId"] ?? ""),
          status: String(r["status"] ?? ""),
          output: String(r["output"] ?? ""),
          startedAt: r["startedAt"] ? String(r["startedAt"]) : null,
          finishedAt: r["finishedAt"] ? String(r["finishedAt"]) : null,
          err: String(r["err"] ?? ""),
        }))
      : [];
    return {
      id: String(run["id"] ?? runId),
      engagementId: String(run["engagementId"] ?? ""),
      scenarioId: String(run["scenarioId"] ?? ""),
      status: String(run["status"] ?? ""),
      results,
      startedAt: run["startedAt"] ? String(run["startedAt"]) : null,
      finishedAt: run["finishedAt"] ? String(run["finishedAt"]) : null,
    };
  }

  async recordDetection(runId: string, techniqueId: string, outcome: string): Promise<void> {
    await this.fetch<undefined>(`/runs/${runId}/detection`, {
      method: "POST",
      body: JSON.stringify({ techniqueId, outcome }),
    });
  }

  async listTechniques(): Promise<Technique[]> {
    const body = await this.fetch<{ techniques: Record<string, unknown>[] }>("/ttps");
    return (body.techniques ?? []).map(mapTechniqueFromApi);
  }

  async createTechnique(t: Technique): Promise<Technique> {
    const body = await this.fetch<{ technique: Record<string, unknown> }>("/ttps", {
      method: "POST",
      body: JSON.stringify({
        id: t.id,
        name: t.name,
        tactic: t.tactic,
        description: t.description ?? "",
        commands: t.commands ?? [],
      }),
    });
    return mapTechniqueFromApi(body.technique ?? {});
  }

  async updateTechnique(t: Technique): Promise<Technique> {
    const body = await this.fetch<{ technique: Record<string, unknown> }>(
      `/ttps/${encodeURIComponent(t.id)}`,
      {
        method: "PUT",
        body: JSON.stringify({
          name: t.name,
          tactic: t.tactic,
          description: t.description ?? "",
          commands: t.commands ?? [],
        }),
      }
    );
    return mapTechniqueFromApi(body.technique ?? {});
  }

  async deleteTechnique(id: string): Promise<void> {
    await this.fetch<undefined>(`/ttps/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  async getCoverage(engagementId: string): Promise<Coverage> {
    const c = await this.fetch<Coverage>(`/engagements/${engagementId}/coverage`);
    // The backend rollup doesn't emit the TRM yet; derive it from the counts.
    const trm = c.trm ?? trmFromCounts(c.exercisedCount ?? 0, c.validatedCount ?? 0);
    return { ...c, trm, trmTrend: c.trmTrend ?? [{ label: "now", trm }] };
  }

  async getNavigatorLayer(engagementId: string): Promise<unknown> {
    return this.fetch<unknown>(`/engagements/${engagementId}/navigator`);
  }

  async listAdvisories(): Promise<Advisory[]> {
    const body = await this.fetch<{ advisories: Record<string, unknown>[] }>("/advisories");
    return (body.advisories ?? []).map(mapAdvisoryFromApi);
  }

  async listAdvisorySources(): Promise<string[]> {
    const body = await this.fetch<{ sources: string[] }>("/advisories/sources");
    return body.sources ?? [];
  }
}

// ---------- Client factory ----------

let _client: RInfraClient | null = null;

export function getClient(): RInfraClient {
  if (_client) return _client;
  const apiBase = process.env.NEXT_PUBLIC_RINFRA_API;
  if (apiBase) {
    _client = new RestClient(apiBase);
  } else {
    _client = new MockClient();
  }
  return _client;
}

/** Whether the REST client is active (for store to switch behaviour). */
export function isRestMode(): boolean {
  return Boolean(process.env.NEXT_PUBLIC_RINFRA_API);
}

export const mockClient = new MockClient();
