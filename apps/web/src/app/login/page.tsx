"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { ArrowLeft, ArrowRight, LockKeyhole } from "lucide-react";
import { Brand } from "@/components/brand";
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
        setError("That email and password don’t match an approved demo account.");
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
        <Brand />
        <Link className="button button-quiet button-small" href="/"><ArrowLeft size={16} aria-hidden="true" />Back to home</Link>
      </header>

      <section className="login-panel" aria-labelledby="login-title">
        <div className="login-intro">
          <span className="login-symbol"><LockKeyhole size={23} aria-hidden="true" /></span>
          <p className="eyebrow">YOUR NEXT JOURNEY STARTS HERE</p>
          <h1 id="login-title">Welcome back.</h1>
          <p>Choose your demo account and sign in. A ride across Lahore is just a few steps away.</p>
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
            />
            <span className="field-hint">Use the password provided for your demo account.</span>
          </div>
          {error && <p className="notice notice-error" role="alert">{error}</p>}
          {(state.status === "unavailable" || state.status === "unlinked") && !error && (
            <p className="notice notice-error" role="alert">{state.message}</p>
          )}
          <button className="button button-primary button-wide" type="submit" disabled={pending || password.length === 0}>
            {pending ? "Signing in…" : "Continue"}
            <ArrowRight size={18} aria-hidden="true" />
          </button>
        </form>

        <p className="login-footnote">
          <LockKeyhole size={14} aria-hidden="true" /> Secure sign-in. Your password stays with our authentication provider.
        </p>
      </section>
      <footer className="app-footer login-footer">
        <span>Ride Lahore</span>
        <span>Local demo · Lahore</span>
      </footer>
    </main>
  );
}
