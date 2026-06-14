"use client";
import React, { useEffect, useMemo, useState } from "react";
import { Icons } from "../icons";
import { PageHead, ProviderBadge } from "../ui";
import { useStore, ACCENTS } from "../../lib/store";
import { useAuth } from "../../lib/auth";
import { getClient, isRestMode, ApiError } from "../../lib/client";
import { PROVIDERS } from "../../lib/data";
import type { CloudProvider, NodeStyle } from "../../lib/types";

// ---------------------------------------------------------------------------
// Per-provider credential field specs. Keys mirror the env-var names the Go
// cloud adapters read from cloud.Credentials.Raw (see internal/cloud/*).
// ---------------------------------------------------------------------------
interface FieldSpec {
  key: string;
  label: string;
  secret?: boolean;
  textarea?: boolean;
  placeholder?: string;
  hint?: string;
}

const CLOUD_FIELDS: Record<CloudProvider, FieldSpec[]> = {
  aws: [
    { key: "AWS_ACCESS_KEY_ID", label: "Access key ID", placeholder: "AKIA…" },
    { key: "AWS_SECRET_ACCESS_KEY", label: "Secret access key", secret: true },
    { key: "AWS_REGION", label: "Default region", placeholder: "us-east-1" },
  ],
  gcp: [
    { key: "GOOGLE_PROJECT", label: "Project ID", placeholder: "my-project-123" },
    {
      key: "GOOGLE_CREDENTIALS",
      label: "Service account JSON",
      textarea: true,
      secret: true,
      placeholder: '{ "type": "service_account", … }',
      hint: "Paste the full service-account key file contents.",
    },
  ],
  azure: [
    { key: "ARM_SUBSCRIPTION_ID", label: "Subscription ID" },
    { key: "ARM_TENANT_ID", label: "Tenant ID" },
    { key: "ARM_CLIENT_ID", label: "Client ID" },
    { key: "ARM_CLIENT_SECRET", label: "Client secret", secret: true },
  ],
  digitalocean: [
    { key: "DIGITALOCEAN_TOKEN", label: "API token", secret: true, placeholder: "dop_v1_…" },
  ],
};

const CLOUD_ORDER: CloudProvider[] = ["aws", "gcp", "azure", "digitalocean"];

type SectionId = "credentials" | "account" | "appearance" | "connection";

const SECTIONS: { id: SectionId; label: string; icon: string }[] = [
  { id: "credentials", label: "Cloud credentials", icon: "Lock" },
  { id: "account", label: "Account", icon: "User" },
  { id: "appearance", label: "Appearance", icon: "Sliders" },
  { id: "connection", label: "Connection", icon: "Cloud" },
];

// Local marker that a provider has creds configured (the secret itself is never
// persisted in the browser — only that it was saved, for the status badge).
const CONFIGURED_KEY = "rinfra-cloud-configured";

function loadConfigured(): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(CONFIGURED_KEY);
    return raw ? (JSON.parse(raw) as Record<string, boolean>) : {};
  } catch {
    return {};
  }
}

export default function SettingsScreen() {
  const { pushToast, activeEngagement, preferences, setTheme, setAccent, setNodeStyle } =
    useStore();
  const { username, updateAccount, logout } = useAuth();

  const [section, setSection] = useState<SectionId>("credentials");

  return (
    <div style={{ height: "100%", overflowY: "auto" }}>
      <div style={{ maxWidth: 960, margin: "0 auto", padding: "26px 24px 60px" }}>
        <PageHead
          title="Settings"
          sub="Cloud provider keys, account, and console configuration."
        />

        <div style={{ display: "flex", gap: 22, alignItems: "flex-start" }} className="settings-grid">
          {/* in-page nav */}
          <div style={{ width: 200, flex: "none", position: "sticky", top: 0 }} className="settings-nav">
            {SECTIONS.map((s) => {
              const Ico = Icons[s.icon] || Icons.Settings;
              const active = section === s.id;
              return (
                <button
                  key={s.id}
                  onClick={() => setSection(s.id)}
                  className={"nav-item" + (active ? " active" : "")}
                  style={{ width: "100%", marginBottom: 2 }}
                >
                  <Ico size={16} /> {s.label}
                </button>
              );
            })}
          </div>

          {/* content */}
          <div style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column", gap: 16 }}>
            {section === "credentials" && (
              <CloudCredentials
                engagementId={activeEngagement?.id ?? ""}
                engagementName={activeEngagement?.codename ?? ""}
                onToast={pushToast}
              />
            )}
            {section === "account" && (
              <AccountSettings username={username} updateAccount={updateAccount} logout={logout} onToast={pushToast} />
            )}
            {section === "appearance" && (
              <AppearanceSettings
                preferences={preferences}
                setTheme={setTheme}
                setAccent={setAccent}
                setNodeStyle={setNodeStyle}
              />
            )}
            {section === "connection" && (
              <ConnectionInfo engagementId={activeEngagement?.id ?? ""} />
            )}
          </div>
        </div>
      </div>

      <style>{`
        @media (max-width: 760px) {
          .settings-grid { flex-direction: column !important; }
          .settings-nav { width: 100% !important; display: flex; flex-wrap: wrap; gap: 4px; }
          .settings-nav .nav-item { width: auto !important; }
        }
      `}</style>
    </div>
  );
}

// --- Card wrapper ---
function Card({
  title,
  desc,
  children,
  footer,
}: {
  title: string;
  desc?: string;
  children: React.ReactNode;
  footer?: React.ReactNode;
}) {
  return (
    <div className="card" style={{ overflow: "hidden" }}>
      <div style={{ padding: "16px 18px", borderBottom: "1px solid var(--border)" }}>
        <div style={{ fontSize: 14.5, fontWeight: 600 }}>{title}</div>
        {desc && (
          <div style={{ fontSize: 12.5, color: "var(--text-3)", marginTop: 3, lineHeight: 1.5 }}>
            {desc}
          </div>
        )}
      </div>
      <div style={{ padding: "18px" }}>{children}</div>
      {footer && (
        <div
          style={{
            padding: "12px 18px",
            borderTop: "1px solid var(--border)",
            background: "var(--surface-2)",
            display: "flex",
            justifyContent: "flex-end",
            gap: 8,
          }}
        >
          {footer}
        </div>
      )}
    </div>
  );
}

// --- Cloud credentials ---
function CloudCredentials({
  engagementId,
  engagementName,
  onToast,
}: {
  engagementId: string;
  engagementName: string;
  onToast: (m: string, k?: "ok" | "warn" | "info" | "danger") => void;
}) {
  const rest = isRestMode();
  const [provider, setProvider] = useState<CloudProvider>("aws");
  const [values, setValues] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [configured, setConfigured] = useState<Record<string, boolean>>({});

  useEffect(() => {
    setConfigured(loadConfigured());
  }, []);

  // Reset the form when switching providers.
  useEffect(() => {
    setValues({});
  }, [provider]);

  const fields = CLOUD_FIELDS[provider];
  const complete = useMemo(
    () => fields.every((f) => (values[f.key] ?? "").trim() !== ""),
    [fields, values]
  );

  function markConfigured(p: string) {
    setConfigured((prev) => {
      const next = { ...prev, [p]: true };
      try {
        localStorage.setItem(CONFIGURED_KEY, JSON.stringify(next));
      } catch {
        // ignore
      }
      return next;
    });
  }

  async function onSave() {
    if (!complete) {
      onToast("Fill in every field before saving.", "warn");
      return;
    }
    setSaving(true);
    try {
      if (rest) {
        if (!engagementId) {
          onToast("Select an active engagement first.", "warn");
          return;
        }
        await getClient().putCredentials(engagementId, provider, values);
      }
      markConfigured(provider);
      setValues({});
      onToast(
        `${PROVIDERS[provider].name} credentials saved${rest ? "" : " (demo — stored locally)"}.`,
        "ok"
      );
    } catch (err) {
      if (err instanceof ApiError) onToast(err.toastMessage(), "danger");
      else onToast("Failed to save credentials.", "danger");
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      {/* BYO-cloud invariant note */}
      <div
        style={{
          display: "flex",
          gap: 10,
          padding: "12px 14px",
          borderRadius: "var(--r-md)",
          background: "var(--surface-2)",
          border: "1px solid var(--border)",
          fontSize: 12.5,
          color: "var(--text-2)",
          lineHeight: 1.5,
        }}
      >
        <span style={{ color: "var(--accent)", flex: "none" }}>
          <Icons.Shield size={16} />
        </span>
        <span>
          <strong style={{ color: "var(--text)" }}>Bring your own cloud.</strong> Keys are
          stored encrypted (AES-256-GCM) and bound to a single engagement — RInfra never
          provisions on its own tenancy.{" "}
          {rest ? (
            <>
              These keys apply to{" "}
              <strong style={{ color: "var(--text)" }}>
                {engagementName || "the active engagement"}
              </strong>
              .
            </>
          ) : (
            <>In this demo build no backend is contacted; values stay in your browser.</>
          )}
        </span>
      </div>

      <Card
        title="Cloud provider keys"
        desc="Add the credentials RInfra uses to provision attack infrastructure into the customer's account."
        footer={
          <>
            <button
              className="btn primary"
              onClick={onSave}
              disabled={saving || !complete}
            >
              {saving ? "Saving…" : "Save credentials"}
            </button>
          </>
        }
      >
        {/* provider tabs */}
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginBottom: 18 }}>
          {CLOUD_ORDER.map((p) => {
            const active = provider === p;
            return (
              <button
                key={p}
                onClick={() => setProvider(p)}
                className="btn"
                style={{
                  height: 40,
                  borderColor: active ? "var(--accent)" : undefined,
                  background: active ? "var(--accent-soft)" : undefined,
                  color: active ? "var(--accent)" : undefined,
                }}
              >
                <ProviderBadge id={p} showName />
                {configured[p] && (
                  <span style={{ color: "var(--ok)", display: "inline-flex" }}>
                    <Icons.CheckCircle size={14} />
                  </span>
                )}
              </button>
            );
          })}
        </div>

        {/* fields */}
        <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
          {fields.map((f) => (
            <div className="field" key={f.key}>
              <label htmlFor={`cred-${f.key}`}>{f.label}</label>
              {f.textarea ? (
                <textarea
                  id={`cred-${f.key}`}
                  className="input"
                  style={{ minHeight: 110, fontFamily: "var(--font-mono)" }}
                  placeholder={f.placeholder}
                  value={values[f.key] ?? ""}
                  onChange={(e) => setValues((v) => ({ ...v, [f.key]: e.target.value }))}
                />
              ) : (
                <input
                  id={`cred-${f.key}`}
                  className="input"
                  type={f.secret ? "password" : "text"}
                  autoComplete="off"
                  placeholder={f.placeholder}
                  value={values[f.key] ?? ""}
                  onChange={(e) => setValues((v) => ({ ...v, [f.key]: e.target.value }))}
                />
              )}
              <span className="hint mono" style={{ fontSize: 11 }}>
                {f.key}
                {f.hint ? ` — ${f.hint}` : ""}
              </span>
            </div>
          ))}
        </div>
      </Card>
    </>
  );
}

// --- Account ---
function AccountSettings({
  username,
  updateAccount,
  logout,
  onToast,
}: {
  username: string;
  updateAccount: (a: { currentPassword: string; newUsername?: string; newPassword?: string }) => string | null;
  logout: () => void;
  onToast: (m: string, k?: "ok" | "warn" | "info" | "danger") => void;
}) {
  const [newUsername, setNewUsername] = useState(username);
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirm, setConfirm] = useState("");

  useEffect(() => setNewUsername(username), [username]);

  function onSave() {
    if (!currentPassword) {
      onToast("Enter your current password.", "warn");
      return;
    }
    if (newPassword && newPassword !== confirm) {
      onToast("New password and confirmation do not match.", "warn");
      return;
    }
    const err = updateAccount({
      currentPassword,
      newUsername: newUsername !== username ? newUsername : undefined,
      newPassword: newPassword || undefined,
    });
    if (err) {
      onToast(err, "danger");
      return;
    }
    setCurrentPassword("");
    setNewPassword("");
    setConfirm("");
    onToast("Account updated.", "ok");
  }

  return (
    <Card
      title="Admin account"
      desc="The console sign-in. The default on a fresh install is admin / admin — change it here."
      footer={
        <>
          <button className="btn" onClick={logout}>
            <Icons.Power size={15} /> Sign out
          </button>
          <button className="btn primary" onClick={onSave}>
            Save changes
          </button>
        </>
      }
    >
      <div style={{ display: "flex", flexDirection: "column", gap: 14, maxWidth: 420 }}>
        <div className="field">
          <label htmlFor="acct-user">Username</label>
          <input
            id="acct-user"
            className="input"
            value={newUsername}
            onChange={(e) => setNewUsername(e.target.value)}
            autoComplete="username"
          />
        </div>
        <div className="field">
          <label htmlFor="acct-cur">Current password</label>
          <input
            id="acct-cur"
            className="input"
            type="password"
            value={currentPassword}
            onChange={(e) => setCurrentPassword(e.target.value)}
            autoComplete="current-password"
            placeholder="Required to save changes"
          />
        </div>
        <div className="field">
          <label htmlFor="acct-new">New password</label>
          <input
            id="acct-new"
            className="input"
            type="password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            autoComplete="new-password"
            placeholder="Leave blank to keep current"
          />
        </div>
        <div className="field">
          <label htmlFor="acct-confirm">Confirm new password</label>
          <input
            id="acct-confirm"
            className="input"
            type="password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            autoComplete="new-password"
          />
        </div>
      </div>
    </Card>
  );
}

// --- Appearance ---
function AppearanceSettings({
  preferences,
  setTheme,
  setAccent,
  setNodeStyle,
}: {
  preferences: { theme: "light" | "dark"; accentId: string; nodeStyle: NodeStyle };
  setTheme: (t: "light" | "dark") => void;
  setAccent: (id: "indigo" | "slate" | "peri" | "steel") => void;
  setNodeStyle: (s: NodeStyle) => void;
}) {
  return (
    <Card title="Appearance" desc="Console theme and canvas styling. Saved to this browser.">
      <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
        <div className="field">
          <label>Theme</label>
          <div
            onClick={() => setTheme(preferences.theme === "dark" ? "light" : "dark")}
            style={{
              display: "flex",
              alignItems: "center",
              gap: 13,
              padding: "11px 13px",
              borderRadius: "var(--r-md)",
              border: "1px solid var(--border)",
              background: "var(--surface-2)",
              cursor: "pointer",
              maxWidth: 420,
            }}
          >
            <div style={{ flex: 1 }}>
              <div style={{ fontSize: 13, fontWeight: 600 }}>Soft dark mode</div>
              <div style={{ fontSize: 12, color: "var(--text-3)", marginTop: 2 }}>
                Warm charcoal — never pure black
              </div>
            </div>
            <div className={"toggle " + (preferences.theme === "dark" ? "on" : "")} />
          </div>
        </div>

        <div className="field" style={{ maxWidth: 420 }}>
          <label>Primary accent</label>
          <div style={{ display: "flex", gap: 7 }}>
            {ACCENTS.map((a) => (
              <button
                key={a.id}
                title={a.name}
                onClick={() => setAccent(a.id)}
                style={{
                  flex: 1,
                  height: 30,
                  borderRadius: 7,
                  cursor: "pointer",
                  background: `oklch(0.58 0.09 ${a.h})`,
                  border:
                    preferences.accentId === a.id
                      ? "2px solid rgba(0,0,0,.55)"
                      : "2px solid transparent",
                  boxShadow: preferences.accentId === a.id ? "0 0 0 2px #fff inset" : "none",
                }}
              />
            ))}
          </div>
        </div>

        <div className="field" style={{ maxWidth: 420 }}>
          <label>Node card style</label>
          <div className="seg" style={{ width: "100%" }}>
            {(["soft", "compact", "outline"] as NodeStyle[]).map((s) => (
              <button
                key={s}
                className={preferences.nodeStyle === s ? "active" : ""}
                onClick={() => setNodeStyle(s)}
                style={{ flex: 1, textTransform: "capitalize" }}
              >
                {s}
              </button>
            ))}
          </div>
        </div>
      </div>
    </Card>
  );
}

// --- Connection ---
function ConnectionInfo({ engagementId }: { engagementId: string }) {
  const rest = isRestMode();
  const base = process.env.NEXT_PUBLIC_RINFRA_API || "";

  function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
    return (
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          gap: 16,
          padding: "10px 0",
          borderBottom: "1px solid var(--border)",
          fontSize: 13,
        }}
      >
        <span style={{ color: "var(--text-3)" }}>{label}</span>
        <span className={mono ? "mono" : ""} style={{ color: "var(--text)", textAlign: "right" }}>
          {value}
        </span>
      </div>
    );
  }

  return (
    <Card
      title="Control plane connection"
      desc="How this console reaches the RInfra control plane."
    >
      <div style={{ display: "flex", flexDirection: "column" }}>
        <Row label="Mode" value={rest ? "Live (REST API)" : "Demo (in-browser mock)"} />
        <Row label="API base URL" value={base || "—"} mono />
        <Row label="Active engagement" value={engagementId || "—"} mono />
      </div>
      {!rest && (
        <div style={{ fontSize: 12, color: "var(--text-3)", marginTop: 14, lineHeight: 1.5 }}>
          Set <span className="mono">NEXT_PUBLIC_RINFRA_API</span> to point the console at a
          running control plane. See the Docker install script in the repository root.
        </div>
      )}
    </Card>
  );
}
