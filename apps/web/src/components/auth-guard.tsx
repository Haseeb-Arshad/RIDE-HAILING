"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";
import { destinationFor, useAuth } from "@/lib/auth-provider";
import type { Role } from "@/lib/api";

export function AuthGuard({ role, children }: Readonly<{ role: Role; children: ReactNode }>) {
  const { state, refresh } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (state.status === "signed_out") router.replace("/login");
    if (state.status === "signed_in" && state.session.user.role !== role) {
      router.replace(destinationFor(state.session.user.role));
    }
  }, [role, router, state]);

  if (state.status === "loading") {
    return (
      <main className="state-page" aria-live="polite">
        <div className="loading-mark" aria-hidden="true" />
        <p>Checking your session…</p>
      </main>
    );
  }

  if (state.status === "unavailable") {
    return (
      <main className="state-page">
        <p className="eyebrow">SERVICE UNAVAILABLE</p>
        <h1>We can’t open your workspace yet.</h1>
        <p>{state.message}</p>
        <button className="button button-primary" onClick={() => void refresh()} type="button">
          Try again
        </button>
      </main>
    );
  }

  if (state.status !== "signed_in" || state.session.user.role !== role) return null;
  return <>{children}</>;
}

export function SessionNotice() {
  const { state } = useAuth();
  if (state.status !== "unavailable") return null;
  return (
    <p className="notice notice-error" role="alert">
      {state.message} <Link href="/login">Sign in again</Link>
    </p>
  );
}
