"use client";
import React, { useState, useEffect } from "react";
import { Icons } from "../icons";
import { Modal } from "../ui";
import type { Engagement } from "../../lib/types";

interface Props {
  open: boolean;
  onClose: () => void;
  onCreate: (e: Engagement) => void;
}

interface FormState {
  client: string;
  codename: string;
  scope: string;
  lead: string;
  start: string;
  end: string;
  targets: string;
  excluded: string;
  roe: {
    noPii: boolean;
    businessHours: boolean;
    noProd: boolean;
    dataHandling: boolean;
  };
  authName: string;
  authTitle: string;
  authRef: string;
  consent: boolean;
}

const STEPS = ["Engagement", "Rules of engagement", "Scope", "Authorization"];

export default function NewEngagementFlow({ open, onClose, onCreate }: Props) {
  const [step, setStep] = useState(0);
  const [form, setForm] = useState<FormState>({
    client: "",
    codename: "",
    scope: "External perimeter",
    lead: "R. Okafor",
    start: "2026-06-12",
    end: "2026-07-10",
    targets: "",
    excluded: "",
    roe: { noPii: true, businessHours: false, noProd: false, dataHandling: true },
    authName: "",
    authTitle: "",
    authRef: "",
    consent: false,
  });

  useEffect(() => {
    if (open) setStep(0);
  }, [open]);

  const set = <K extends keyof FormState>(k: K, v: FormState[K]) =>
    setForm((f) => ({ ...f, [k]: v }));
  const setRoe = (k: keyof FormState["roe"], v: boolean) =>
    setForm((f) => ({ ...f, roe: { ...f.roe, [k]: v } }));

  const canNext =
    step === 0
      ? form.client.trim() && form.codename.trim()
      : step === 2
      ? form.targets.trim()
      : step === 3
      ? form.authName.trim() && form.authTitle.trim() && form.consent
      : true;

  const finish = () => {
    onCreate({
      id: "ENG-" + (2412 + Math.floor(Math.random() * 80)),
      client: form.client,
      codename: form.codename,
      scope: form.scope,
      status: "draft",
      auth: form.consent ? "authorized" : "pending",
      authBy: form.authName ? `${form.authName} (${form.authTitle})` : "—",
      start: form.start,
      end: form.end,
      assets: 0,
      live: 0,
      cost: 0,
      lead: form.lead,
      targets: form.targets
        .split("\n")
        .map((s) => s.trim())
        .filter(Boolean),
      frameworks: [],
    });
  };

  const StepField = ({
    label,
    hint,
    children,
  }: {
    label: string;
    hint?: string;
    children: React.ReactNode;
  }) => (
    <div className="field" style={{ marginBottom: 16 }}>
      <label>{label}</label>
      {children}
      {hint && <div className="hint">{hint}</div>}
    </div>
  );

  return (
    <Modal open={open} onClose={onClose} width={620} label="New engagement">
      {/* header with steps */}
      <div
        style={{
          padding: "18px 22px 16px",
          borderBottom: "1px solid var(--border)",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            marginBottom: 16,
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <div
              style={{
                width: 32,
                height: 32,
                borderRadius: 8,
                display: "grid",
                placeItems: "center",
                background: "var(--accent-soft)",
                color: "var(--accent)",
                border: "1px solid var(--accent-soft-border)",
              }}
            >
              <Icons.Target size={17} />
            </div>
            <div style={{ fontSize: 16, fontWeight: 600 }}>New engagement</div>
          </div>
          <button
            className="btn ghost sm"
            onClick={onClose}
            style={{ padding: 6 }}
          >
            <Icons.X size={16} />
          </button>
        </div>
        <div style={{ display: "flex", gap: 6 }}>
          {STEPS.map((s, i) => (
            <div
              key={i}
              style={{ flex: 1, display: "flex", flexDirection: "column", gap: 6 }}
            >
              <div
                style={{
                  height: 3,
                  borderRadius: 99,
                  background: i <= step ? "var(--accent)" : "var(--border-2)",
                  transition: "background .2s",
                }}
              />
              <span
                style={{
                  fontSize: 11,
                  fontWeight: 500,
                  color: i === step ? "var(--text)" : "var(--text-3)",
                }}
              >
                {i + 1}. {s}
              </span>
            </div>
          ))}
        </div>
      </div>

      {/* body */}
      <div
        className="scroll"
        style={{ padding: "20px 22px", flex: 1, minHeight: 220 }}
      >
        {step === 0 && (
          <div className="fade-in">
            <div
              style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 14 }}
            >
              <StepField label="Client organization">
                <input
                  className="input"
                  placeholder="e.g. Meridian Financial Group"
                  value={form.client}
                  onChange={(e) => set("client", e.target.value)}
                  autoFocus
                />
              </StepField>
              <StepField label="Engagement codename">
                <input
                  className="input"
                  placeholder="e.g. Northwind"
                  value={form.codename}
                  onChange={(e) => set("codename", e.target.value)}
                />
              </StepField>
            </div>
            <StepField label="Engagement type">
              <div className="seg" style={{ width: "100%" }}>
                {["External perimeter", "Assumed breach", "Full red team", "Purple team"].map(
                  (s) => (
                    <button
                      key={s}
                      style={{ flex: 1 }}
                      className={form.scope === s ? "active" : ""}
                      onClick={() => set("scope", s)}
                    >
                      {s}
                    </button>
                  )
                )}
              </div>
            </StepField>
            <div
              style={{
                display: "grid",
                gridTemplateColumns: "1fr 1fr 1fr",
                gap: 14,
              }}
            >
              <StepField label="Lead operator">
                <input
                  className="input"
                  value={form.lead}
                  onChange={(e) => set("lead", e.target.value)}
                />
              </StepField>
              <StepField label="Start date">
                <input
                  className="input mono"
                  type="date"
                  value={form.start}
                  onChange={(e) => set("start", e.target.value)}
                />
              </StepField>
              <StepField label="End date">
                <input
                  className="input mono"
                  type="date"
                  value={form.end}
                  onChange={(e) => set("end", e.target.value)}
                />
              </StepField>
            </div>
          </div>
        )}

        {step === 1 && (
          <div className="fade-in">
            <div
              style={{
                fontSize: 13,
                color: "var(--text-2)",
                marginBottom: 16,
                lineHeight: 1.5,
              }}
            >
              Define the operating constraints. These are recorded to the audit
              trail and surfaced to every operator on this engagement.
            </div>
            {(
              [
                [
                  "noPii",
                  "No live PII exfiltration",
                  "Demonstrate access only; never remove real customer data.",
                ],
                [
                  "businessHours",
                  "Restrict to business hours",
                  "Run intrusive actions 09:00–18:00 client local time only.",
                ],
                [
                  "noProd",
                  "Exclude production-impacting techniques",
                  "No DoS, destructive impact, or service-disruptive actions.",
                ],
                [
                  "dataHandling",
                  "Encrypted evidence handling",
                  "All collected evidence encrypted at rest and purged on close.",
                ],
              ] as [keyof FormState["roe"], string, string][]
            ).map(([k, title, desc]) => (
              <div
                key={k}
                onClick={() => setRoe(k, !form.roe[k])}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 13,
                  padding: "13px 14px",
                  borderRadius: "var(--r-md)",
                  border: `1px solid ${form.roe[k] ? "var(--accent-soft-border)" : "var(--border)"}`,
                  background: form.roe[k]
                    ? "var(--accent-soft)"
                    : "var(--surface-2)",
                  marginBottom: 9,
                  cursor: "pointer",
                  transition: "all .12s",
                }}
              >
                <div style={{ flex: 1 }}>
                  <div style={{ fontSize: 13, fontWeight: 600 }}>{title}</div>
                  <div
                    style={{
                      fontSize: 12,
                      color: "var(--text-3)",
                      marginTop: 2,
                    }}
                  >
                    {desc}
                  </div>
                </div>
                <div className={"toggle " + (form.roe[k] ? "on" : "")} />
              </div>
            ))}
          </div>
        )}

        {step === 2 && (
          <div className="fade-in">
            <StepField
              label="Authorized targets"
              hint="One per line. CIDR ranges, hostnames, and wildcard domains. Infrastructure can only direct traffic at these."
            >
              <textarea
                className="input mono"
                style={{ fontSize: 12.5, minHeight: 96 }}
                placeholder={"*.client.com\n203.0.113.0/24\ncorp.client.internal"}
                value={form.targets}
                onChange={(e) => set("targets", e.target.value)}
                autoFocus
              />
            </StepField>
            <StepField
              label="Explicit exclusions"
              hint="Out-of-scope assets that must never be touched."
            >
              <textarea
                className="input mono"
                style={{ fontSize: 12.5, minHeight: 64 }}
                placeholder={"payments.client.com\n10.0.0.0/24 (OT segment)"}
                value={form.excluded}
                onChange={(e) => set("excluded", e.target.value)}
              />
            </StepField>
          </div>
        )}

        {step === 3 && (
          <div className="fade-in">
            <div
              style={{
                display: "flex",
                gap: 10,
                padding: "12px 14px",
                borderRadius: "var(--r-md)",
                background: "var(--warn-soft)",
                border: "1px solid var(--warn-soft-border)",
                marginBottom: 18,
              }}
            >
              <span style={{ color: "var(--warn)", marginTop: 1 }}>
                <Icons.Lock size={16} />
              </span>
              <div
                style={{
                  fontSize: 12.5,
                  color: "var(--text-2)",
                  lineHeight: 1.5,
                }}
              >
                No infrastructure can be provisioned until a named authorizing
                party is recorded. This gate is enforced platform-wide.
              </div>
            </div>
            <div
              style={{
                display: "grid",
                gridTemplateColumns: "1fr 1fr",
                gap: 14,
              }}
            >
              <StepField label="Authorizing party">
                <input
                  className="input"
                  placeholder="Full name"
                  value={form.authName}
                  onChange={(e) => set("authName", e.target.value)}
                  autoFocus
                />
              </StepField>
              <StepField label="Title">
                <input
                  className="input"
                  placeholder="e.g. CISO"
                  value={form.authTitle}
                  onChange={(e) => set("authTitle", e.target.value)}
                />
              </StepField>
            </div>
            <StepField
              label="Authorization reference"
              hint="Signed SOW / authorization letter reference number."
            >
              <input
                className="input mono"
                placeholder="AUTH-2026-0142"
                value={form.authRef}
                onChange={(e) => set("authRef", e.target.value)}
                style={{ fontSize: 12.5 }}
              />
            </StepField>
            <div
              onClick={() => set("consent", !form.consent)}
              style={{
                display: "flex",
                alignItems: "flex-start",
                gap: 11,
                padding: "13px 14px",
                borderRadius: "var(--r-md)",
                border: `1px solid ${form.consent ? "var(--accent)" : "var(--border-2)"}`,
                background: form.consent
                  ? "var(--accent-soft)"
                  : "var(--surface-2)",
                cursor: "pointer",
                marginTop: 4,
              }}
            >
              <div
                style={{
                  width: 18,
                  height: 18,
                  flex: "none",
                  borderRadius: 5,
                  marginTop: 1,
                  border: `1.5px solid ${form.consent ? "var(--accent)" : "var(--border-strong)"}`,
                  background: form.consent ? "var(--accent)" : "transparent",
                  display: "grid",
                  placeItems: "center",
                  color: "#fff",
                }}
              >
                {form.consent && <Icons.Check size={13} />}
              </div>
              <div
                style={{
                  fontSize: 12.5,
                  color: "var(--text-2)",
                  lineHeight: 1.5,
                }}
              >
                I confirm written authorization is on file and that all
                operations will remain within the defined scope and rules of
                engagement.
              </div>
            </div>
          </div>
        )}
      </div>

      {/* footer */}
      <div
        style={{
          padding: "14px 22px",
          borderTop: "1px solid var(--border)",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <button
          className="btn ghost"
          onClick={step === 0 ? onClose : () => setStep(step - 1)}
        >
          {step === 0 ? "Cancel" : "Back"}
        </button>
        <div style={{ display: "flex", gap: 9 }}>
          {step < 3 ? (
            <button
              className="btn primary"
              disabled={!canNext}
              onClick={() => setStep(step + 1)}
            >
              Continue <Icons.ArrowRight size={15} />
            </button>
          ) : (
            <button
              className="btn primary"
              disabled={!canNext}
              onClick={finish}
            >
              <Icons.ShieldCheck size={15} /> Create engagement
            </button>
          )}
        </div>
      </div>
    </Modal>
  );
}
