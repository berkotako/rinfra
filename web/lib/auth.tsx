"use client";
import React, {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
} from "react";

// ---------------------------------------------------------------------------
// Client-side authentication gate.
//
// RInfra's control plane authenticates operators via the X-RInfra-Operator
// header (a slot for real OIDC later — see internal/api/middleware.go). The web
// console, which also ships as a static demo build (no backend), needs a
// lightweight gate of its own so the operator screens aren't world-open. This
// provider implements that gate entirely in the browser.
//
// NOTE: this is a console access gate, NOT a cryptographic auth system. The
// credential lives in localStorage (lightly obfuscated), so it keeps casual
// visitors out of the demo and gives a place to manage the admin login — it is
// not a substitute for server-side authn, which lands with the OIDC phase.
//
// The default account at first start is username "admin" / password "admin".
// It can be changed from Settings → Account.
// ---------------------------------------------------------------------------

const ACCOUNT_KEY = "rinfra-account";
const SESSION_KEY = "rinfra-session";

export const DEFAULT_USERNAME = "admin";
export const DEFAULT_PASSWORD = "admin";

interface StoredAccount {
  username: string;
  // base64 of the password — obfuscation only, not encryption.
  secret: string;
}

interface AuthState {
  /** True once localStorage has been read (avoids SSR/hydration flicker). */
  ready: boolean;
  /** Whether the current browser session is signed in. */
  authed: boolean;
  /** The configured admin username. */
  username: string;
  /** Validate credentials and start a session. Returns true on success. */
  login: (username: string, password: string) => boolean;
  /** End the current session. */
  logout: () => void;
  /**
   * Change the admin username and/or password. The current password must
   * match. Returns an error string, or null on success.
   */
  updateAccount: (args: {
    currentPassword: string;
    newUsername?: string;
    newPassword?: string;
  }) => string | null;
}

const enc = (s: string): string => {
  try {
    return typeof window !== "undefined" ? window.btoa(unescape(encodeURIComponent(s))) : s;
  } catch {
    return s;
  }
};
function loadAccount(): StoredAccount {
  if (typeof window === "undefined") {
    return { username: DEFAULT_USERNAME, secret: enc(DEFAULT_PASSWORD) };
  }
  try {
    const raw = localStorage.getItem(ACCOUNT_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<StoredAccount>;
      if (parsed.username && parsed.secret) {
        return { username: parsed.username, secret: parsed.secret };
      }
    }
  } catch {
    // fall through to default
  }
  return { username: DEFAULT_USERNAME, secret: enc(DEFAULT_PASSWORD) };
}

function saveAccount(acct: StoredAccount) {
  try {
    localStorage.setItem(ACCOUNT_KEY, JSON.stringify(acct));
  } catch {
    // ignore — private mode etc.
  }
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [ready, setReady] = useState(false);
  const [authed, setAuthed] = useState(false);
  const [account, setAccount] = useState<StoredAccount>(() => ({
    username: DEFAULT_USERNAME,
    secret: enc(DEFAULT_PASSWORD),
  }));

  // Hydrate from localStorage on mount (client only).
  useEffect(() => {
    setAccount(loadAccount());
    try {
      setAuthed(sessionStorage.getItem(SESSION_KEY) === "1");
    } catch {
      setAuthed(false);
    }
    setReady(true);
  }, []);

  const login = useCallback(
    (username: string, password: string): boolean => {
      const acct = loadAccount();
      const ok =
        username.trim() === acct.username && enc(password) === acct.secret;
      if (ok) {
        setAuthed(true);
        try {
          sessionStorage.setItem(SESSION_KEY, "1");
        } catch {
          // ignore
        }
      }
      return ok;
    },
    []
  );

  const logout = useCallback(() => {
    setAuthed(false);
    try {
      sessionStorage.removeItem(SESSION_KEY);
    } catch {
      // ignore
    }
  }, []);

  const updateAccount = useCallback(
    ({
      currentPassword,
      newUsername,
      newPassword,
    }: {
      currentPassword: string;
      newUsername?: string;
      newPassword?: string;
    }): string | null => {
      const acct = loadAccount();
      if (enc(currentPassword) !== acct.secret) {
        return "Current password is incorrect.";
      }
      const nextUsername = (newUsername ?? acct.username).trim();
      if (!nextUsername) {
        return "Username cannot be empty.";
      }
      if (newPassword !== undefined && newPassword.length < 4) {
        return "New password must be at least 4 characters.";
      }
      const next: StoredAccount = {
        username: nextUsername,
        secret: newPassword ? enc(newPassword) : acct.secret,
      };
      saveAccount(next);
      setAccount(next);
      return null;
    },
    []
  );

  return (
    <AuthContext.Provider
      value={{
        ready,
        authed,
        username: account.username,
        login,
        logout,
        updateAccount,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
