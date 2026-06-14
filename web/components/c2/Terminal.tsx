"use client";
import React, { useEffect, useRef, useState } from "react";
import { getShellSession, shellPrompt, CLEAR, type ShellSession } from "../../lib/shell";
import type { DeployedC2 } from "../../lib/types";

// TerminalPane is the inline operator terminal: a dark console bound to one
// DeployedC2's shell session. Reused by the manual-access modal and the
// multi-pane Alive C2s page.
export default function TerminalPane({
  d,
  engagementId,
  height = 360,
}: {
  d: DeployedC2;
  engagementId: string;
  height?: number;
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
    return () => s.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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
    <div
      onClick={() => inputRef.current?.focus()}
      style={{
        position: "relative",
        background: "#0b0e14",
        padding: "12px 14px",
        height,
        display: "flex",
        flexDirection: "column",
        borderRadius: "var(--r-md)",
        cursor: "text",
      }}
    >
      <span
        className="mono"
        style={{
          position: "absolute",
          top: 8,
          right: 12,
          fontSize: 10,
          color: closed ? "#f0883e" : "#7ee787",
          display: "inline-flex",
          alignItems: "center",
          gap: 5,
        }}
      >
        <span
          style={{
            width: 6,
            height: 6,
            borderRadius: 99,
            background: closed ? "#f0883e" : "#7ee787",
            display: "inline-block",
          }}
        />
        {closed ? "disconnected" : "connected"}
      </span>
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
  );
}
