"use client";
import React, { useCallback, useEffect, useState } from "react";
import { Icons } from "../icons";
import { PageHead, Modal, Avatar } from "../ui";
import { useStore } from "../../lib/store";
import { useAuth } from "../../lib/auth";
import { getClient, ApiError } from "../../lib/client";
import type { Role, User } from "../../lib/types";

const ROLE_LABEL: Record<Role, string> = {
  admin: "Administrator",
  lead: "Team lead",
  operator: "Operator",
};

function RoleBadge({ role }: { role: Role }) {
  const cls = role === "admin" ? "danger" : role === "lead" ? "info" : "";
  return <span className={"pill " + cls}>{ROLE_LABEL[role]}</span>;
}

export default function UsersScreen() {
  const { pushToast } = useStore();
  const { role: myRole } = useAuth();
  const client = getClient();

  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);
  const [editUser, setEditUser] = useState<User | null>(null);
  const [pwUser, setPwUser] = useState<User | null>(null);

  const isAdmin = myRole === "admin";

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      setUsers(await client.listUsers());
    } catch (e) {
      pushToast(e instanceof ApiError ? e.message : "Failed to load users", "danger");
    } finally {
      setLoading(false);
    }
  }, [client, pushToast]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const name = (id: string) => users.find((u) => u.id === id)?.displayName || users.find((u) => u.id === id)?.username || "—";

  return (
    <div style={{ padding: "22px 26px", maxWidth: 1000, margin: "0 auto" }}>
      <PageHead title="Users" sub="Operator accounts and roles. Leads manage their own operators; admins manage everyone.">
        <button className="btn primary" onClick={() => setCreateOpen(true)}>
          <Icons.Plus size={15} /> New user
        </button>
      </PageHead>

      {loading ? (
        <div className="card" style={{ padding: 24, color: "var(--text-3)", fontSize: 13 }}>Loading…</div>
      ) : (
        <div className="card" style={{ overflow: "hidden" }}>
          {users.map((u, i) => (
            <div
              key={u.id}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 13,
                padding: "12px 16px",
                borderTop: i === 0 ? "none" : "1px solid var(--border)",
                opacity: u.disabled ? 0.55 : 1,
              }}
            >
              <Avatar name={u.displayName || u.username} size={32} />
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: 14, fontWeight: 600 }}>
                  {u.displayName || u.username}
                  {u.disabled && <span className="pill" style={{ marginLeft: 8 }}>Disabled</span>}
                </div>
                <div className="mono" style={{ fontSize: 12, color: "var(--text-3)" }}>
                  {u.username}{u.email ? ` · ${u.email}` : ""}
                </div>
              </div>
              {u.managerId && (
                <div style={{ fontSize: 12, color: "var(--text-3)" }}>Lead: {name(u.managerId)}</div>
              )}
              <RoleBadge role={u.role} />
              <button className="btn sm" onClick={() => setPwUser(u)} title="Change password">
                <Icons.Lock size={14} />
              </button>
              <button className="btn sm" onClick={() => setEditUser(u)} title="Edit user">
                <Icons.Sliders size={14} />
              </button>
            </div>
          ))}
        </div>
      )}

      {createOpen && (
        <UserModal
          mode="create"
          isAdmin={isAdmin}
          onClose={() => setCreateOpen(false)}
          onSaved={() => {
            setCreateOpen(false);
            void reload();
          }}
        />
      )}
      {editUser && (
        <UserModal
          mode="edit"
          isAdmin={isAdmin}
          user={editUser}
          onClose={() => setEditUser(null)}
          onSaved={() => {
            setEditUser(null);
            void reload();
          }}
        />
      )}
      {pwUser && (
        <PasswordModal user={pwUser} onClose={() => setPwUser(null)} />
      )}
    </div>
  );
}

function UserModal({
  mode,
  isAdmin,
  user,
  onClose,
  onSaved,
}: {
  mode: "create" | "edit";
  isAdmin: boolean;
  user?: User;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { pushToast } = useStore();
  const [username, setUsername] = useState(user?.username ?? "");
  const [displayName, setDisplayName] = useState(user?.displayName ?? "");
  const [email, setEmail] = useState(user?.email ?? "");
  // Leads may only create operators; admins may pick any role.
  const [role, setRole] = useState<Role>(user?.role ?? (isAdmin ? "operator" : "operator"));
  const [password, setPassword] = useState("");
  const [disabled, setDisabled] = useState(user?.disabled ?? false);
  const [busy, setBusy] = useState(false);

  const roleOptions: Role[] = isAdmin ? ["admin", "lead", "operator"] : ["operator"];

  async function submit() {
    setBusy(true);
    try {
      if (mode === "create") {
        if (!username.trim() || !password) {
          pushToast("Username and password are required", "warn");
          setBusy(false);
          return;
        }
        await getClient().createUser({ username: username.trim(), displayName: displayName.trim(), email: email.trim(), role, password });
        pushToast("User created", "ok");
      } else if (user) {
        await getClient().updateUser(user.id, {
          displayName: displayName.trim(),
          email: email.trim(),
          ...(isAdmin ? { role } : {}),
          disabled,
        });
        pushToast("User updated", "ok");
      }
      onSaved();
    } catch (e) {
      pushToast(e instanceof ApiError ? e.message : "Save failed", "danger");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal open onClose={onClose} width={460} label={mode === "create" ? "New user" : "Edit user"}>
      <div style={{ padding: "18px 20px", borderBottom: "1px solid var(--border)", fontWeight: 600 }}>
        {mode === "create" ? "New user" : `Edit ${user?.username}`}
      </div>
      <div style={{ padding: 20, display: "flex", flexDirection: "column", gap: 14, overflow: "auto" }}>
        {mode === "create" && (
          <div className="field">
            <label>Username</label>
            <input className="input" value={username} autoFocus onChange={(e) => setUsername(e.target.value)} placeholder="j.doe" />
          </div>
        )}
        <div className="field">
          <label>Display name</label>
          <input className="input" value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder="Jane Doe" />
        </div>
        <div className="field">
          <label>Email</label>
          <input className="input" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="jane@example.com" />
        </div>
        <div className="field">
          <label>Role</label>
          <select className="input" value={role} disabled={!isAdmin && mode === "edit"} onChange={(e) => setRole(e.target.value as Role)}>
            {roleOptions.map((r) => (
              <option key={r} value={r}>{ROLE_LABEL[r]}</option>
            ))}
          </select>
          {!isAdmin && <div style={{ fontSize: 11.5, color: "var(--text-3)", marginTop: 5 }}>Leads can manage operators only.</div>}
        </div>
        {mode === "create" && (
          <div className="field">
            <label>Initial password</label>
            <input className="input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="••••••" />
          </div>
        )}
        {mode === "edit" && (
          <label style={{ display: "flex", alignItems: "center", gap: 9, fontSize: 13, cursor: "pointer" }}>
            <input type="checkbox" checked={disabled} onChange={(e) => setDisabled(e.target.checked)} />
            Account disabled (cannot sign in)
          </label>
        )}
      </div>
      <div style={{ padding: "14px 20px", borderTop: "1px solid var(--border)", display: "flex", justifyContent: "flex-end", gap: 8 }}>
        <button className="btn" onClick={onClose}>Cancel</button>
        <button className="btn primary" onClick={submit} disabled={busy}>{busy ? "Saving…" : "Save"}</button>
      </div>
    </Modal>
  );
}

function PasswordModal({ user, onClose }: { user: User; onClose: () => void }) {
  const { pushToast } = useStore();
  const [pw, setPw] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit() {
    if (pw.length < 4) {
      pushToast("Password must be at least 4 characters", "warn");
      return;
    }
    setBusy(true);
    try {
      await getClient().changePassword(user.id, pw);
      pushToast("Password changed", "ok");
      onClose();
    } catch (e) {
      pushToast(e instanceof ApiError ? e.message : "Change failed", "danger");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal open onClose={onClose} width={400} label="Change password">
      <div style={{ padding: "18px 20px", borderBottom: "1px solid var(--border)", fontWeight: 600 }}>
        Change password · {user.username}
      </div>
      <div style={{ padding: 20 }}>
        <div className="field">
          <label>New password</label>
          <input className="input" type="password" value={pw} autoFocus onChange={(e) => setPw(e.target.value)} placeholder="••••••" />
        </div>
      </div>
      <div style={{ padding: "14px 20px", borderTop: "1px solid var(--border)", display: "flex", justifyContent: "flex-end", gap: 8 }}>
        <button className="btn" onClick={onClose}>Cancel</button>
        <button className="btn primary" onClick={submit} disabled={busy}>{busy ? "Saving…" : "Set password"}</button>
      </div>
    </Modal>
  );
}
