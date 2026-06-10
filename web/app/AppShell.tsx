"use client";
import React, { useState } from "react";
import { usePathname } from "next/navigation";
import { StoreProvider, useStore } from "../lib/store";
import Sidebar from "../components/shell/Sidebar";
import TopBar from "../components/shell/TopBar";
import AppearanceMenu from "../components/shell/AppearanceMenu";
import { Toasts } from "../components/ui";

// Inner component that can access store
function ShellInner({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const { toasts } = useStore();
  const [appearanceOpen, setAppearanceOpen] = useState(false);

  // The marketing landing page ("/") renders full-bleed, without the console
  // chrome (sidebar/top bar). All other routes get the operator shell.
  const isLanding = pathname === "/" || pathname === "";
  if (isLanding) {
    return (
      <>
        {children}
        <Toasts items={toasts} />
      </>
    );
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
    <StoreProvider>
      <ShellInner>{children}</ShellInner>
    </StoreProvider>
  );
}
