"use client";
import React, { useCallback, useEffect, useState } from "react";
import { Icons } from "../icons";
import { PageHead, Modal, EmptyState, Avatar } from "../ui";
import { useStore } from "../../lib/store";
import { useAuth } from "../../lib/auth";
import { getClient, ApiError } from "../../lib/client";
import type { Project, User, ProjectMember } from "../../lib/types";

export default function ProjectsScreen() {
  const { pushToast } = useStore();
  const { role, user } = useAuth();
  const client = getClient();

  const [projects, setProjects] = useState<Project[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);
  const [membersFor, setMembersFor] = useState<Project | null>(null);

  const canManage = role === "admin" || role === "lead";

  const userName = useCallback(
    (id: string) => users.find((u) => u.id === id)?.displayName || users.find((u) => u.id === id)?.username || id,
    [users]
  );

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const [ps, us] = await Promise.all([
        client.listProjects(),
        client.listUsers().catch(() => [] as User[]),
      ]);
      setProjects(ps);
      setUsers(us);
    } catch (e) {
      pushToast(e instanceof ApiError ? e.message : "Failed to load projects", "danger");
    } finally {
      setLoading(false);
    }
  }, [client, pushToast]);

  useEffect(() => {
    void reload();
  }, [reload]);

  async function onDelete(p: Project) {
    if (!window.confirm(`Delete project “${p.name}”? This cannot be undone.`)) return;
    try {
      await client.deleteProject(p.id);
      pushToast("Project deleted", "ok");
      void reload();
    } catch (e) {
      pushToast(e instanceof ApiError ? e.message : "Delete failed", "danger");
    }
  }

  return (
    <div style={{ padding: "22px 26px", maxWidth: 1100, margin: "0 auto" }}>
      <PageHead title="Projects" sub="Group engagements by client engagement and assign the operators who work them.">
        {canManage && (
          <button className="btn primary" onClick={() => setCreateOpen(true)}>
            <Icons.Plus size={15} /> New project
          </button>
        )}
      </PageHead>

      {loading ? (
        <div className="card" style={{ padding: 24, color: "var(--text-3)", fontSize: 13 }}>Loading…</div>
      ) : projects.length === 0 ? (
        <EmptyState
          icon="Building"
          title="No projects yet"
          body={canManage ? "Create a project to organize engagements and operators." : "You are not assigned to any projects yet."}
          action={canManage ? <button className="btn primary" onClick={() => setCreateOpen(true)}><Icons.Plus size={15} /> New project</button> : undefined}
        />
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          {projects.map((p) => {
            const mine = p.leadId === user?.id;
            const editable = role === "admin" || mine;
            return (
              <div key={p.id} className="card" style={{ padding: "15px 17px" }}>
                <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: 14 }}>
                  <div style={{ minWidth: 0 }}>
                    <div style={{ display: "flex", alignItems: "center", gap: 9 }}>
                      <span style={{ fontSize: 15, fontWeight: 600 }}>{p.name}</span>
                      {p.clientName && <span className="pill">{p.clientName}</span>}
                    </div>
                    {p.description && (
                      <div style={{ fontSize: 13, color: "var(--text-3)", marginTop: 5, lineHeight: 1.5 }}>{p.description}</div>
                    )}
                    <div style={{ display: "flex", alignItems: "center", gap: 7, marginTop: 9, fontSize: 12, color: "var(--text-3)" }}>
                      <Avatar name={userName(p.leadId)} size={20} />
                      Lead: {userName(p.leadId)}
                    </div>
                  </div>
                  <div style={{ display: "flex", gap: 7, flex: "none" }}>
                    {canManage && (
                      <button className="btn sm" onClick={() => setMembersFor(p)}>
                        <Icons.User size={14} /> Members
                      </button>
                    )}
                    {editable && (
                      <button className="btn sm" onClick={() => onDelete(p)} title="Delete project">
                        <Icons.Trash size={14} />
                      </button>
                    )}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {createOpen && (
        <CreateProjectModal
          users={users}
          isAdmin={role === "admin"}
          onClose={() => setCreateOpen(false)}
          onCreated={() => {
            setCreateOpen(false);
            void reload();
          }}
        />
      )}

      {membersFor && (
        <MembersModal
          project={membersFor}
          users={users}
          onClose={() => setMembersFor(null)}
        />
      )}
    </div>
  );
}

function CreateProjectModal({
  users,
  isAdmin,
  onClose,
  onCreated,
}: {
  users: User[];
  isAdmin: boolean;
  onClose: () => void;
  onCreated: () => void;
}) {
  const { pushToast } = useStore();
  const [name, setName] = useState("");
  const [clientName, setClientName] = useState("");
  const [description, setDescription] = useState("");
  const [leadId, setLeadId] = useState("");
  const [busy, setBusy] = useState(false);
  const leads = users.filter((u) => u.role === "lead" || u.role === "admin");

  async function submit() {
    if (!name.trim()) {
      pushToast("Project name is required", "warn");
      return;
    }
    setBusy(true);
    try {
      await getClient().createProject({
        name: name.trim(),
        clientName: clientName.trim(),
        description: description.trim(),
        leadId: isAdmin && leadId ? leadId : undefined,
      });
      pushToast("Project created", "ok");
      onCreated();
    } catch (e) {
      pushToast(e instanceof ApiError ? e.message : "Create failed", "danger");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal open onClose={onClose} width={480} label="New project">
      <div style={{ padding: "18px 20px", borderBottom: "1px solid var(--border)", fontWeight: 600 }}>New project</div>
      <div style={{ padding: 20, display: "flex", flexDirection: "column", gap: 14, overflow: "auto" }}>
        <div className="field">
          <label>Name</label>
          <input className="input" value={name} autoFocus onChange={(e) => setName(e.target.value)} placeholder="Acme Q2 Red Team" />
        </div>
        <div className="field">
          <label>Client</label>
          <input className="input" value={clientName} onChange={(e) => setClientName(e.target.value)} placeholder="Acme Corp" />
        </div>
        <div className="field">
          <label>Description</label>
          <input className="input" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Scope summary" />
        </div>
        {isAdmin && (
          <div className="field">
            <label>Lead</label>
            <select className="input" value={leadId} onChange={(e) => setLeadId(e.target.value)}>
              <option value="">— assign to me —</option>
              {leads.map((u) => (
                <option key={u.id} value={u.id}>{u.displayName || u.username}</option>
              ))}
            </select>
          </div>
        )}
      </div>
      <div style={{ padding: "14px 20px", borderTop: "1px solid var(--border)", display: "flex", justifyContent: "flex-end", gap: 8 }}>
        <button className="btn" onClick={onClose}>Cancel</button>
        <button className="btn primary" onClick={submit} disabled={busy}>{busy ? "Creating…" : "Create project"}</button>
      </div>
    </Modal>
  );
}

function MembersModal({
  project,
  users,
  onClose,
}: {
  project: Project;
  users: User[];
  onClose: () => void;
}) {
  const { pushToast } = useStore();
  const client = getClient();
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [addId, setAddId] = useState("");
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setMembers(await client.listProjectMembers(project.id));
    } catch (e) {
      pushToast(e instanceof ApiError ? e.message : "Failed to load members", "danger");
    } finally {
      setLoading(false);
    }
  }, [client, project.id, pushToast]);

  useEffect(() => {
    void load();
  }, [load]);

  const memberIds = new Set(members.map((m) => m.userId));
  const candidates = users.filter((u) => !memberIds.has(u.id) && u.id !== project.leadId);
  const name = (id: string) => users.find((u) => u.id === id)?.displayName || users.find((u) => u.id === id)?.username || id;

  async function add() {
    if (!addId) return;
    try {
      await client.addProjectMember(project.id, addId);
      setAddId("");
      void load();
    } catch (e) {
      pushToast(e instanceof ApiError ? e.message : "Add failed", "danger");
    }
  }

  async function remove(userId: string) {
    try {
      await client.removeProjectMember(project.id, userId);
      void load();
    } catch (e) {
      pushToast(e instanceof ApiError ? e.message : "Remove failed", "danger");
    }
  }

  return (
    <Modal open onClose={onClose} width={480} label="Project members">
      <div style={{ padding: "18px 20px", borderBottom: "1px solid var(--border)" }}>
        <div style={{ fontWeight: 600 }}>Members · {project.name}</div>
        <div style={{ fontSize: 12, color: "var(--text-3)", marginTop: 3 }}>Operators assigned to this project.</div>
      </div>
      <div style={{ padding: 20, display: "flex", flexDirection: "column", gap: 12, overflow: "auto" }}>
        <div style={{ display: "flex", gap: 8 }}>
          <select className="input" value={addId} onChange={(e) => setAddId(e.target.value)} style={{ flex: 1 }}>
            <option value="">Add a member…</option>
            {candidates.map((u) => (
              <option key={u.id} value={u.id}>{u.displayName || u.username} ({u.role})</option>
            ))}
          </select>
          <button className="btn primary" onClick={add} disabled={!addId}>
            <Icons.Plus size={14} /> Add
          </button>
        </div>

        {loading ? (
          <div style={{ fontSize: 13, color: "var(--text-3)" }}>Loading…</div>
        ) : members.length === 0 ? (
          <div style={{ fontSize: 13, color: "var(--text-3)" }}>No members yet. The project lead always has access.</div>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            {members.map((m) => (
              <div key={m.userId} style={{ display: "flex", alignItems: "center", gap: 9, padding: "7px 4px" }}>
                <Avatar name={name(m.userId)} size={24} />
                <span style={{ flex: 1, fontSize: 13 }}>{name(m.userId)}</span>
                <button className="btn sm" onClick={() => remove(m.userId)} title="Remove">
                  <Icons.X size={13} />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
      <div style={{ padding: "14px 20px", borderTop: "1px solid var(--border)", display: "flex", justifyContent: "flex-end" }}>
        <button className="btn" onClick={onClose}>Done</button>
      </div>
    </Modal>
  );
}
