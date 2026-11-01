"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { AuthChangeEvent, Session } from "@supabase/supabase-js";
import { api, ApiError, setApiAccessToken, type Role, type SessionResponse } from "@/lib/api";
import { getSupabaseBrowserClient } from "@/lib/supabase/client";

type AuthState =
  | { status: "loading"; session: null; message: null }
  | { status: "signed_out"; session: null; message: null }
  | { status: "signed_in"; session: SessionResponse; message: null }
  | { status: "unlinked"; session: null; message: string }
  | { status: "unavailable"; session: null; message: string };

type AuthContextValue = {
  state: AuthState;
  signIn(email: string, password: string): Promise<SessionResponse>;
  signOut(): Promise<void>;
  refresh(): Promise<void>;
};

const AuthContext = createContext<AuthContextValue | null>(null);
const serviceUnavailableMessage = "We couldn’t reach the ride service. Check that the API is running, then try again.";

export function AuthProvider({ children }: Readonly<{ children: React.ReactNode }>) {
  const [state, setState] = useState<AuthState>({ status: "loading", session: null, message: null });
  const supabase = getSupabaseBrowserClient();

  const loadProfile = useCallback(async (accessToken: string) => {
    setApiAccessToken(accessToken);
    try {
      const session = await api.me();
      setState({ status: "signed_in", session, message: null });
      return session;
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        setApiAccessToken(null);
        setState({ status: "signed_out", session: null, message: null });
      } else if (error instanceof ApiError && error.status === 403) {
        setApiAccessToken(null);
        setState({ status: "unlinked", session: null, message: error.message });
      } else {
        setState({ status: "unavailable", session: null, message: serviceUnavailableMessage });
      }
      throw error;
    }
  }, []);

  const refresh = useCallback(async () => {
    if (!supabase) {
      setApiAccessToken(null);
      setState({ status: "unavailable", session: null, message: "Supabase Auth is not configured for this web app." });
      return;
    }
    const { data, error } = await supabase.auth.getSession();
    if (error) {
      setApiAccessToken(null);
      setState({ status: "unavailable", session: null, message: "We couldn’t restore your Supabase session. Try signing in again." });
      return;
    }
    if (!data.session) {
      setApiAccessToken(null);
      setState({ status: "signed_out", session: null, message: null });
      return;
    }
    try {
      await loadProfile(data.session.access_token);
    } catch {
      // loadProfile reports the appropriate signed-out or unavailable state.
    }
  }, [loadProfile, supabase]);

  useEffect(() => {
    if (!supabase) {
      const initialLoad = window.setTimeout(() => void refresh(), 0);
      return () => window.clearTimeout(initialLoad);
    }

    const { data: { subscription } } = supabase.auth.onAuthStateChange((event: AuthChangeEvent, session: Session | null) => {
      if (event === "INITIAL_SESSION") return;
      queueMicrotask(() => {
        if (!session) {
          setApiAccessToken(null);
          setState({ status: "signed_out", session: null, message: null });
          return;
        }
        void loadProfile(session.access_token).catch(() => undefined);
      });
    });
    const initialLoad = window.setTimeout(() => void refresh(), 0);
    return () => {
      window.clearTimeout(initialLoad);
      subscription.unsubscribe();
    };
  }, [loadProfile, refresh, supabase]);

  useEffect(() => {
    const expireSession = () => {
      setApiAccessToken(null);
      setState({ status: "signed_out", session: null, message: null });
      if (supabase) void supabase.auth.signOut();
    };
    window.addEventListener("ride:unauthorized", expireSession);
    return () => window.removeEventListener("ride:unauthorized", expireSession);
  }, [supabase]);

  const signIn = useCallback(async (email: string, password: string) => {
    if (!supabase) throw new ApiError(0, "auth_not_configured", "Supabase Auth is not configured for this web app.");
    const { data, error } = await supabase.auth.signInWithPassword({ email: email.trim(), password });
    if (error || !data.session) {
      const status = error?.status === 400 || error?.status === 401 ? 401 : error?.status ?? 401;
      throw new ApiError(status, "invalid_credentials", error?.message ?? "Email and password could not be verified.");
    }
    try {
      return await loadProfile(data.session.access_token);
    } catch (profileError) {
      setApiAccessToken(null);
      await supabase.auth.signOut();
      throw profileError;
    }
  }, [loadProfile, supabase]);

  const signOut = useCallback(async () => {
    setApiAccessToken(null);
    setState({ status: "signed_out", session: null, message: null });
    if (!supabase) return;
    const { error } = await supabase.auth.signOut();
    if (error) throw new ApiError(error.status ?? 0, "sign_out_failed", "The Supabase session could not be fully signed out.");
  }, [supabase]);

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
