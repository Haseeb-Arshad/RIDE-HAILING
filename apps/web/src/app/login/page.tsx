import Link from "next/link";

export default function LoginPage() {
  return (
    <main className="page-shell">
      <nav className="topbar" aria-label="Main navigation">
        <Link className="brand" href="/">
          <span className="brand-mark" aria-hidden="true">
            R
          </span>
          <span>Ride Lahore</span>
        </Link>
        <Link className="text-link" href="/">
          Back home
        </Link>
      </nav>

      <section className="login-card" aria-labelledby="login-title">
        <p className="eyebrow">RIDER & DRIVER ACCESS</p>
        <h1 id="login-title">Sign in</h1>
        <p>
          The account screen is ready for the session flow. Authentication and
          demo accounts will be connected in a later stage.
        </p>
        <p className="login-note" role="status">
          Sign-in is not active yet. No credentials are collected on this page.
        </p>
      </section>

      <footer className="page-footer">
        <span>Ride Lahore</span>
        <span>Authentication is not connected yet</span>
      </footer>
    </main>
  );
}
