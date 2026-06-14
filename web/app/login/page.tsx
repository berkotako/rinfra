"use client";
import React, { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { Icons } from "../../components/icons";
import { useAuth, DEFAULT_USERNAME, DEFAULT_PASSWORD } from "../../lib/auth";

export default function LoginPage() {
  const router = useRouter();
  const { ready, authed, login } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);

  // Already signed in → go straight to the console.
  useEffect(() => {
    if (ready && authed) router.replace("/engagements");
  }, [ready, authed, router]);

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (login(username, password)) {
      setError(null);
      router.replace("/engagements");
    } else {
      setError("Invalid username or password.");
    }
  }

  return (
    <div
      style={{
        minHeight: "100%",
        display: "grid",
        placeItems: "center",
        padding: 24,
        background: "var(--bg)",
      }}
    >
      <div style={{ width: 380, maxWidth: "100%" }}>
        {/* brand */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 11,
            justifyContent: "center",
            marginBottom: 22,
          }}
        >
          <div
            style={{
              width: 34,
              height: 34,
              borderRadius: 9,
              background: "var(--accent)",
              color: "var(--accent-contrast)",
              display: "grid",
              placeItems: "center",
              boxShadow: "var(--shadow-sm)",
            }}
          >
            <Icons.Logo size={20} />
          </div>
          <div style={{ lineHeight: 1.1 }}>
            <div style={{ fontSize: 17, fontWeight: 600, letterSpacing: "-0.02em" }}>
              RInfra
            </div>
            <div style={{ fontSize: 11, color: "var(--text-3)" }}>
              Operations platform
            </div>
          </div>
        </div>

        <form className="card" onSubmit={onSubmit} style={{ padding: "22px 22px 20px" }}>
          <div style={{ fontSize: 16, fontWeight: 600, marginBottom: 4 }}>
            Sign in
          </div>
          <div style={{ fontSize: 13, color: "var(--text-3)", marginBottom: 18 }}>
            Operator console access
          </div>

          <div className="field" style={{ marginBottom: 13 }}>
            <label htmlFor="login-user">Username</label>
            <input
              id="login-user"
              className="input"
              autoComplete="username"
              autoFocus
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="admin"
            />
          </div>

          <div className="field" style={{ marginBottom: 16 }}>
            <label htmlFor="login-pass">Password</label>
            <input
              id="login-pass"
              className="input"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••"
            />
          </div>

          {error && (
            <div
              className="pill danger"
              style={{ width: "100%", justifyContent: "center", marginBottom: 14, height: 32 }}
            >
              <Icons.AlertTriangle size={13} /> {error}
            </div>
          )}

          <button
            type="submit"
            className="btn primary"
            style={{ width: "100%", height: 40 }}
          >
            <Icons.Shield size={16} /> Sign in
          </button>

          <div
            style={{
              marginTop: 16,
              paddingTop: 14,
              borderTop: "1px solid var(--border)",
              fontSize: 11.5,
              color: "var(--text-3)",
              lineHeight: 1.5,
            }}
          >
            Default credentials on a fresh install are{" "}
            <span className="mono" style={{ color: "var(--text-2)" }}>
              {DEFAULT_USERNAME}
            </span>{" "}
            /{" "}
            <span className="mono" style={{ color: "var(--text-2)" }}>
              {DEFAULT_PASSWORD}
            </span>
            . Change them in Settings → Account after signing in.
          </div>
        </form>

        <div style={{ textAlign: "center", marginTop: 16 }}>
          <Link
            href="/"
            className="btn ghost sm"
            style={{ textDecoration: "none" }}
          >
            <Icons.ArrowRight size={14} style={{ transform: "rotate(180deg)" }} /> Back to overview
          </Link>
        </div>
      </div>
    </div>
  );
}
