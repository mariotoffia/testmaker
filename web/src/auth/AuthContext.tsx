import { createContext, useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { api } from "../api/client";
import type { Whoami } from "../api/types";

interface AuthValue {
  role: Whoami["role"];
  mode: Whoami["mode"] | "unknown";
  // ready is false until the first whoami settles. Guards must wait for it:
  // role is "anonymous" while resolving, so redirecting before ready would
  // bounce a valid operator to /login on every reload (the location moves
  // before whoami can promote the role back).
  ready: boolean;
  operatorToken: string;
  login: (token: string) => Promise<void>;
  logout: () => void;
}

// eslint-disable-next-line react-refresh/only-export-components
export const AuthCtx = createContext<AuthValue | null>(null);

const KEY = "tm.operatorToken";

export function AuthProvider({ children }: { children: ReactNode }) {
  const [operatorToken, setToken] = useState(() => localStorage.getItem(KEY) ?? "");
  const [role, setRole] = useState<AuthValue["role"]>("anonymous");
  const [mode, setMode] = useState<AuthValue["mode"]>("unknown");
  const [ready, setReady] = useState(false);

  const resolve = useCallback(async (token: string) => {
    const who = await api.whoami(token || undefined);
    setRole(who.role);
    setMode(who.mode);
  }, []);

  // On mount, learn the server's auth mode: none-mode makes everyone operator,
  // so the console is reachable without a login.
  useEffect(() => {
    resolve(operatorToken)
      .catch(() => setRole("anonymous"))
      .finally(() => setReady(true));
  }, [resolve, operatorToken]);

  const login = useCallback(
    async (token: string) => {
      await resolve(token); // throws on a bad token → caller shows the error
      localStorage.setItem(KEY, token);
      setToken(token);
    },
    [resolve],
  );

  const logout = useCallback(() => {
    localStorage.removeItem(KEY);
    setToken("");
    setRole("anonymous");
  }, []);

  const value = useMemo(
    () => ({ role, mode, ready, operatorToken, login, logout }),
    [role, mode, ready, operatorToken, login, logout],
  );
  return <AuthCtx.Provider value={value}>{children}</AuthCtx.Provider>;
}
