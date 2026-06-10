"use client";
import React from "react";
import { usePathname } from "next/navigation";
import { Icons } from "../icons";
import { Dropdown, MenuItem, Avatar } from "../ui";
import { useStore } from "../../lib/store";

const TITLES: Record<string, string> = {
  "/engagements": "Engagements",
  "/infrastructure": "Infrastructure Builder",
  "/c2": "C2 Frameworks",
  "/emulation": "Emulation Runner",
  "/reporting": "Coverage & Reporting",
};

const CLOUDS = ["All providers", "AWS", "GCP", "Azure", "DigitalOcean"];

export default function TopBar({ onAppearance }: { onAppearance?: () => void }) {
  const pathname = usePathname();
  const { engagements, activeEngagement, setActiveEngagementId } = useStore();
  const [cloudEnv, setCloudEnv] = React.useState("All providers");

  const title = TITLES[pathname] || "RInfra";
  const activeList = engagements.filter(
    (e) => e.status === "active" || e.status === "provisioning" || e.status === "authorized"
  );

  return (
    <div
      style={{
        height: 56,
        flex: "none",
        background: "var(--surface)",
        borderBottom: "1px solid var(--border)",
        display: "flex",
        alignItems: "center",
        gap: 14,
        padding: "0 18px",
      }}
    >
      {/* engagement context switcher */}
      <Dropdown
        width={300}
        trigger={(open) => (
          <button className="btn" style={{ height: 38, paddingRight: 10 }}>
            <span
              style={{
                width: 24,
                height: 24,
                borderRadius: 6,
                background: "var(--accent-soft)",
                color: "var(--accent)",
                display: "grid",
                placeItems: "center",
                flex: "none",
              }}
            >
              <Icons.Target size={14} />
            </span>
            <span style={{ textAlign: "left", lineHeight: 1.15 }}>
              <span
                style={{
                  fontSize: 9.5,
                  color: "var(--text-3)",
                  display: "block",
                  letterSpacing: "0.03em",
                }}
              >
                ACTIVE ENGAGEMENT
              </span>
              <span style={{ fontSize: 13, fontWeight: 600 }}>
                {activeEngagement.codename} ·{" "}
                {activeEngagement.client.split(" ")[0]}
              </span>
            </span>
            <span
              style={{
                color: "var(--text-4)",
                transition: "transform .15s",
                transform: open ? "rotate(180deg)" : "none",
              }}
            >
              <Icons.ChevronDown size={15} />
            </span>
          </button>
        )}
      >
        <div
          style={{
            padding: "6px 10px 4px",
            fontSize: 10.5,
            fontWeight: 600,
            letterSpacing: "0.04em",
            color: "var(--text-4)",
            textTransform: "uppercase",
          }}
        >
          Switch context
        </div>
        {activeList.map((e) => (
          <MenuItem
            key={e.id}
            icon="Target"
            label={`${e.codename} · ${e.client}`}
            sub={`${e.id} · ${e.scope}`}
            active={e.id === activeEngagement.id}
            onClick={() => setActiveEngagementId(e.id)}
          />
        ))}
      </Dropdown>

      <div style={{ width: 1, height: 22, background: "var(--border)" }} />
      <div style={{ fontSize: 14, fontWeight: 600 }}>{title}</div>

      <div style={{ flex: 1 }} />

      {/* cloud env filter */}
      <Dropdown
        width={190}
        align="right"
        trigger={() => (
          <button className="btn sm" style={{ height: 34 }}>
            <Icons.Cloud size={15} /> {cloudEnv}
            <Icons.ChevronDown size={14} />
          </button>
        )}
      >
        {CLOUDS.map((c) => (
          <MenuItem
            key={c}
            icon="Cloud"
            label={c}
            active={c === cloudEnv}
            onClick={() => setCloudEnv(c)}
          />
        ))}
      </Dropdown>

      {/* activity */}
      <button
        className="btn sm"
        style={{ padding: 8, height: 34, width: 34, justifyContent: "center" }}
      >
        <Icons.Activity size={16} />
      </button>

      <div style={{ width: 1, height: 22, background: "var(--border)" }} />

      {/* appearance + user menu */}
      <Dropdown
        width={210}
        align="right"
        trigger={() => (
          <button
            style={{
              display: "flex",
              alignItems: "center",
              gap: 8,
              padding: 3,
              borderRadius: 99,
            }}
          >
            <Avatar name="Rina Okafor" size={32} />
          </button>
        )}
      >
        <div
          style={{
            padding: "8px 10px 10px",
            borderBottom: "1px solid var(--border)",
            marginBottom: 4,
          }}
        >
          <div style={{ fontSize: 13, fontWeight: 600 }}>Rina Okafor</div>
          <div style={{ fontSize: 11.5, color: "var(--text-3)" }}>
            Lead operator · Acme Offensive
          </div>
        </div>
        <MenuItem icon="Sliders" label="Appearance" onClick={onAppearance} />
        <MenuItem icon="User" label="Profile & API keys" />
        <MenuItem icon="Shield" label="Audit log" />
        <MenuItem icon="Settings" label="Workspace settings" />
      </Dropdown>
    </div>
  );
}
