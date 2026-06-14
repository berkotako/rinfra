"use client";
import React, { useEffect, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { StoreProvider, useStore } from "../lib/store";
import { AuthProvider, useAuth } from "../lib/auth";
import Sidebar from "../components/shell/Sidebar";
import TopBar from "../components/shell/TopBar";
import AppearanceMenu from "../components/shell/AppearanceMenu";
import { Toasts } from "../components/ui";

// Routes that never require authentication: the marketing landing page and the
// login screen itself.
const PUBLIC_ROUTES = new Set(["/", "", "/login"]);

function isPublic(pathname: string): boolean {
  return PUBLIC_ROUTES.has(pathname) || PUBLIC_ROUTES.has(pathname.replace(/\/$/, ""));
}

// Inner component that can access store + auth
function ShellInner({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { toasts } = useStore();
  const { ready, authed } = useAuth();
  const [appearanceOpen, setAppearanceOpen] = useState(false);

  const publicRoute = isPublic(pathname);

  // Auth gate: once localStorage is read, bounce unauthenticated users on a
  // protected route to the login screen.
  useEffect(() => {
    if (ready && !authed && !publicRoute) {
      router.replace("/login");
    }
  }, [ready, authed, publicRoute, router]);

  // The marketing landing page ("/") and the login page render full-bleed,
  // without the console chrome (sidebar/top bar).
  if (publicRoute) {
    return (
      <>
        {children}
        <Toasts items={toasts} />
      </>
    );
  }

  // Protected route: wait for hydration, and don't flash console content while
  // an unauthenticated user is being redirected to /login.
  if (!ready || !authed) {
    return null;
  }

  const isBuilder = pathname === "/infrastructure" || pathname === "/infrastructure/";

  return (
    <div style={{ display: "flex", height: "100%" }}>
      <Sidebar />
      <div
        style={{
          flex: 1,
          minWidth: 0,
          display: "flex",
          flexDirection: "column",
        }}
      >
        <TopBar onAppearance={() => setAppearanceOpen(true)} />
        <div
          style={{
            flex: 1,
            minHeight: 0,
            background: isBuilder ? "var(--canvas-bg)" : "var(--bg)",
          }}
        >
          {children}
        </div>
      </div>
      <AppearanceMenu
        open={appearanceOpen}
        onClose={() => setAppearanceOpen(false)}
      />
      <Toasts items={toasts} />
    </div>
  );
}

export default function AppShell({ children }: { children: React.ReactNode }) {
  return (
    <AuthProvider>
      <StoreProvider>
        <ShellInner>{children}</ShellInner>
      </StoreProvider>
    </AuthProvider>
  );
}
