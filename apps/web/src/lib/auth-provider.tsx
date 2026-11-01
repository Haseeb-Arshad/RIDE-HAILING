"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { api, ApiError, type Role, type SessionResponse } from "@/lib/api";

type AuthState =
  | { status: "loading"; session: null; message: null }
  | { status: "signed_out"; session: null; message: null }
  | { status: "signed_in"; session: SessionResponse; message: null }
  | { status: "unavailable"; session: null; message: string };

type AuthContextValue = {
  state: AuthState;
  signIn(email: string, password: string): Promise<SessionResponse>;
  signOut(): Promise<void>;
  refresh(): Promise<void>;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: Readonly<{ children: React.ReactNode }>) {
  const [state, setState] = useState<AuthState>({ status: "loading", session: null, message: null });

  const refresh = useCallback(async () => {
    try {
      const session = await api.me();
      setState({ status: "signed_in", session, message: null });
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        setState({ status: "signed_out", session: null, message: null });
      } else {
        setState({
          status: "unavailable",
          session: null,
          message: "We couldn’t reach the ride service. Check that the API is running, then try again.",
        });
      }
    }
  }, []);

  useEffect(() => {
    const initialLoad = window.setTimeout(() => void refresh(), 0);
    return () => window.clearTimeout(initialLoad);
  }, [refresh]);

  useEffect(() => {
    const expireSession = () => setState({ status: "signed_out", session: null, message: null });
    window.addEventListener("ride:unauthorized", expireSession);
    return () => window.removeEventListener("ride:unauthorized", expireSession);
  }, []);

  const signIn = useCallback(async (email: string, password: string) => {
    const session = await api.login(email, password);
    setState({ status: "signed_in", session, message: null });
    return session;
  }, []);

  const signOut = useCallback(async () => {
    if (state.status !== "signed_in") {
      setState({ status: "signed_out", session: null, message: null });
      return;
    }
    await api.logout(state.session.csrf_token);
    setState({ status: "signed_out", session: null, message: null });
  }, [state]);

  const value = useMemo(() => ({ state, signIn, signOut, refresh }), [state, signIn, signOut, refresh]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error("useAuth must be used inside AuthProvider");
  return value;
}

export function destinationFor(role: Role) {
  return role === "driver" ? "/driver" : "/rider";
}
