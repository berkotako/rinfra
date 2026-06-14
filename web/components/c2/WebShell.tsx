"use client";
import React, { useEffect, useRef, useState } from "react";
import { Icons } from "../icons";
import { Modal } from "../ui";
import { getShellSession, shellPrompt, CLEAR, type ShellSession } from "../../lib/shell";
import type { DeployedC2 } from "../../lib/types";

export default function WebShell({
  d,
  engagementId,
  onClose,
}: {
  d: DeployedC2;
  engagementId: string;
  onClose: () => void;
}) {
  const [buffer, setBuffer] = useState("");
  const [input, setInput] = useState("");
  const [closed, setClosed] = useState(false);
  const sessionRef = useRef<ShellSession | null>(null);
  const outRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const history = useRef<string[]>([]);
  const histIdx = useRef<number>(-1);
  const prompt = shellPrompt(d);

  useEffect(() => {
    const s = getShellSession(engagementId, d);
    sessionRef.current = s;
    s.onData((chunk) => {
      if (chunk === CLEAR) setBuffer("");
      else setBuffer((b) => b + chunk);
    });
    s.onClose(() => {
      setClosed(true);
      setBuffer((b) => b + "\n[session closed]\n");
    });
    s.open();
    const t = setTimeout(() => inputRef.current?.focus(), 50);
    return () => {
      clearTimeout(t);
      s.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Auto-scroll to bottom as output streams in.
  useEffect(() => {
    if (outRef.current) outRef.current.scrollTop = outRef.current.scrollHeight;
  }, [buffer]);

  const submit = () => {
    if (closed) return;
    const line = input;
    setBuffer((b) => b + prompt + line + "\n");
    if (line.trim()) {
      history.current.push(line);
      histIdx.current = history.current.length;
    }
    sessionRef.current?.send(line);
    setInput("");
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      submit();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      if (history.current.length === 0) return;
      histIdx.current = Math.max(0, histIdx.current - 1);
      setInput(history.current[histIdx.current] ?? "");
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      if (history.current.length === 0) return;
      histIdx.current = Math.min(history.current.length, histIdx.current + 1);
      setInput(history.current[histIdx.current] ?? "");
    } else if (e.key === "l" && e.ctrlKey) {
      e.preventDefault();
      setBuffer("");
    }
  };

  return (
    <Modal open onClose={onClose} width={820} label="Web shell">
      {/* title bar */}
      <div
        style={{
          padding: "12px 16px",
          borderBottom: "1px solid var(--border)",
          display: "flex",
          alignItems: "center",
          gap: 10,
        }}
      >
        <span style={{ color: "var(--text-3)" }}>
          <Icons.Terminal size={16} />
        </span>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 14, fontWeight: 600 }}>
            Web shell — {d.name}
          </div>
          <div style={{ fontSize: 11.5, color: "var(--text-3)" }}>
            {d.frameworkName} · {d.ip}
          </div>
        </div>
        <span className={"pill " + (closed ? "" : "ok")} style={{ height: 22 }}>
          <span className={"status-dot " + (closed ? "destroyed" : "live")} style={{ width: 7, height: 7 }} />
          {closed ? "disconnected" : "connected"}
        </span>
        <button className="btn ghost sm" onClick={onClose} style={{ padding: 6 }}>
          <Icons.X size={16} />
        </button>
      </div>

      {/* terminal */}
      <div
        onClick={() => inputRef.current?.focus()}
        style={{
          background: "#0b0e14",
          padding: "12px 14px",
          height: 380,
          display: "flex",
          flexDirection: "column",
          borderBottomLeftRadius: "var(--r-lg)",
          borderBottomRightRadius: "var(--r-lg)",
          cursor: "text",
        }}
      >
        <div
          ref={outRef}
          className="scroll mono"
          style={{
            flex: 1,
            minHeight: 0,
            fontSize: 12.5,
            lineHeight: 1.55,
            color: "#c9d1d9",
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
          }}
        >
          {buffer}
        </div>
        {/* prompt line */}
        <div style={{ display: "flex", alignItems: "center", gap: 6, marginTop: 4 }}>
          <span className="mono" style={{ fontSize: 12.5, color: "#7ee787", flex: "none" }}>
            {prompt}
          </span>
          <input
            ref={inputRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={onKeyDown}
            disabled={closed}
            spellCheck={false}
            autoComplete="off"
            className="mono"
            style={{
              flex: 1,
              minWidth: 0,
              background: "transparent",
              border: "none",
              outline: "none",
              color: "#c9d1d9",
              fontSize: 12.5,
              caretColor: "#7ee787",
            }}
          />
        </div>
      </div>
    </Modal>
  );
}
