// RInfraClient interface — seam for REST and mock implementations.
// When NEXT_PUBLIC_RINFRA_API is set the RestClient is selected automatically;
// otherwise MockClient is used so the static demo build keeps working unchanged.
import type {
  Engagement,
  CanvasNode,
  CanvasEdge,
  C2Framework,
  Scenario,
  NodeStatus,
  EngagementStatus,
} from "./types";
import { ENGAGEMENTS, INITIAL_NODES, INITIAL_EDGES, C2_FRAMEWORKS, SCENARIOS } from "./data";

// ---------- Typed error codes coming from the API error envelope ----------

export type ApiErrorCode =
  | "authorization_required"
  | "auth_expired"
  | "outside_window"
  | "empty_scope"
  | "job_running"
  | "not_found"
  | "bad_request"
  | "internal_error";

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

  // Scenarios & runs
  listScenarios(): Promise<Scenario[]>;
  startRun(engagementId: string, scenarioId: string): Promise<{ runId: string }>;
  getRun(runId: string): Promise<ScenarioRun>;
}

export interface CreateEngagementParams {
  client: string;
  codename: string;
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

// ---------- MockClient ----------

export class MockClient implements RInfraClient {
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

  async listScenarios(): Promise<Scenario[]> {
    return SCENARIOS;
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
    desc: "",
    techniques,
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
    return {
      "Content-Type": "application/json",
      [OPERATOR_HEADER]: this.operator,
    };
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

    const body = await res.json() as Record<string, unknown>;

    if (!res.ok) {
      const err = body["error"] as Record<string, unknown> | undefined;
      const code = (err?.["code"] ?? "internal_error") as ApiErrorCode;
      const message = String(err?.["message"] ?? "An error occurred");
      throw new ApiError(code, message, res.status);
    }

    return body as T;
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

  // ---------- Scenarios & Runs ----------

  async listScenarios(): Promise<Scenario[]> {
    const body = await this.fetch<{ scenarios: Record<string, unknown>[] }>("/scenarios");
    return (body.scenarios ?? []).map(mapScenarioFromApi);
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
