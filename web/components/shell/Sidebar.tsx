"use client";
import React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Icons } from "../icons";
import { useAuth } from "../../lib/auth";
import type { Role } from "../../lib/types";

interface NavItem {
  id: string;
  label: string;
  icon: string;
  href: string;
  /** Roles allowed to see this item. Omitted = visible to everyone. */
  roles?: Role[];
}

const NAV: NavItem[] = [
  { id: "engagements", label: "Engagements", icon: "Target", href: "/engagements" },
  { id: "projects", label: "Projects", icon: "Building", href: "/projects" },
  { id: "infrastructure", label: "Infrastructure", icon: "Network", href: "/infrastructure" },
  { id: "c2", label: "C2 Frameworks", icon: "Server", href: "/c2" },
  { id: "emulation", label: "Emulation", icon: "Crosshair", href: "/emulation" },
  { id: "reporting", label: "Coverage & Reports", icon: "FileText", href: "/reporting" },
  { id: "users", label: "Users", icon: "User", href: "/users", roles: ["admin", "lead"] },
];

export default function Sidebar() {
  const pathname = usePathname();
  const { role } = useAuth();
  const nav = NAV.filter((n) => !n.roles || (role !== null && n.roles.includes(role)));

  return (
    <div
      style={{
        width: 226,
        flex: "none",
        background: "var(--surface)",
        borderRight: "1px solid var(--border)",
        display: "flex",
        flexDirection: "column",
      }}
    >
      {/* brand */}
      <Link
        href="/"
        style={{
          textDecoration: "none",
          color: "inherit",
          height: 56,
          display: "flex",
          alignItems: "center",
          gap: 10,
          padding: "0 16px",
          borderBottom: "1px solid var(--border)",
        }}
      >
        <div
          style={{
            width: 30,
            height: 30,
            borderRadius: 8,
            background: "var(--accent)",
            color: "var(--accent-contrast)",
            display: "grid",
            placeItems: "center",
            boxShadow: "var(--shadow-sm)",
          }}
        >
          <Icons.Logo size={18} />
        </div>
        <div style={{ lineHeight: 1.1 }}>
          <div
            style={{
              fontSize: 14.5,
              fontWeight: 600,
              letterSpacing: "-0.02em",
            }}
          >
            RInfra
          </div>
          <div style={{ fontSize: 10, color: "var(--text-3)" }}>
            Operations platform
          </div>
        </div>
      </Link>

      {/* nav */}
      <div
        style={{
          flex: 1,
          padding: "12px 12px",
          display: "flex",
          flexDirection: "column",
          gap: 2,
        }}
      >
        <div
          style={{
            fontSize: 10,
            fontWeight: 600,
            letterSpacing: "0.05em",
            color: "var(--text-4)",
            textTransform: "uppercase",
            padding: "6px 10px 7px",
          }}
        >
          Workspace
        </div>
        {nav.map((n) => {
          const active = pathname === n.href || (n.href !== "/" && pathname.startsWith(n.href));
          const Ico = Icons[n.icon] || Icons.Target;
          return (
            <Link key={n.id} href={n.href} style={{ textDecoration: "none" }}>
              <button className={"nav-item" + (active ? " active" : "")} style={{ width: "100%" }}>
                <Ico size={17} /> {n.label}
              </button>
            </Link>
          );
        })}
      </div>

      {/* footer */}
      <div
        style={{
          padding: 12,
          borderTop: "1px solid var(--border)",
          display: "flex",
          flexDirection: "column",
          gap: 2,
        }}
      >
        <Link href="/settings" style={{ textDecoration: "none" }}>
          <button
            className={
              "nav-item" +
              (pathname === "/settings" || pathname.startsWith("/settings/") ? " active" : "")
            }
            style={{ width: "100%" }}
          >
            <Icons.Settings size={17} /> Settings
          </button>
        </Link>
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 9,
            padding: "8px 8px",
            marginTop: 4,
            borderRadius: "var(--r-sm)",
            background: "var(--surface-2)",
            border: "1px solid var(--border)",
          }}
        >
          <div
            style={{
              width: 7,
              height: 7,
              borderRadius: 99,
              background: "var(--ok)",
              flex: "none",
            }}
          />
          <div
            style={{ fontSize: 11, color: "var(--text-3)", lineHeight: 1.3 }}
          >
            All systems operational
            <br />
            <span style={{ color: "var(--text-4)" }}>Audit logging active</span>
          </div>
        </div>
      </div>
    </div>
  );
}
