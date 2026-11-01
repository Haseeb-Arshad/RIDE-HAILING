"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { ApiError } from "@/lib/api";
import { destinationFor, useAuth } from "@/lib/auth-provider";

const demoAccounts = [
  { label: "Rider", email: "rider@example.test" },
  { label: "Driver 1", email: "driver.one@example.test" },
  { label: "Driver 2", email: "driver.two@example.test" },
];

export default function LoginPage() {
  const { state, signIn } = useAuth();
  const router = useRouter();
  const [email, setEmail] = useState(demoAccounts[0].email);
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (state.status === "signed_in") router.replace(destinationFor(state.session.user.role));
  }, [router, state]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setError(null);
    try {
      const session = await signIn(email.trim(), password);
      router.replace(destinationFor(session.user.role));
    } catch (reason) {
      if (reason instanceof ApiError && reason.status === 401) {
        setError("That email and password don’t match a seeded demo account.");
      } else if (reason instanceof ApiError) {
        setError(reason.message);
      } else {
        setError("Sign in failed. Check the API connection and try again.");
      }
    } finally {
      setPending(false);
    }
  }

  return (
    <main className="login-layout">
      <header className="app-header login-header">
        <Link className="brand" href="/" aria-label="Ride Lahore home">
          <span className="brand-mark" aria-hidden="true">R</span>
          <span>Ride Lahore</span>
        </Link>
        <Link className="button button-quiet button-small" href="/">Back to home</Link>
      </header>

      <section className="login-panel" aria-labelledby="login-title">
        <div className="login-intro">
          <p className="eyebrow">LAHORE · RIDE BOOKING DEMO</p>
          <h1 id="login-title">Welcome back.</h1>
          <p>Sign in with one of the seeded accounts to open the rider or driver workspace.</p>
        </div>

        <form className="form-stack" onSubmit={submit}>
          <div className="field-group">
            <label htmlFor="account">Demo account</label>
            <select id="account" value={email} onChange={(event) => setEmail(event.target.value)}>
              {demoAccounts.map((account) => (
                <option key={account.email} value={account.email}>{account.label} · {account.email}</option>
              ))}
            </select>
          </div>
          <div className="field-group">
            <label htmlFor="password">Demo password</label>
            <input
              id="password"
              autoComplete="current-password"
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              required
              maxLength={72}
            />
            <span className="field-hint">Use the password configured when the demo accounts were seeded.</span>
          </div>
          {error && <p className="notice notice-error" role="alert">{error}</p>}
          {state.status === "unavailable" && !error && (
            <p className="notice notice-error" role="alert">{state.message}</p>
          )}
          <button className="button button-primary button-wide" type="submit" disabled={pending || password.length === 0}>
            {pending ? "Signing in…" : "Continue"}
            <span aria-hidden="true">→</span>
          </button>
        </form>

        <p className="login-footnote">
          The session is kept in a secure, HttpOnly cookie. This browser never stores your password or session token.
        </p>
      </section>
      <footer className="app-footer login-footer">
        <span>Ride Lahore</span>
        <span>Local demo · Lahore</span>
      </footer>
    </main>
  );
}
