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
} from "./types";
import { ENGAGEMENTS, INITIAL_NODES, INITIAL_EDGES } from "./data";

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
}

const StoreContext = createContext<StoreState | null>(null);

export function StoreProvider({ children }: { children: React.ReactNode }) {
  const [engagements, setEngagements] = useState<Engagement[]>(ENGAGEMENTS);
  const [activeEngagementId, setActiveEngagementId] = useState("ENG-2411");
  const [nodes, setNodes] = useState<CanvasNode[]>(INITIAL_NODES);
  const [edges, setEdges] = useState<CanvasEdge[]>(INITIAL_EDGES);
  const [preferences, setPreferences] = useState<Preferences>(DEFAULT_PREFS);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const toastCounter = useRef(0);

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

  const pushToast = useCallback((msg: string, kind: ToastKind = "info") => {
    const id = ++toastCounter.current;
    setToasts((ts) => [...ts, { id, msg, kind }]);
    setTimeout(
      () => setToasts((ts) => ts.filter((t) => t.id !== id)),
      3200
    );
  }, []);

  const activeEngagement =
    engagements.find((e) => e.id === activeEngagementId) ?? engagements[0];

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
