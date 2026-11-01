"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { CarFront, LogOut, MapPin, UserRound } from "lucide-react";
import { Brand } from "@/components/brand";
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
        <Brand />
        {session && <nav className="workspace-nav" aria-label="Workspace"><Link href={session.user.role === "driver" ? "/driver" : "/rider"}>{session.user.role === "driver" ? <CarFront size={17} aria-hidden="true" /> : <MapPin size={17} aria-hidden="true" />}{session.user.role === "driver" ? "Driver workspace" : "Book a ride"}</Link></nav>}
        <div className="header-account">
          {session && (
            <span className="account-name">
              <span className="account-avatar"><UserRound size={16} aria-hidden="true" /></span>
              {session.user.display_name}
            </span>
          )}
          <button className="button button-quiet button-small" type="button" onClick={() => void handleSignOut()} disabled={busy}>
            {busy ? "Signing out…" : "Sign out"}
            <LogOut size={15} aria-hidden="true" />
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
