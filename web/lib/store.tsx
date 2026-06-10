"use client";
import React, {
  createContext,
  useContext,
  useState,
  useCallback,
  useEffect,
  useRef,
} from "react";
import type {
  Engagement,
  CanvasNode,
  CanvasEdge,
  Toast,
  ToastKind,
  Preferences,
  AccentId,
  NodeStyle,
  NodeStatus,
} from "./types";
import { ENGAGEMENTS, INITIAL_NODES, INITIAL_EDGES } from "./data";
import {
  getClient,
  isRestMode,
  ApiError,
  type SseEvent,
  type CreateEngagementParams,
} from "./client";

// Accent hue map — matches Appearance menu options
export const ACCENTS: { id: AccentId; name: string; h: number }[] = [
  { id: "indigo", name: "Indigo", h: 262 },
  { id: "slate", name: "Slate blue", h: 245 },
  { id: "peri", name: "Periwinkle", h: 278 },
  { id: "steel", name: "Steel", h: 222 },
];

const DEFAULT_PREFS: Preferences = {
  theme: "light",
  accentId: "indigo",
  nodeStyle: "soft",
};

interface StoreState {
  engagements: Engagement[];
  setEngagements: React.Dispatch<React.SetStateAction<Engagement[]>>;
  activeEngagementId: string;
  setActiveEngagementId: (id: string) => void;
  activeEngagement: Engagement;
  nodes: CanvasNode[];
  setNodes: React.Dispatch<React.SetStateAction<CanvasNode[]>>;
  edges: CanvasEdge[];
  setEdges: React.Dispatch<React.SetStateAction<CanvasEdge[]>>;
  preferences: Preferences;
  setTheme: (t: "light" | "dark") => void;
  setAccent: (id: AccentId) => void;
  setNodeStyle: (s: NodeStyle) => void;
  toasts: Toast[];
  pushToast: (msg: string, kind?: ToastKind) => void;

  // API-connected actions (no-ops / local simulation in mock mode)
  apiCreateEngagement: (params: CreateEngagementParams) => Promise<Engagement>;
  apiDeploy: (engagementId: string) => Promise<void>;
  apiTeardown: (engagementId: string) => Promise<void>;
  apiStartRun: (engagementId: string, scenarioId: string) => Promise<string>;
  apiSaveTopology: (engagementId: string, nodes: CanvasNode[], edges: CanvasEdge[]) => Promise<void>;
}

const StoreContext = createContext<StoreState | null>(null);

// Debounce helper
function debounce<T extends (...args: Parameters<T>) => void>(
  fn: T,
  ms: number
): (...args: Parameters<T>) => void {
  let timer: ReturnType<typeof setTimeout> | null = null;
  return (...args: Parameters<T>) => {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => fn(...args), ms);
  };
}

export function StoreProvider({ children }: { children: React.ReactNode }) {
  const rest = isRestMode();
  const client = getClient();

  const [engagements, setEngagements] = useState<Engagement[]>(rest ? [] : ENGAGEMENTS);
  const [activeEngagementId, setActiveEngagementId] = useState("ENG-2411");
  const [nodes, setNodes] = useState<CanvasNode[]>(rest ? [] : INITIAL_NODES);
  const [edges, setEdges] = useState<CanvasEdge[]>(rest ? [] : INITIAL_EDGES);
  const [preferences, setPreferences] = useState<Preferences>(DEFAULT_PREFS);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const toastCounter = useRef(0);

  // Keep a ref to activeEngagementId so effects can read it without stale closures.
  const activeEngagementIdRef = useRef(activeEngagementId);
  activeEngagementIdRef.current = activeEngagementId;

  // ---- Toast helper ----
  const pushToast = useCallback((msg: string, kind: ToastKind = "info") => {
    const id = ++toastCounter.current;
    setToasts((ts) => [...ts, { id, msg, kind }]);
    setTimeout(
      () => setToasts((ts) => ts.filter((t) => t.id !== id)),
      3200
    );
  }, []);

  // ---- Error helper: maps ApiError to toast ----
  const handleApiError = useCallback(
    (err: unknown, fallback = "An error occurred") => {
      if (err instanceof ApiError) {
        pushToast(err.toastMessage(), "danger");
      } else if (err instanceof Error) {
        pushToast(err.message || fallback, "danger");
      } else {
        pushToast(fallback, "danger");
      }
    },
    [pushToast]
  );

  // Load preferences from localStorage on mount (client only)
  useEffect(() => {
    try {
      const raw = localStorage.getItem("rinfra-prefs");
      if (raw) {
        const saved = JSON.parse(raw) as Partial<Preferences>;
        setPreferences((p) => ({ ...p, ...saved }));
      }
    } catch {
      // ignore
    }
  }, []);

  // Apply preferences to <html> data-theme and --accent-h
  useEffect(() => {
    const h = ACCENTS.find((a) => a.id === preferences.accentId)?.h ?? 262;
    document.documentElement.setAttribute(
      "data-theme",
      preferences.theme === "dark" ? "dark" : ""
    );
    document.documentElement.style.setProperty("--accent-h", String(h));
  }, [preferences.theme, preferences.accentId]);

  const saveAndSet = useCallback((patch: Partial<Preferences>) => {
    setPreferences((p) => {
      const next = { ...p, ...patch };
      try {
        localStorage.setItem("rinfra-prefs", JSON.stringify(next));
      } catch {
        // ignore
      }
      return next;
    });
  }, []);

  const setTheme = useCallback(
    (t: "light" | "dark") => saveAndSet({ theme: t }),
    [saveAndSet]
  );
  const setAccent = useCallback(
    (id: AccentId) => saveAndSet({ accentId: id }),
    [saveAndSet]
  );
  const setNodeStyle = useCallback(
    (s: NodeStyle) => saveAndSet({ nodeStyle: s }),
    [saveAndSet]
  );

  // ---- REST mode: initial data load ----
  useEffect(() => {
    if (!rest) return;

    client.listEngagements().then((engs) => {
      setEngagements(engs);
      // Default active to the first engagement if available.
      if (engs.length > 0) {
        setActiveEngagementId(engs[0].id);
      }
    }).catch((err: unknown) => handleApiError(err, "Failed to load engagements"));
  }, [rest, client, handleApiError]);

  // ---- REST mode: load topology when active engagement changes ----
  useEffect(() => {
    if (!rest || !activeEngagementId) return;

    client.getTopology(activeEngagementId).then(({ nodes: n, edges: e }) => {
      setNodes(n);
      setEdges(e);
    }).catch(() => {
      // Topology may not exist yet for new engagements — start empty.
      setNodes([]);
      setEdges([]);
    });
  }, [rest, client, activeEngagementId]);

  // ---- REST mode: SSE subscription for node/job/run events ----
  useEffect(() => {
    if (!rest || !activeEngagementId) return;

    const unsubscribe = client.subscribeEvents(activeEngagementId, (ev: SseEvent) => {
      if (ev.kind === "node_status") {
        const { nodeId, status, publicIp } = ev.data;
        setNodes((ns) =>
          ns.map((n) => {
            if (n.id !== nodeId) return n;
            const healthNum =
              status === "live" ? 95 + Math.floor(Math.random() * 5) :
              status === "provisioning" ? 0 :
              status === "draining" ? 0 :
              0;
            return {
              ...n,
              status: status as NodeStatus,
              ip: publicIp || (status === "live" ? n.ip : "—"),
              health: healthNum,
            };
          })
        );
      } else if (ev.kind === "job_status") {
        const { status } = ev.data;
        if (status === "done") {
          pushToast("Job completed successfully", "ok");
        } else if (status === "failed") {
          pushToast("Job failed — check audit log for details", "danger");
        }
      }
      // run_status events: consumed by EmulationScreen directly via its own hook.
    });

    return unsubscribe;
  }, [rest, client, activeEngagementId, pushToast]);

  // ---- Debounced topology save (REST mode only) ----
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const debouncedSaveTopology = useCallback(
    debounce(
      (engagementId: string, n: CanvasNode[], e: CanvasEdge[]) => {
        client.putTopology(engagementId, n, e).catch((err: unknown) =>
          handleApiError(err, "Failed to save topology")
        );
      },
      800
    ),
    [client, handleApiError]
  );

  // ---- Actions ----

  const apiSaveTopology = useCallback(
    async (engagementId: string, n: CanvasNode[], e: CanvasEdge[]) => {
      if (!rest) return;
      debouncedSaveTopology(engagementId, n, e);
    },
    [rest, debouncedSaveTopology]
  );

  const apiCreateEngagement = useCallback(
    async (params: CreateEngagementParams): Promise<Engagement> => {
      if (!rest) {
        // Mock path: build a local engagement object.
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
        setEngagements((list) => [e, ...list]);
        setActiveEngagementId(e.id);
        return e;
      }
      const created = await client.createEngagement(params);
      setEngagements((list) => [created, ...list]);
      setActiveEngagementId(created.id);
      return created;
    },
    [rest, client]
  );

  const apiDeploy = useCallback(
    async (engagementId: string): Promise<void> => {
      if (!rest) {
        // Mock mode: local setTimeout simulation (existing behaviour).
        return;
      }
      try {
        const { jobId } = await client.deploy(engagementId);
        pushToast(`Deploy job started (${jobId}) — watching for updates…`, "info");
      } catch (err: unknown) {
        handleApiError(err, "Deploy failed");
        throw err;
      }
    },
    [rest, client, pushToast, handleApiError]
  );

  const apiTeardown = useCallback(
    async (engagementId: string): Promise<void> => {
      if (!rest) {
        return;
      }
      try {
        const { jobId } = await client.teardown(engagementId);
        pushToast(`Teardown job started (${jobId}) — watching for updates…`, "info");
      } catch (err: unknown) {
        handleApiError(err, "Teardown failed");
        throw err;
      }
    },
    [rest, client, pushToast, handleApiError]
  );

  const apiStartRun = useCallback(
    async (engagementId: string, scenarioId: string): Promise<string> => {
      if (!rest) {
        return "mock-run-" + Date.now();
      }
      const { runId } = await client.startRun(engagementId, scenarioId);
      return runId;
    },
    [rest, client]
  );

  const activeEngagement =
    engagements.find((e) => e.id === activeEngagementId) ?? engagements[0] ?? ENGAGEMENTS[0];

  return (
    <StoreContext.Provider
      value={{
        engagements,
        setEngagements,
        activeEngagementId,
        setActiveEngagementId,
        activeEngagement,
        nodes,
        setNodes,
        edges,
        setEdges,
        preferences,
        setTheme,
        setAccent,
        setNodeStyle,
        toasts,
        pushToast,
        apiCreateEngagement,
        apiDeploy,
        apiTeardown,
        apiStartRun,
        apiSaveTopology,
      }}
    >
      {children}
    </StoreContext.Provider>
  );
}

export function useStore(): StoreState {
  const ctx = useContext(StoreContext);
  if (!ctx) throw new Error("useStore must be used within StoreProvider");
  return ctx;
}
