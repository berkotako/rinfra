"use client";
import React, { useEffect, useRef } from "react";
import { Icons } from "../icons";
import { PROVIDERS, STATUS_META } from "../../lib/data";
import type { NodeType, NodeStatus } from "../../lib/types";

// --- ProviderBadge ---
export function ProviderBadge({
  id,
  size = "sm",
  showName = false,
}: {
  id: string;
  size?: "sm" | "md" | "lg";
  showName?: boolean;
}) {
  const p = PROVIDERS[id as keyof typeof PROVIDERS];
  if (!p) return null;
  const px = size === "lg" ? 26 : size === "md" ? 22 : 18;
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <span
        style={{
          width: px,
          height: px,
          borderRadius: 5,
          flex: "none",
          display: "grid",
          placeItems: "center",
          background: `color-mix(in oklch, ${p.color} 14%, var(--surface))`,
          color: p.color,
          fontSize: px * 0.42,
          fontWeight: 700,
          letterSpacing: "-0.02em",
          border: `1px solid color-mix(in oklch, ${p.color} 22%, transparent)`,
          fontFamily: "var(--font-sans)",
        }}
      >
        {p.short}
      </span>
      {showName && (
        <span style={{ fontSize: 12.5, color: "var(--text-2)" }}>{p.label}</span>
      )}
    </span>
  );
}

// --- StatusPill ---
export function StatusPill({
  status,
  sm,
}: {
  status: NodeStatus | string;
  sm?: boolean;
}) {
  const m = STATUS_META[status] || STATUS_META.pending;
  const cls = m.cls === "muted" ? "" : m.cls;
  return (
    <span
      className={"pill " + cls}
      style={sm ? { height: 20, fontSize: 11 } : undefined}
    >
      <span
        className={"status-dot " + status}
        style={{ width: 7, height: 7 }}
      />
      {m.label}
    </span>
  );
}

// --- NodeGlyph ---
export const NODE_TYPE_META: Record<
  string,
  { icon: string; label: string; hue: number }
> = {
  redirector: { icon: "Globe", label: "Redirector", hue: 240 },
  c2_server: { icon: "Server", label: "C2 Server", hue: 262 },
  payload_host: { icon: "HardDrive", label: "Staging", hue: 200 },
};

export function NodeGlyph({
  type,
  subtype,
  size = 18,
}: {
  type: NodeType | string;
  subtype?: string;
  size?: number;
}) {
  const meta = NODE_TYPE_META[type] || NODE_TYPE_META.c2_server;
  let iconName = meta.icon;
  if (type === "redirector" && subtype === "DNS") iconName = "Dns";
  const Ico = Icons[iconName] || Icons.Server;
  return (
    <span
      style={{
        width: size + 14,
        height: size + 14,
        borderRadius: 7,
        flex: "none",
        display: "grid",
        placeItems: "center",
        background: `oklch(0.95 0.03 ${meta.hue})`,
        color: `oklch(0.5 0.09 ${meta.hue})`,
        border: `1px solid oklch(0.88 0.04 ${meta.hue})`,
      }}
    >
      <Ico size={size} />
    </span>
  );
}

// --- Avatar ---
export function Avatar({ name, size = 28 }: { name: string; size?: number }) {
  const initials = (name || "?")
    .split(/[\s.]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((s) => s[0])
    .join("")
    .toUpperCase();
  const hue = (name || "")
    .split("")
    .reduce((a, c) => a + c.charCodeAt(0), 0) % 360;
  return (
    <span
      style={{
        width: size,
        height: size,
        borderRadius: 99,
        flex: "none",
        display: "grid",
        placeItems: "center",
        background: `oklch(0.92 0.035 ${hue})`,
        color: `oklch(0.42 0.06 ${hue})`,
        fontSize: size * 0.36,
        fontWeight: 600,
        letterSpacing: "-0.02em",
        border: `1px solid oklch(0.86 0.04 ${hue})`,
      }}
    >
      {initials}
    </span>
  );
}

// --- HealthMeter ---
export function HealthMeter({
  value,
  status,
}: {
  value: number;
  status: string;
}) {
  if (status === "provisioning") {
    return (
      <span className="mono" style={{ fontSize: 11, color: "var(--warn)" }}>
        — —
      </span>
    );
  }
  if (status === "destroyed" || status === "pending") {
    return (
      <span className="mono" style={{ fontSize: 11, color: "var(--text-4)" }}>
        offline
      </span>
    );
  }
  const col =
    value >= 90
      ? "var(--ok)"
      : value >= 70
      ? "var(--warn)"
      : "var(--danger)";
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <span
        style={{
          width: 40,
          height: 4,
          borderRadius: 99,
          background: "var(--surface-3)",
          overflow: "hidden",
          display: "inline-block",
        }}
      >
        <span
          style={{
            display: "block",
            width: `${value}%`,
            height: "100%",
            background: col,
            borderRadius: 99,
          }}
        />
      </span>
      <span className="mono" style={{ fontSize: 11, color: "var(--text-2)" }}>
        {value}%
      </span>
    </span>
  );
}

// --- Modal ---
export function Modal({
  open,
  onClose,
  children,
  width = 560,
  label,
}: {
  open: boolean;
  onClose: () => void;
  children: React.ReactNode;
  width?: number;
  label?: string;
}) {
  useEffect(() => {
    if (!open) return;
    const h = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, [open, onClose]);

  if (!open) return null;
  return (
    <div
      className="modal-scrim fade-in"
      onMouseDown={onClose}
      aria-label={label}
      style={{
        position: "fixed",
        inset: 0,
        zIndex: 1000,
        background: "color-mix(in oklch, var(--text) 28%, transparent)",
        backdropFilter: "blur(2px)",
        display: "grid",
        placeItems: "center",
        padding: 24,
      }}
    >
      <div
        className="card fade-up"
        onMouseDown={(e) => e.stopPropagation()}
        style={{
          width,
          maxWidth: "100%",
          maxHeight: "calc(100vh - 48px)",
          display: "flex",
          flexDirection: "column",
          boxShadow: "var(--shadow-pop)",
        }}
      >
        {children}
      </div>
    </div>
  );
}

// --- EmptyState ---
export function EmptyState({
  icon = "Layers",
  title,
  body,
  action,
}: {
  icon?: string;
  title: string;
  body: string;
  action?: React.ReactNode;
}) {
  const Ico = Icons[icon] || Icons.Layers;
  return (
    <div
      style={{
        display: "grid",
        placeItems: "center",
        padding: "56px 24px",
        textAlign: "center",
      }}
    >
      <div
        style={{
          width: 52,
          height: 52,
          borderRadius: 13,
          display: "grid",
          placeItems: "center",
          background: "var(--surface-3)",
          border: "1px solid var(--border)",
          color: "var(--text-3)",
          marginBottom: 16,
        }}
      >
        <Ico size={24} />
      </div>
      <div style={{ fontSize: 15, fontWeight: 600, marginBottom: 5 }}>
        {title}
      </div>
      <div
        style={{
          fontSize: 13,
          color: "var(--text-3)",
          maxWidth: 340,
          lineHeight: 1.5,
        }}
      >
        {body}
      </div>
      {action && <div style={{ marginTop: 18 }}>{action}</div>}
    </div>
  );
}

// --- PageHead ---
export function PageHead({
  title,
  sub,
  children,
}: {
  title: string;
  sub?: string;
  children?: React.ReactNode;
}) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "flex-end",
        justifyContent: "space-between",
        gap: 16,
        marginBottom: 20,
      }}
    >
      <div>
        <h1
          style={{
            fontSize: 21,
            fontWeight: 600,
            letterSpacing: "-0.02em",
          }}
        >
          {title}
        </h1>
        {sub && (
          <div style={{ fontSize: 13.5, color: "var(--text-3)", marginTop: 3 }}>
            {sub}
          </div>
        )}
      </div>
      {children && (
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          {children}
        </div>
      )}
    </div>
  );
}

// --- TierBadge ---
export function TierBadge({
  tier,
  label,
}: {
  tier: string;
  label: string;
}) {
  const map: Record<string, string> = {
    orchestrated: "ok",
    scripted: "info",
    fronted: "",
  };
  const iconMap: Record<string, string> = {
    orchestrated: "Bolt",
    scripted: "Terminal",
    fronted: "Power",
  };
  const Ico = Icons[iconMap[tier] || "Power"] || Icons.Power;
  return (
    <span className={"pill " + (map[tier] || "")}>
      <Ico size={12} /> {label}
    </span>
  );
}

// --- Dropdown ---
interface DropdownProps {
  trigger: (open: boolean) => React.ReactNode;
  children: React.ReactNode;
  width?: number;
  align?: "left" | "right";
}
export function Dropdown({
  trigger,
  children,
  width = 240,
  align = "left",
}: DropdownProps) {
  const [open, setOpen] = React.useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const h = (e: PointerEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node))
        setOpen(false);
    };
    window.addEventListener("pointerdown", h);
    return () => window.removeEventListener("pointerdown", h);
  }, [open]);

  return (
    <div ref={ref} style={{ position: "relative" }}>
      <div onClick={() => setOpen((o) => !o)}>{trigger(open)}</div>
      {open && (
        <div
          className="card fade-up"
          style={{
            position: "absolute",
            top: "calc(100% + 6px)",
            [align]: 0,
            width,
            zIndex: 200,
            boxShadow: "var(--shadow-pop)",
            padding: 5,
            overflow: "hidden",
          }}
          onClick={() => setOpen(false)}
        >
          {children}
        </div>
      )}
    </div>
  );
}

// --- MenuItem ---
export function MenuItem({
  icon,
  label,
  sub,
  active,
  onClick,
  right,
}: {
  icon?: string;
  label: string;
  sub?: string;
  active?: boolean;
  onClick?: () => void;
  right?: React.ReactNode;
}) {
  const Ico = icon ? Icons[icon] : null;
  return (
    <button
      onClick={onClick}
      className="menu-item"
      style={{
        width: "100%",
        display: "flex",
        alignItems: "center",
        gap: 10,
        padding: "8px 10px",
        borderRadius: "var(--r-sm)",
        textAlign: "left",
        color: "var(--text)",
      }}
    >
      {Ico && (
        <span style={{ color: active ? "var(--accent)" : "var(--text-3)" }}>
          <Ico size={16} />
        </span>
      )}
      <span style={{ flex: 1, minWidth: 0 }}>
        <span
          style={{
            fontSize: 13,
            fontWeight: active ? 600 : 500,
            display: "block",
          }}
        >
          {label}
        </span>
        {sub && (
          <span style={{ fontSize: 11, color: "var(--text-3)" }}>{sub}</span>
        )}
      </span>
      {active && (
        <span style={{ color: "var(--accent)" }}>
          <Icons.Check size={15} />
        </span>
      )}
      {right}
    </button>
  );
}

// --- Toasts ---
export function Toasts({ items }: { items: Array<{ id: number; msg: string; kind: string }> }) {
  const tone: Record<string, [string, string]> = {
    ok: ["var(--ok)", "CheckCircle"],
    warn: ["var(--warn)", "Activity"],
    info: ["var(--accent)", "Info"],
    danger: ["var(--danger)", "AlertTriangle"],
  };
  return (
    <div
      style={{
        position: "fixed",
        bottom: 18,
        left: "50%",
        transform: "translateX(-50%)",
        zIndex: 1500,
        display: "flex",
        flexDirection: "column",
        gap: 8,
        alignItems: "center",
        pointerEvents: "none",
      }}
    >
      {items.map((t) => {
        const [c, ic] = tone[t.kind] || tone.info;
        const Ico = Icons[ic] || Icons.Info;
        return (
          <div
            key={t.id}
            className="card fade-up"
            style={{
              display: "flex",
              alignItems: "center",
              gap: 10,
              padding: "10px 15px",
              boxShadow: "var(--shadow-pop)",
              minWidth: 240,
            }}
          >
            <span style={{ color: c }}>
              <Ico size={16} />
            </span>
            <span style={{ fontSize: 13, fontWeight: 500 }}>{t.msg}</span>
          </div>
        );
      })}
    </div>
  );
}
