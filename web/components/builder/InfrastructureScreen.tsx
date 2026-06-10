"use client";
import React, { useState, useRef, useCallback, useEffect } from "react";
import { Icons } from "../icons";
import { NodeGlyph, EmptyState, ProviderBadge, StatusPill, HealthMeter, Modal } from "../ui";
import { NODE_TYPE_META } from "../ui";
import { useStore } from "../../lib/store";
import { NODE_TEMPLATES, REGIONS, C2_FRAMEWORKS, PROVIDERS } from "../../lib/data";
import { C2Selector } from "../c2/C2Screen";
import type { CanvasNode, NodeType, NodeStyle } from "../../lib/types";

// Node card dimensions per style
const NODE_DIMS: Record<NodeStyle, { w: number; h: number }> = {
  soft: { w: 216, h: 110 },
  compact: { w: 200, h: 60 },
  outline: { w: 212, h: 96 },
};

function portPos(node: CanvasNode, side: "in" | "out", style: NodeStyle) {
  const d = NODE_DIMS[style] || NODE_DIMS.soft;
  return { x: node.x + (side === "out" ? d.w : 0), y: node.y + d.h / 2 };
}

function edgePath(a: { x: number; y: number }, b: { x: number; y: number }) {
  const dx = Math.max(40, Math.abs(b.x - a.x) * 0.5);
  return `M ${a.x} ${a.y} C ${a.x + dx} ${a.y}, ${b.x - dx} ${b.y}, ${b.x} ${b.y}`;
}

interface NodeCardProps {
  node: CanvasNode;
  style: NodeStyle;
  selected: boolean;
  onSelect: (id: string) => void;
  onPointerDownDrag: (e: React.PointerEvent, node: CanvasNode) => void;
  onPortDown: (e: React.PointerEvent, node: CanvasNode) => void;
  onPortEnter: (node: CanvasNode) => void;
  onPortLeave: () => void;
  connecting: boolean;
}

function NodeCard({
  node,
  style,
  selected,
  onSelect,
  onPointerDownDrag,
  onPortDown,
  onPortEnter,
  onPortLeave,
  connecting,
}: NodeCardProps) {
  const dims = NODE_DIMS[style] || NODE_DIMS.soft;
  const meta = NODE_TYPE_META[node.type] || NODE_TYPE_META.c2_server;
  const destroyed = node.status === "destroyed";
  const provisioning = node.status === "provisioning";

  const ring = selected ? "var(--accent)" : "var(--border-2)";
  const base: React.CSSProperties = {
    position: "absolute",
    left: node.x,
    top: node.y,
    width: dims.w,
    height: dims.h,
    background: "var(--surface)",
    borderRadius: "var(--r-lg)",
    border: `1px solid ${ring}`,
    boxShadow: selected
      ? "0 0 0 3px var(--accent-soft), var(--shadow-md)"
      : "var(--shadow-sm)",
    cursor: "grab",
    userSelect: "none",
    transition: "box-shadow .15s, border-color .15s, opacity .3s",
    opacity: destroyed ? 0.55 : 1,
  };

  const Port = ({ side }: { side: "in" | "out" }) => (
    <div
      onPointerDown={(e) => {
        e.stopPropagation();
        if (side === "out") onPortDown(e, node);
      }}
      onPointerEnter={() => side === "in" && onPortEnter(node)}
      onPointerLeave={() => side === "in" && onPortLeave()}
      title={side === "out" ? "Drag to connect" : "Inbound"}
      style={{
        position: "absolute",
        top: dims.h / 2 - 7,
        [side === "out" ? "right" : "left"]: -7,
        width: 14,
        height: 14,
        borderRadius: 99,
        zIndex: 3,
        background: "var(--surface)",
        border: `2px solid ${connecting && side === "in" ? "var(--accent)" : "var(--border-strong)"}`,
        cursor: side === "out" ? "crosshair" : "default",
        display: "grid",
        placeItems: "center",
        transition: "border-color .12s, transform .12s",
        transform:
          connecting && side === "in" ? "scale(1.25)" : "none",
      }}
    >
      <div
        style={{
          width: 4,
          height: 4,
          borderRadius: 99,
          background:
            side === "out" ? "var(--accent)" : "var(--text-4)",
        }}
      />
    </div>
  );

  if (style === "compact") {
    return (
      <div
        style={base}
        onPointerDown={(e) => onPointerDownDrag(e, node)}
        onClick={() => onSelect(node.id)}
      >
        <Port side="in" />
        <Port side="out" />
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            padding: "0 12px",
            height: "100%",
          }}
        >
          <NodeGlyph type={node.type as NodeType} subtype={node.subtype} size={16} />
          <div style={{ minWidth: 0, flex: 1 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
              <span
                style={{
                  fontWeight: 600,
                  fontSize: 13,
                  whiteSpace: "nowrap",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                }}
              >
                {node.name}
              </span>
            </div>
            <div
              className="mono"
              style={{
                fontSize: 10.5,
                color: "var(--text-3)",
                whiteSpace: "nowrap",
                overflow: "hidden",
                textOverflow: "ellipsis",
              }}
            >
              {node.subtype} · {node.region}
            </div>
          </div>
          <ProviderBadge id={node.provider} />
          <span className={"status-dot " + node.status} />
        </div>
      </div>
    );
  }

  if (style === "outline") {
    return (
      <div
        style={{
          ...base,
          background: "var(--surface-2)",
          boxShadow: selected ? "0 0 0 3px var(--accent-soft)" : "none",
        }}
        onPointerDown={(e) => onPointerDownDrag(e, node)}
        onClick={() => onSelect(node.id)}
      >
        <Port side="in" />
        <Port side="out" />
        <div
          style={{
            padding: "10px 12px",
            height: "100%",
            display: "flex",
            flexDirection: "column",
            justifyContent: "space-between",
          }}
        >
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
            }}
          >
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 8,
                minWidth: 0,
              }}
            >
              <span
                style={{
                  color: `oklch(0.5 0.09 ${meta.hue})`,
                  display: "grid",
                  placeItems: "center",
                }}
              >
                {React.createElement(
                  Icons[
                    node.type === "redirector" && node.subtype === "DNS"
                      ? "Dns"
                      : meta.icon
                  ] || Icons.Server,
                  { size: 15 }
                )}
              </span>
              <span
                style={{
                  fontWeight: 600,
                  fontSize: 13,
                  whiteSpace: "nowrap",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                }}
              >
                {node.name}
              </span>
            </div>
            <ProviderBadge id={node.provider} />
          </div>
          <div className="mono" style={{ fontSize: 11, color: "var(--text-2)" }}>
            {node.ip}
          </div>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
            }}
          >
            <StatusPill status={node.status} sm />
            <HealthMeter value={node.health} status={node.status} />
          </div>
        </div>
      </div>
    );
  }

  // SOFT (default)
  return (
    <div
      style={base}
      onPointerDown={(e) => onPointerDownDrag(e, node)}
      onClick={() => onSelect(node.id)}
    >
      <Port side="in" />
      <Port side="out" />
      <div
        style={{
          padding: "11px 12px 0",
          display: "flex",
          alignItems: "flex-start",
          gap: 10,
        }}
      >
        <NodeGlyph type={node.type as NodeType} subtype={node.subtype} size={18} />
        <div style={{ minWidth: 0, flex: 1 }}>
          <div
            style={{
              fontWeight: 600,
              fontSize: 13.5,
              whiteSpace: "nowrap",
              overflow: "hidden",
              textOverflow: "ellipsis",
              letterSpacing: "-0.01em",
            }}
          >
            {node.name}
          </div>
          <div style={{ fontSize: 11.5, color: "var(--text-3)", marginTop: 1 }}>
            {meta.label}
            {node.subtype ? ` · ${node.subtype}` : ""}
          </div>
        </div>
        <ProviderBadge id={node.provider} />
      </div>
      <div
        style={{
          padding: "8px 12px 0",
          display: "flex",
          alignItems: "center",
          gap: 8,
        }}
      >
        <span
          className="mono"
          style={{
            fontSize: 11,
            color: "var(--text-2)",
            background: "var(--surface-inset)",
            border: "1px solid var(--border)",
            borderRadius: 5,
            padding: "2px 6px",
            whiteSpace: "nowrap",
            overflow: "hidden",
            textOverflow: "ellipsis",
            maxWidth: "100%",
          }}
        >
          {provisioning ? "allocating…" : node.ip}
        </span>
      </div>
      <div
        style={{
          position: "absolute",
          left: 12,
          right: 12,
          bottom: 9,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <StatusPill status={node.status} sm />
        <HealthMeter value={node.health} status={node.status} />
      </div>
    </div>
  );
}

function ValidationPopover({
  checks,
  onClose,
}: {
  checks: { ok: boolean; label: string; warn?: boolean }[];
  onClose: () => void;
}) {
  useEffect(() => {
    const h = (e: PointerEvent) => {
      if (!(e.target as Element).closest(".val-pop")) onClose();
    };
    const t = setTimeout(() => window.addEventListener("pointerdown", h), 0);
    return () => {
      clearTimeout(t);
      window.removeEventListener("pointerdown", h);
    };
  }, [onClose]);

  const warns = checks.filter((c) => c.warn).length;
  const fails = checks.filter((c) => !c.ok && !c.warn).length;

  return (
    <div
      className="val-pop card fade-up"
      style={{
        position: "absolute",
        top: 40,
        right: 0,
        width: 320,
        zIndex: 50,
        boxShadow: "var(--shadow-pop)",
        padding: 0,
      }}
    >
      <div
        style={{
          padding: "12px 14px",
          borderBottom: "1px solid var(--border)",
          display: "flex",
          alignItems: "center",
          gap: 8,
        }}
      >
        <span
          style={{
            color: fails
              ? "var(--danger)"
              : warns
              ? "var(--warn)"
              : "var(--ok)",
          }}
        >
          {React.createElement(
            fails ? Icons.AlertTriangle : Icons.ShieldCheck,
            { size: 16 }
          )}
        </span>
        <span style={{ fontSize: 13, fontWeight: 600 }}>
          {fails
            ? "Validation failed"
            : warns
            ? "Passed with warnings"
            : "Configuration valid"}
        </span>
      </div>
      <div
        style={{
          padding: "8px 14px 12px",
          display: "flex",
          flexDirection: "column",
          gap: 9,
        }}
      >
        {checks.map((c, i) => {
          const Ico = c.ok ? Icons.CheckCircle : c.warn ? Icons.AlertTriangle : Icons.X;
          return (
            <div
              key={i}
              style={{
                display: "flex",
                alignItems: "flex-start",
                gap: 9,
                fontSize: 12.5,
              }}
            >
              <span
                style={{
                  marginTop: 1,
                  color: c.ok
                    ? "var(--ok)"
                    : c.warn
                    ? "var(--warn)"
                    : "var(--danger)",
                }}
              >
                <Ico size={15} />
              </span>
              <span style={{ color: "var(--text-2)" }}>{c.label}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function Inspector({
  node,
  onChange,
  onClose,
  onDelete,
}: {
  node: CanvasNode | null;
  onChange: (n: CanvasNode) => void;
  onClose: () => void;
  onDelete: (id: string) => void;
}) {
  if (!node) {
    return (
      <div
        style={{
          padding: "32px 18px",
          textAlign: "center",
          color: "var(--text-3)",
        }}
      >
        <div
          style={{
            width: 44,
            height: 44,
            borderRadius: 11,
            display: "grid",
            placeItems: "center",
            background: "var(--surface-3)",
            border: "1px solid var(--border)",
            margin: "0 auto 14px",
            color: "var(--text-4)",
          }}
        >
          <Icons.Sliders size={20} />
        </div>
        <div
          style={{
            fontSize: 13.5,
            fontWeight: 600,
            color: "var(--text-2)",
            marginBottom: 4,
          }}
        >
          No node selected
        </div>
        <div style={{ fontSize: 12.5, lineHeight: 1.5 }}>
          Select a node on the canvas to configure its provider, region and
          listener.
        </div>
      </div>
    );
  }

  const meta = NODE_TYPE_META[node.type] || NODE_TYPE_META.c2_server;
  const regions = REGIONS[node.provider as keyof typeof REGIONS] || [];
  const set = <K extends keyof CanvasNode>(k: K, v: CanvasNode[K]) =>
    onChange({ ...node, [k]: v });

  return (
    <div
      className="scroll"
      style={{ height: "100%", display: "flex", flexDirection: "column" }}
    >
      <div
        style={{
          padding: "14px 16px",
          borderBottom: "1px solid var(--border)",
          display: "flex",
          alignItems: "center",
          gap: 11,
        }}
      >
        <NodeGlyph type={node.type as NodeType} subtype={node.subtype} size={18} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontWeight: 600, fontSize: 14 }}>{node.name}</div>
          <div style={{ fontSize: 12, color: "var(--text-3)" }}>
            {meta.label}
          </div>
        </div>
        <button
          className="btn ghost sm"
          onClick={onClose}
          style={{ padding: 6 }}
        >
          <Icons.X size={15} />
        </button>
      </div>

      <div
        className="scroll"
        style={{
          flex: 1,
          padding: 16,
          display: "flex",
          flexDirection: "column",
          gap: 16,
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
          }}
        >
          <StatusPill status={node.status} />
          <HealthMeter value={node.health} status={node.status} />
        </div>

        <div className="field">
          <label>Name</label>
          <input
            className="input"
            value={node.name}
            onChange={(e) => set("name", e.target.value)}
          />
        </div>

        <div className="field">
          <label>Cloud provider</label>
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "1fr 1fr",
              gap: 6,
            }}
          >
            {Object.values(PROVIDERS).map((p) => (
              <button
                key={p.id}
                onClick={() => set("provider", p.id)}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  padding: "8px 10px",
                  borderRadius: "var(--r-sm)",
                  border: `1px solid ${node.provider === p.id ? "var(--accent)" : "var(--border-2)"}`,
                  background:
                    node.provider === p.id
                      ? "var(--accent-soft)"
                      : "var(--surface-inset)",
                  boxShadow:
                    node.provider === p.id
                      ? "0 0 0 2px var(--accent-soft)"
                      : "none",
                }}
              >
                <ProviderBadge id={p.id} />
                <span
                  style={{
                    fontSize: 12,
                    fontWeight: 500,
                    color: "var(--text-2)",
                  }}
                >
                  {p.name}
                </span>
              </button>
            ))}
          </div>
        </div>

        <div className="field">
          <label>Region</label>
          <select
            className="select"
            value={node.region}
            onChange={(e) => set("region", e.target.value)}
          >
            {regions.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>
        </div>

        {node.type === "c2_server" && (
          <>
            <div className="field">
              <label>C2 framework</label>
              <select
                className="select"
                value={node.framework || ""}
                onChange={(e) => set("framework", e.target.value)}
              >
                {C2_FRAMEWORKS.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
              <div className="hint">
                Pick from the framework library for orchestration tier &
                licensing.
              </div>
            </div>
            <div className="field">
              <label>Listener protocol</label>
              <div className="seg" style={{ flexWrap: "wrap" }}>
                {(
                  C2_FRAMEWORKS.find((c) => c.id === node.framework)
                    ?.listeners || ["HTTPS"]
                ).map((l) => (
                  <button
                    key={l}
                    className={node.listener === l ? "active" : ""}
                    onClick={() => set("listener", l)}
                  >
                    {l}
                  </button>
                ))}
              </div>
            </div>
          </>
        )}

        {node.type === "redirector" && (
          <div className="field">
            <label>Front domain</label>
            <input
              className="input mono"
              value={node.domain || ""}
              onChange={(e) => set("domain", e.target.value)}
              placeholder="cdn-assets.example"
              style={{ fontSize: 12.5 }}
            />
            <div className="hint">
              Categorized domain used to mask C2 traffic.
            </div>
          </div>
        )}

        {node.type === "payload_host" && (
          <div className="field">
            <label>Delivery domain</label>
            <input
              className="input mono"
              value={node.domain || ""}
              onChange={(e) => set("domain", e.target.value)}
              style={{ fontSize: 12.5 }}
            />
          </div>
        )}

        <div className="field">
          <label>Identifiers</label>
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: 6,
              fontSize: 12,
            }}
          >
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                gap: 12,
              }}
            >
              <span style={{ color: "var(--text-3)" }}>Resource ID</span>
              <span className="mono" style={{ color: "var(--text-2)" }}>
                i-0{node.id}f7a2{node.provider}9c
              </span>
            </div>
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                gap: 12,
              }}
            >
              <span style={{ color: "var(--text-3)" }}>Public IP</span>
              <span className="mono" style={{ color: "var(--text-2)" }}>
                {node.ip}
              </span>
            </div>
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                gap: 12,
              }}
            >
              <span style={{ color: "var(--text-3)" }}>Est. cost</span>
              <span className="mono" style={{ color: "var(--text-2)" }}>
                ${node.cost.toFixed(2)}/hr
              </span>
            </div>
          </div>
        </div>
      </div>

      <div
        style={{
          padding: 14,
          borderTop: "1px solid var(--border)",
          display: "flex",
          gap: 8,
        }}
      >
        <button
          className="btn danger"
          onClick={() => onDelete(node.id)}
          style={{ flex: 1, justifyContent: "center" }}
        >
          <Icons.Trash size={15} /> Remove node
        </button>
      </div>
    </div>
  );
}

export default function InfrastructureScreen() {
  const {
    nodes, setNodes, edges, setEdges, activeEngagement, pushToast, preferences,
    apiDeploy, apiTeardown, apiSaveTopology, activeEngagementId,
  } = useStore();
  const nodeStyle = preferences.nodeStyle;

  const [selected, setSelected] = useState<string | null>(null);
  const [pan, setPan] = useState({ x: 40, y: 20 });
  const [validation, setValidation] = useState<
    { ok: boolean; label: string; warn?: boolean }[] | null
  >(null);
  const [confirmTear, setConfirmTear] = useState(false);
  const [c2Modal, setC2Modal] = useState(false);
  const [ghost, setGhost] = useState<{
    x: number;
    y: number;
    template: (typeof NODE_TEMPLATES)[0];
  } | null>(null);
  const [edgePreview, setEdgePreview] = useState<{
    from: { x: number; y: number };
    to: { x: number; y: number };
  } | null>(null);
  const [hoverPort, setHoverPort] = useState<string | null>(null);

  const canvasRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{
    kind: string;
    id?: string;
    dx?: number;
    dy?: number;
    sx?: number;
    sy?: number;
    panX?: number;
    panY?: number;
    template?: (typeof NODE_TEMPLATES)[0];
    fromId?: string;
    fromNode?: CanvasNode;
    moved?: boolean;
  } | null>(null);

  const panRef = useRef(pan);
  panRef.current = pan;
  const nodesRef = useRef(nodes);
  nodesRef.current = nodes;
  const hoverRef = useRef(hoverPort);
  hoverRef.current = hoverPort;

  const toCanvas = useCallback((clientX: number, clientY: number) => {
    const r = canvasRef.current!.getBoundingClientRect();
    return {
      x: clientX - r.left - panRef.current.x,
      y: clientY - r.top - panRef.current.y,
    };
  }, []);

  // addNodeRef lets the effect access addNode without adding it as a dep
  const addNodeRef = useRef<((tmpl: (typeof NODE_TEMPLATES)[0], x: number, y: number) => void) | null>(null);

  useEffect(() => {
    const move = (e: PointerEvent) => {
      const d = drag.current;
      if (!d) return;
      if (d.kind === "node") {
        const p = toCanvas(e.clientX, e.clientY);
        setNodes((ns) =>
          ns.map((n) =>
            n.id === d.id
              ? {
                  ...n,
                  x: Math.round(p.x - (d.dx || 0)),
                  y: Math.round(p.y - (d.dy || 0)),
                }
              : n
          )
        );
        d.moved = true;
      } else if (d.kind === "pan") {
        setPan({
          x: (d.panX || 0) + (e.clientX - (d.sx || 0)),
          y: (d.panY || 0) + (e.clientY - (d.sy || 0)),
        });
      } else if (d.kind === "palette") {
        setGhost({ x: e.clientX, y: e.clientY, template: d.template! });
      } else if (d.kind === "edge") {
        const p = toCanvas(e.clientX, e.clientY);
        const fromPos = portPos(d.fromNode!, "out", nodeStyle);
        setEdgePreview({ from: fromPos, to: p });
      }
    };

    const up = (e: PointerEvent) => {
      const d = drag.current;
      if (d) {
        if (d.kind === "palette") {
          const r = canvasRef.current!.getBoundingClientRect();
          if (
            e.clientX >= r.left &&
            e.clientX <= r.right &&
            e.clientY >= r.top &&
            e.clientY <= r.bottom
          ) {
            const p = toCanvas(e.clientX, e.clientY);
            const dims = NODE_DIMS[nodeStyle] || NODE_DIMS.soft;
            addNodeRef.current?.(d.template!, p.x - dims.w / 2, p.y - dims.h / 2);
          }
          setGhost(null);
        } else if (d.kind === "edge") {
          const target = hoverRef.current;
          if (target && target !== d.fromId) {
            setEdges((es) => {
              if (es.some((x) => x.from === d.fromId && x.to === target))
                return es;
              return [
                ...es,
                { id: "e" + Date.now(), from: d.fromId!, to: target },
              ];
            });
            pushToast("Edge created — traffic flow added", "ok");
          }
          setEdgePreview(null);
          setHoverPort(null);
        } else if (d.kind === "node" && !d.moved) {
          setSelected(d.id || null);
        }
      }
      drag.current = null;
    };

    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
  }, [nodeStyle, toCanvas, setNodes, setEdges, pushToast]);

  const addNode = useCallback((
    tmpl: (typeof NODE_TEMPLATES)[0],
    x: number,
    y: number
  ) => {
    const n = nodesRef.current.length + 1;
    const id = "n" + Date.now();
    const node: CanvasNode = {
      id,
      type: tmpl.type,
      subtype: tmpl.subtype,
      name:
        (tmpl.type === "c2_server"
          ? "teamserver"
          : tmpl.type === "payload_host"
          ? "stage-host"
          : "edge-" + (tmpl.subtype || "").toLowerCase()) +
        "-" +
        String(n).padStart(2, "0"),
      provider: "aws",
      region: "us-east-1",
      status: "pending",
      health: 0,
      ip: "—",
      framework: tmpl.type === "c2_server" ? "sliver" : undefined,
      listener: tmpl.type === "c2_server" ? "mTLS" : undefined,
      domain: tmpl.type !== "c2_server" ? "example-cdn.net" : undefined,
      x: Math.round(x),
      y: Math.round(y),
      cost: tmpl.type === "c2_server" ? 1.85 : 0.4,
    };
    setNodes((ns) => [...ns, node]);
    setSelected(id);
    pushToast(`${tmpl.label} added — configure it in the inspector`, "info");
  }, [setNodes, pushToast]);

  // Keep the ref in sync so the pointer-event effect can call addNode
  addNodeRef.current = addNode;

  const updateNode = (nn: CanvasNode) =>
    setNodes((ns) => ns.map((n) => (n.id === nn.id ? nn : n)));
  const deleteNode = (id: string) => {
    setNodes((ns) => ns.filter((n) => n.id !== id));
    setEdges((es) => es.filter((e) => e.from !== id && e.to !== id));
    if (selected === id) setSelected(null);
    pushToast("Node removed", "info");
  };

  const onNodeDrag = (e: React.PointerEvent, node: CanvasNode) => {
    e.preventDefault();
    const p = toCanvas(e.clientX, e.clientY);
    drag.current = {
      kind: "node",
      id: node.id,
      dx: p.x - node.x,
      dy: p.y - node.y,
      moved: false,
    };
  };

  const onBgDown = (e: React.PointerEvent) => {
    const target = e.target as HTMLElement;
    if (target === canvasRef.current || target.dataset.bg) {
      drag.current = {
        kind: "pan",
        sx: e.clientX,
        sy: e.clientY,
        panX: pan.x,
        panY: pan.y,
      };
      setSelected(null);
    }
  };

  const onPortDown = (e: React.PointerEvent, node: CanvasNode) => {
    drag.current = { kind: "edge", fromId: node.id, fromNode: node };
    const fromPos = portPos(node, "out", nodeStyle);
    setEdgePreview({ from: fromPos, to: fromPos });
  };

  const onPaletteDown = (
    e: React.PointerEvent,
    tmpl: (typeof NODE_TEMPLATES)[0]
  ) => {
    e.preventDefault();
    drag.current = { kind: "palette", template: tmpl };
    setGhost({ x: e.clientX, y: e.clientY, template: tmpl });
  };

  // Toolbar actions
  const liveCount = nodes.filter(
    (n) => n.status === "live" || n.status === "provisioning"
  ).length;
  const hourly = nodes
    .filter((n) => n.status !== "destroyed" && n.status !== "pending")
    .reduce((a, n) => a + n.cost, 0);

  const doValidate = () => {
    const checks: { ok: boolean; label: string; warn?: boolean }[] = [];
    checks.push({
      ok: nodes.length > 0,
      label: `${nodes.length} assets in topology`,
      warn: nodes.length === 0,
    });
    const hasC2 = nodes.some((n) => n.type === "c2_server");
    const hasRedir = nodes.some((n) => n.type === "redirector");
    checks.push({ ok: hasC2, label: "At least one C2 server present" });
    checks.push({
      ok: hasRedir,
      label: "Redirector fronts C2 traffic",
      warn: !hasRedir,
    });
    const orphan = nodes.filter(
      (n) =>
        n.type === "c2_server" && !edges.some((e) => e.to === n.id)
    );
    checks.push({
      ok: orphan.length === 0,
      label:
        orphan.length === 0
          ? "All C2 servers have an inbound redirector"
          : `${orphan.length} C2 server(s) directly exposed`,
      warn: orphan.length > 0,
    });
    checks.push({
      ok: activeEngagement.auth === "authorized",
      label: "Engagement authorization on file",
    });
    setValidation(checks);
  };

  const doDeploy = () => {
    // Authorization gate: mirror the server-side invariant on the client.
    // No infrastructure may be provisioned for an engagement that is not
    // authorized. (The control plane also enforces this and returns 403, but
    // in mock/demo mode there is no server, and either way the UI must not
    // present deploy as available for an unauthorized engagement.)
    if (activeEngagement.auth !== "authorized") {
      pushToast(
        "Deploy blocked — engagement authorization is not on file",
        "danger"
      );
      return;
    }

    const targets = nodes.filter(
      (n) => n.status === "pending" || n.status === "provisioning"
    );
    if (!targets.length) {
      pushToast("Nothing to deploy — all assets already live", "info");
      return;
    }

    // REST mode: save topology first, then call deploy; SSE drives state transitions.
    apiSaveTopology(activeEngagementId, nodes, edges);

    apiDeploy(activeEngagementId).then(() => {
      // REST mode: set pending→provisioning optimistically; SSE will transition to live.
      setNodes((ns) =>
        ns.map((n) => (n.status === "pending" ? { ...n, status: "provisioning" } : n))
      );
      pushToast(`Provisioning ${targets.length} asset(s)…`, "warn");
    }).catch(() => {
      // Error toast already emitted by apiDeploy.
    });

    // Mock mode simulation (no-op in REST mode since apiDeploy returns quickly).
    if (!process.env.NEXT_PUBLIC_RINFRA_API) {
      setNodes((ns) =>
        ns.map((n) => (n.status === "pending" ? { ...n, status: "provisioning" } : n))
      );
      pushToast(`Provisioning ${targets.length} asset(s)…`, "warn");
      targets.forEach((t, i) => {
        setTimeout(() => {
          setNodes((ns) =>
            ns.map((n) =>
              n.id === t.id
                ? {
                    ...n,
                    status: "live",
                    health: 92 + Math.floor(Math.random() * 7),
                    ip:
                      n.type === "c2_server"
                        ? `10.0.${Math.floor(Math.random() * 9) + 1}.${Math.floor(Math.random() * 200) + 10}`
                        : `203.0.113.${Math.floor(Math.random() * 200) + 10}`,
                  }
                : n
            )
          );
        }, 900 + i * 700);
      });
      setTimeout(
        () => pushToast("Deployment complete — infrastructure live", "ok"),
        900 + targets.length * 700
      );
    }
  };

  const doTearDown = () => {
    setConfirmTear(false);
    const live = nodes.filter(
      (n) => n.status === "live" || n.status === "provisioning"
    );
    if (!live.length) {
      pushToast("No live assets to tear down", "info");
      return;
    }

    // REST mode: call teardown API; SSE drives state transitions.
    apiTeardown(activeEngagementId).then(() => {
      setNodes((ns) =>
        ns.map((n) =>
          n.status === "live" || n.status === "provisioning"
            ? { ...n, status: "draining" }
            : n
        )
      );
      pushToast("Draining connections…", "info");
    }).catch(() => {
      // Error toast already emitted by apiTeardown.
    });

    // Mock mode simulation (no-op in REST mode since apiTeardown returns quickly).
    if (!process.env.NEXT_PUBLIC_RINFRA_API) {
      setNodes((ns) =>
        ns.map((n) =>
          n.status === "live" || n.status === "provisioning"
            ? { ...n, status: "draining" }
            : n
        )
      );
      pushToast("Draining connections…", "info");
      live.forEach((t, i) => {
        setTimeout(
          () =>
            setNodes((ns) =>
              ns.map((n) =>
                n.id === t.id ? { ...n, status: "destroyed", health: 0, ip: "—" } : n
              )
            ),
          800 + i * 500
        );
      });
      setTimeout(
        () => pushToast("Infrastructure torn down — assets destroyed", "ok"),
        800 + live.length * 500
      );
    }
  };

  const selNode = nodes.find((n) => n.id === selected) || null;

  return (
    <div style={{ display: "flex", height: "100%", minHeight: 0 }}>
      {/* PALETTE */}
      <div
        style={{
          width: 224,
          flex: "none",
          borderRight: "1px solid var(--border)",
          background: "var(--surface)",
          display: "flex",
          flexDirection: "column",
        }}
      >
        <div style={{ padding: "14px 16px 10px" }}>
          <div className="eyebrow">Node library</div>
          <div style={{ fontSize: 12, color: "var(--text-3)", marginTop: 4 }}>
            Drag onto the canvas to add.
          </div>
        </div>
        <div
          className="scroll"
          style={{
            flex: 1,
            padding: "4px 12px 12px",
            display: "flex",
            flexDirection: "column",
            gap: 7,
          }}
        >
          {(
            [
              ["Redirectors", NODE_TEMPLATES.filter((t) => t.type === "redirector")],
              ["Command & control", NODE_TEMPLATES.filter((t) => t.type === "c2_server")],
              ["Staging", NODE_TEMPLATES.filter((t) => t.type === "payload_host")],
            ] as [string, typeof NODE_TEMPLATES][]
          ).map(([grp, items]) => (
            <div key={grp} style={{ marginTop: 6 }}>
              <div
                style={{
                  fontSize: 10.5,
                  fontWeight: 600,
                  letterSpacing: "0.04em",
                  textTransform: "uppercase",
                  color: "var(--text-4)",
                  padding: "2px 4px 7px",
                }}
              >
                {grp}
              </div>
              <div
                style={{
                  display: "flex",
                  flexDirection: "column",
                  gap: 7,
                }}
              >
                {items.map((t, i) => (
                  <div
                    key={i}
                    onPointerDown={(e) => onPaletteDown(e, t)}
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 10,
                      padding: "9px 10px",
                      borderRadius: "var(--r-sm)",
                      border: "1px solid var(--border)",
                      background: "var(--surface-2)",
                      cursor: "grab",
                      transition: "border-color .12s, background .12s, box-shadow .12s",
                    }}
                    onMouseEnter={(e) => {
                      (e.currentTarget as HTMLElement).style.borderColor =
                        "var(--border-strong)";
                      (e.currentTarget as HTMLElement).style.boxShadow =
                        "var(--shadow-xs)";
                    }}
                    onMouseLeave={(e) => {
                      (e.currentTarget as HTMLElement).style.borderColor =
                        "var(--border)";
                      (e.currentTarget as HTMLElement).style.boxShadow = "none";
                    }}
                  >
                    <NodeGlyph type={t.type} subtype={t.subtype} size={15} />
                    <div style={{ minWidth: 0 }}>
                      <div style={{ fontSize: 12.5, fontWeight: 500 }}>
                        {t.label}
                      </div>
                      <div style={{ fontSize: 11, color: "var(--text-3)" }}>
                        {t.desc}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ))}
          <button
            className="btn sm"
            onClick={() => setC2Modal(true)}
            style={{ marginTop: 12, justifyContent: "center" }}
          >
            <Icons.Layers size={14} /> Browse C2 frameworks
          </button>
        </div>
      </div>

      {/* CANVAS + TOOLBAR */}
      <div
        style={{
          flex: 1,
          minWidth: 0,
          display: "flex",
          flexDirection: "column",
        }}
      >
        {/* toolbar */}
        <div
          style={{
            height: 52,
            flex: "none",
            borderBottom: "1px solid var(--border)",
            background: "var(--surface)",
            display: "flex",
            alignItems: "center",
            gap: 10,
            padding: "0 16px",
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <span style={{ fontSize: 13, fontWeight: 600 }}>Topology</span>
            <span className="pill" style={{ height: 20 }}>
              {activeEngagement.codename}
            </span>
          </div>
          <div style={{ flex: 1 }} />
          {/* counters */}
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 14,
              marginRight: 6,
              padding: "0 14px",
              borderLeft: "1px solid var(--border)",
              borderRight: "1px solid var(--border)",
              height: 32,
            }}
          >
            <div style={{ textAlign: "right" }}>
              <div
                style={{
                  fontSize: 10,
                  color: "var(--text-3)",
                  letterSpacing: "0.03em",
                }}
              >
                ASSETS
              </div>
              <div
                className="mono"
                style={{ fontSize: 13, fontWeight: 600 }}
              >
                {liveCount}
                <span style={{ color: "var(--text-4)" }}>/{nodes.length}</span>
              </div>
            </div>
            <div style={{ textAlign: "right" }}>
              <div
                style={{
                  fontSize: 10,
                  color: "var(--text-3)",
                  letterSpacing: "0.03em",
                }}
              >
                BURN RATE
              </div>
              <div
                className="mono"
                style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}
              >
                ${hourly.toFixed(2)}
                <span style={{ color: "var(--text-4)", fontWeight: 400 }}>
                  /hr
                </span>
              </div>
            </div>
          </div>
          <div style={{ position: "relative" }}>
            <button className="btn sm" onClick={doValidate}>
              <Icons.ShieldCheck size={15} /> Validate
            </button>
            {validation && (
              <ValidationPopover
                checks={validation}
                onClose={() => setValidation(null)}
              />
            )}
          </div>
          <button
            className="btn sm danger"
            onClick={() => setConfirmTear(true)}
          >
            <Icons.Power size={15} /> Tear down
          </button>
          <button className="btn primary sm" onClick={doDeploy}>
            <Icons.Bolt size={15} /> Deploy
          </button>
        </div>

        {/* canvas */}
        <div
          ref={canvasRef}
          data-bg="1"
          onPointerDown={onBgDown}
          style={{
            position: "relative",
            flex: 1,
            minHeight: 0,
            overflow: "hidden",
            cursor: "default",
            background: "var(--canvas-bg)",
            backgroundImage:
              "radial-gradient(var(--grid-dot) 1.3px, transparent 1.3px)",
            backgroundSize: "22px 22px",
            backgroundPosition: `${pan.x}px ${pan.y}px`,
          }}
        >
          {/* edges SVG */}
          <svg
            style={{
              position: "absolute",
              inset: 0,
              width: "100%",
              height: "100%",
              pointerEvents: "none",
              transform: `translate(${pan.x}px,${pan.y}px)`,
              overflow: "visible",
            }}
          >
            <defs>
              <marker
                id="arw"
                markerWidth="9"
                markerHeight="9"
                refX="7"
                refY="4.5"
                orient="auto"
              >
                <path
                  d="M1 1 L7 4.5 L1 8"
                  fill="none"
                  stroke="var(--text-4)"
                  strokeWidth="1.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </marker>
            </defs>
            {edges.map((e) => {
              const a = nodes.find((n) => n.id === e.from);
              const b = nodes.find((n) => n.id === e.to);
              if (!a || !b) return null;
              const live = a.status === "live" && b.status === "live";
              const aPos = portPos(a, "out", nodeStyle);
              const bPos = portPos(b, "in", nodeStyle);
              const d = edgePath(aPos, bPos);
              return (
                <g key={e.id}>
                  <path
                    d={d}
                    fill="none"
                    stroke={live ? "var(--accent)" : "var(--border-strong)"}
                    strokeWidth={live ? 2 : 1.6}
                    markerEnd="url(#arw)"
                    opacity={live ? 0.85 : 0.6}
                    strokeLinecap="round"
                  />
                  {live && (
                    <path
                      d={d}
                      fill="none"
                      stroke="var(--accent)"
                      strokeWidth="2"
                      strokeDasharray="2 8"
                      strokeLinecap="round"
                      opacity="0.9"
                    >
                      <animate
                        attributeName="stroke-dashoffset"
                        from="20"
                        to="0"
                        dur="1.1s"
                        repeatCount="indefinite"
                      />
                    </path>
                  )}
                </g>
              );
            })}
            {edgePreview && (
              <path
                d={edgePath(edgePreview.from, edgePreview.to)}
                fill="none"
                stroke="var(--accent)"
                strokeWidth="2"
                strokeDasharray="5 5"
                opacity="0.7"
                strokeLinecap="round"
              />
            )}
          </svg>

          {/* nodes */}
          <div
            style={{
              position: "absolute",
              inset: 0,
              transform: `translate(${pan.x}px,${pan.y}px)`,
            }}
          >
            {nodes.map((n) => (
              <NodeCard
                key={n.id}
                node={n}
                style={nodeStyle}
                selected={selected === n.id}
                onSelect={setSelected}
                onPointerDownDrag={onNodeDrag}
                onPortDown={onPortDown}
                onPortEnter={(nd) => setHoverPort(nd.id)}
                onPortLeave={() => setHoverPort(null)}
                connecting={!!edgePreview}
              />
            ))}
          </div>

          {nodes.length === 0 && (
            <div
              style={{
                position: "absolute",
                inset: 0,
                display: "grid",
                placeItems: "center",
                pointerEvents: "none",
              }}
            >
              <EmptyState
                icon="Network"
                title="Empty topology"
                body="Drag redirectors, C2 servers and staging hosts from the library to compose your attack infrastructure."
              />
            </div>
          )}

          {/* hint chip */}
          <div
            style={{
              position: "absolute",
              left: 14,
              bottom: 14,
              display: "flex",
              gap: 8,
              alignItems: "center",
              pointerEvents: "none",
            }}
          >
            <span
              className="pill"
              style={{
                background: "var(--surface)",
                boxShadow: "var(--shadow-sm)",
              }}
            >
              <Icons.Info size={12} /> Drag a node&apos;s right port to connect
              traffic flow
            </span>
          </div>
        </div>
      </div>

      {/* INSPECTOR */}
      <div
        style={{
          width: selNode ? 300 : 264,
          flex: "none",
          borderLeft: "1px solid var(--border)",
          background: "var(--surface)",
          transition: "width .2s",
        }}
      >
        <Inspector
          node={selNode}
          onChange={updateNode}
          onClose={() => setSelected(null)}
          onDelete={deleteNode}
        />
      </div>

      {/* palette drag ghost */}
      {ghost && (
        <div
          style={{
            position: "fixed",
            left: ghost.x - 90,
            top: ghost.y - 22,
            width: 180,
            pointerEvents: "none",
            zIndex: 2000,
            opacity: 0.92,
          }}
        >
          <div
            className="card"
            style={{
              display: "flex",
              alignItems: "center",
              gap: 10,
              padding: "10px 12px",
              boxShadow: "var(--shadow-pop)",
            }}
          >
            <NodeGlyph
              type={ghost.template.type}
              subtype={ghost.template.subtype}
              size={15}
            />
            <div style={{ fontSize: 12.5, fontWeight: 500 }}>
              {ghost.template.label}
            </div>
          </div>
        </div>
      )}

      {/* tear down confirm */}
      <Modal
        open={confirmTear}
        onClose={() => setConfirmTear(false)}
        width={440}
      >
        <div
          style={{
            padding: "22px 22px 0",
            display: "flex",
            gap: 14,
          }}
        >
          <div
            style={{
              width: 40,
              height: 40,
              borderRadius: 10,
              flex: "none",
              display: "grid",
              placeItems: "center",
              background: "var(--danger-soft)",
              color: "var(--danger)",
              border: "1px solid var(--danger-soft-border)",
            }}
          >
            <Icons.AlertTriangle size={20} />
          </div>
          <div>
            <div style={{ fontSize: 16, fontWeight: 600 }}>
              Tear down infrastructure?
            </div>
            <div
              style={{
                fontSize: 13,
                color: "var(--text-2)",
                marginTop: 5,
                lineHeight: 1.5,
              }}
            >
              This will drain and destroy all live assets for{" "}
              <b>{activeEngagement.codename}</b>. Active C2 sessions will be
              lost. This action is logged to the engagement audit trail and
              cannot be undone.
            </div>
          </div>
        </div>
        <div
          style={{
            padding: 18,
            display: "flex",
            justifyContent: "flex-end",
            gap: 9,
            marginTop: 8,
          }}
        >
          <button className="btn" onClick={() => setConfirmTear(false)}>
            Cancel
          </button>
          <button className="btn danger" onClick={doTearDown}>
            <Icons.Power size={15} /> Tear down all assets
          </button>
        </div>
      </Modal>

      {/* C2 modal */}
      {c2Modal && (
        <C2Selector
          asModal
          selectedId={selNode?.type === "c2_server" ? selNode.framework : undefined}
          onClose={() => setC2Modal(false)}
          onSelect={(c) => {
            if (selNode && selNode.type === "c2_server") {
              updateNode({
                ...selNode,
                framework: c.id,
                listener: c.listeners[0] ?? selNode.listener,
              });
              pushToast(`${c.name} assigned to ${selNode.name}`, "ok");
            } else {
              pushToast(
                "Select a C2 server node first to assign a framework",
                "warn"
              );
            }
          }}
        />
      )}
    </div>
  );
}
