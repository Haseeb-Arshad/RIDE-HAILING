import Link from "next/link";

export default function Home() {
  return (
    <main className="landing-shell">
      <header className="app-header landing-header">
        <Link className="brand" href="/" aria-label="Ride Lahore home">
          <span className="brand-mark" aria-hidden="true">R</span>
          <span>Ride Lahore</span>
        </Link>
        <Link className="button button-quiet button-small" href="/login">Sign in <span aria-hidden="true">↗</span></Link>
      </header>

      <section className="landing-hero" aria-labelledby="hero-title">
        <div className="landing-copy">
          <p className="eyebrow">A LOCAL RIDE BOOKING DEMO</p>
          <h1 id="hero-title">Across Lahore,<br /><span>with a little less fuss.</span></h1>
          <p className="hero-copy">
            Choose a pickup and destination, review a server-calculated estimate, then follow the ride from request to arrival.
          </p>
          <Link className="button button-primary" href="/login">Open the demo <span aria-hidden="true">→</span></Link>
        </div>

        <div className="landing-route" aria-label="Illustrative Lahore trip flow">
          <div className="landing-route-top">
            <span className="route-label">LAHORE</span>
            <span className="demo-chip">DEMO LOCATIONS</span>
          </div>
          <div className="landing-route-stops">
            <div className="landing-stop"><span className="route-dot route-dot-start" /><div><small>START</small><strong>Choose a pickup</strong></div></div>
            <div className="landing-stop-line" />
            <div className="landing-stop"><span className="route-dot route-dot-end" /><div><small>DESTINATION</small><strong>Choose where to go</strong></div></div>
          </div>
          <div className="landing-route-bottom">
            <span>Economy or Comfort</span>
            <span className="route-arrow" aria-hidden="true">↗</span>
          </div>
        </div>
      </section>

      <section className="landing-notes" aria-label="How the demo works">
        <div><span className="note-index">01</span><p>Review a fare estimate from the ride service.</p></div>
        <div><span className="note-index">02</span><p>Request a ride and track its saved status.</p></div>
        <div><span className="note-index">03</span><p>Sign in as a driver to accept and complete it.</p></div>
      </section>

      <footer className="app-footer landing-footer">
        <span>Ride Lahore</span>
        <span>Approximate demo fares · No live route estimates</span>
      </footer>
    </main>
  );
}
