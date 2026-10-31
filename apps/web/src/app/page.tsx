import Link from "next/link";

export default function Home() {
  return (
    <main className="page-shell">
      <nav className="topbar" aria-label="Main navigation">
        <Link className="brand" href="/" aria-label="Lahore Ride Hailing home">
          <span className="brand-mark" aria-hidden="true">
            R
          </span>
          <span>Ride Lahore</span>
        </Link>
        <Link className="text-link" href="/login">
          Sign in <span aria-hidden="true">↗</span>
        </Link>
      </nav>

      <section className="hero" aria-labelledby="hero-title">
        <p className="eyebrow">LAHORE · RIDE HAILING</p>
        <h1 id="hero-title">
          Your next ride,
          <br />
          <span>made simple.</span>
        </h1>
        <p className="hero-copy">
          A straightforward way to get across the city. Rider and driver
          journeys are taking shape now.
        </p>
        <Link className="button button-primary" href="/login">
          Explore the demo <span aria-hidden="true">→</span>
        </Link>
      </section>

      <footer className="page-footer">
        <span>Built for Lahore.</span>
        <span>Early build · Core booking flows are in progress</span>
      </footer>
    </main>
  );
}
