"use client";
import React, {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
} from "react";
import type { Role, User } from "./types";
import {
  getClient,
  isRestMode,
  setAuthToken,
  getAuthToken,
  ApiError,
} from "./client";

// ---------------------------------------------------------------------------
// Authentication gate — dual mode.
//
//   REST mode  (NEXT_PUBLIC_RINFRA_API set): real authentication against the
//   control plane. login() posts to /auth/login, stores the opaque bearer
//   token, and resolves the operator's role via /auth/me. Role drives nav
//   visibility; the server remains the authoritative authorization gate.
//
//   Mock mode  (static demo build, no backend): a lightweight browser-side
//   gate over a localStorage credential. Not cryptographic — it keeps casual
//   visitors out of the demo. The default account is admin / admin and is
//   editable from Settings → Account. The demo always runs as an admin so all
//   screens are explorable.
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

export interface AuthUser {
  id: string;
  username: string;
  displayName: string;
  role: Role;
}

interface AuthState {
  /** True once the initial session probe completes (avoids hydration flicker). */
  ready: boolean;
  /** Whether the current browser session is signed in. */
  authed: boolean;
  /** The signed-in operator (null until authenticated). */
  user: AuthUser | null;
  /** The signed-in username (convenience; "" when signed out). */
  username: string;
  /** The signed-in role (null when signed out). */
  role: Role | null;
  /** Validate credentials and start a session. Returns null on success, else an error message. */
  login: (username: string, password: string) => Promise<string | null>;
  /** End the current session. */
  logout: () => void;
  /**
   * Change the signed-in operator's account (Settings → Account). In REST mode
   * a password change is sent to the backend (POST /users/{id}/password, which
   * verifies the current password and bcrypt-hashes the new one); username
   * changes are managed from the Users screen. In mock mode it updates the
   * local demo account. Returns an error string or null on success.
   */
  updateAccount: (args: {
    currentPassword: string;
    newUsername?: string;
    newPassword?: string;
  }) => Promise<string | null>;
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

function toAuthUser(u: User): AuthUser {
  return { id: u.id, username: u.username, displayName: u.displayName || u.username, role: u.role };
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const rest = isRestMode();
  const [ready, setReady] = useState(false);
  const [user, setUser] = useState<AuthUser | null>(null);
  // Mock-mode local account (unused in REST mode).
  const [account, setAccount] = useState<StoredAccount>(() => ({
    username: DEFAULT_USERNAME,
    secret: enc(DEFAULT_PASSWORD),
  }));

  // Initial session probe.
  useEffect(() => {
    let cancelled = false;
    if (rest) {
      // Restore a persisted bearer token by resolving /auth/me.
      const token = getAuthToken();
      if (!token) {
        setReady(true);
        return;
      }
      getClient()
        .me()
        .then((u) => {
          if (!cancelled) setUser(toAuthUser(u));
        })
        .catch(() => {
          setAuthToken(null);
        })
        .finally(() => {
          if (!cancelled) setReady(true);
        });
    } else {
      setAccount(loadAccount());
      let signedIn = false;
      try {
        signedIn = sessionStorage.getItem(SESSION_KEY) === "1";
      } catch {
        signedIn = false;
      }
      if (signedIn) {
        const acct = loadAccount();
        setUser({ id: "local", username: acct.username, displayName: acct.username, role: "admin" });
      }
      setReady(true);
    }
    return () => {
      cancelled = true;
    };
  }, [rest]);

  const login = useCallback(
    async (username: string, password: string): Promise<string | null> => {
      if (rest) {
        try {
          const { token, user: u } = await getClient().login(username.trim(), password);
          setAuthToken(token);
          setUser(toAuthUser(u));
          return null;
        } catch (e) {
          if (e instanceof ApiError) return e.message || "Invalid username or password.";
          return "Could not reach the control plane.";
        }
      }
      // Mock mode.
      const acct = loadAccount();
      const ok = username.trim() === acct.username && enc(password) === acct.secret;
      if (!ok) return "Invalid username or password.";
      try {
        sessionStorage.setItem(SESSION_KEY, "1");
      } catch {
        // ignore
      }
      setUser({ id: "local", username: acct.username, displayName: acct.username, role: "admin" });
      return null;
    },
    [rest]
  );

  const logout = useCallback(() => {
    if (rest) {
      // Best-effort server-side invalidation; clear local state regardless.
      void getClient().logout().catch(() => undefined);
      setAuthToken(null);
    } else {
      try {
        sessionStorage.removeItem(SESSION_KEY);
      } catch {
        // ignore
      }
    }
    setUser(null);
  }, [rest]);

  const updateAccount = useCallback(
    async ({
      currentPassword,
      newUsername,
      newPassword,
    }: {
      currentPassword: string;
      newUsername?: string;
      newPassword?: string;
    }): Promise<string | null> => {
      if (rest) {
        if (newUsername && newUsername !== user?.username) {
          return "Username changes are managed from the Users screen.";
        }
        if (!newPassword) {
          return "Enter a new password.";
        }
        if (!user?.id) {
          return "Not signed in.";
        }
        try {
          // The backend verifies currentPassword and bcrypt-hashes newPassword.
          await getClient().changePassword(user.id, newPassword, currentPassword);
          return null;
        } catch (e) {
          return e instanceof ApiError ? e.toastMessage() : "Could not change password.";
        }
      }
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
      setUser((prev) => (prev ? { ...prev, username: nextUsername, displayName: nextUsername } : prev));
      return null;
    },
    [rest, user]
  );

  return (
    <AuthContext.Provider
      value={{
        ready,
        authed: user !== null,
        user,
        username: user?.username ?? account.username,
        role: user?.role ?? null,
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
