"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { ApiError } from "@/lib/api";
import { useAuth } from "@/lib/auth-provider";

export function AppFrame({ children }: Readonly<{ children: React.ReactNode }>) {
  const { state, signOut } = useAuth();
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const session = state.status === "signed_in" ? state.session : null;

  async function handleSignOut() {
    setBusy(true);
    setMessage(null);
    try {
      await signOut();
      router.replace("/login");
    } catch (error) {
      setMessage(error instanceof ApiError ? error.message : "Sign out failed. Please retry.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="app-shell">
      <header className="app-header">
        <Link className="brand" href="/" aria-label="Ride Lahore home">
          <span className="brand-mark" aria-hidden="true">R</span>
          <span>Ride Lahore</span>
        </Link>
        <div className="header-account">
          {session && (
            <span className="account-name">
              <span className="account-dot" aria-hidden="true" />
              {session.user.display_name}
            </span>
          )}
          <button className="button button-quiet button-small" type="button" onClick={() => void handleSignOut()} disabled={busy}>
            {busy ? "Signing out…" : "Sign out"}
          </button>
        </div>
      </header>
      {message && <p className="notice notice-error app-notice" role="alert">{message}</p>}
      {children}
      <footer className="app-footer">
        <span>Ride Lahore</span>
        <span>Approximate estimates · Lahore demo</span>
      </footer>
    </div>
  );
}
