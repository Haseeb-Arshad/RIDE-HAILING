import Link from "next/link";
import { ArrowRight, ArrowUpRight, CarFront, Check, MapPin, Route, TicketCheck } from "lucide-react";
import { Brand } from "@/components/brand";

export default function Home() {
  return (
    <main className="landing-shell">
      <header className="app-header landing-header">
        <Brand />
        <Link className="button button-quiet button-small" href="/login">Sign in <ArrowUpRight size={16} aria-hidden="true" /></Link>
      </header>

      <section className="landing-hero" aria-labelledby="hero-title">
        <div className="landing-copy">
          <p className="eyebrow hero-city"><MapPin size={14} aria-hidden="true" /> ROOTED IN LAHORE</p>
          <h1 id="hero-title">Your city.<br /><span>A smoother way<br />to get there.</span></h1>
          <p className="hero-copy">
            From a quick stop in Gulberg to the other side of town. Plan your journey, see an estimate, and follow your ride in one place.
          </p>
          <Link className="button button-primary" href="/login">Find your next ride <ArrowRight size={18} aria-hidden="true" /></Link>
          <p className="hero-footnote"><Check size={14} aria-hidden="true" /> Economy & Comfort <span>·</span> A local booking demo</p>
        </div>

        <div className="landing-route" aria-label="Illustrative Lahore trip flow">
          <div className="landing-route-top">
            <span className="route-label">A LITTLE CLOSER</span>
            <span className="demo-chip">ILLUSTRATIVE TRIP</span>
          </div>
          <div className="landing-city-art" aria-hidden="true"><div className="city-block block-one" /><div className="city-block block-two" /><div className="city-block block-three" /><svg viewBox="0 0 400 180" fill="none"><path d="M68 124H167V57H326" stroke="#285c46" strokeWidth="5" strokeLinejoin="round" /><circle cx="68" cy="124" r="9" fill="#285c46" stroke="white" strokeWidth="4" /><circle cx="326" cy="57" r="9" fill="#b88230" stroke="white" strokeWidth="4" /></svg><span className="city-art-label">LAHORE / PK</span></div>
          <div className="landing-route-stops">
            <div className="landing-stop"><span className="route-dot route-dot-start" /><div><small>START</small><strong>Liberty Market</strong></div></div>
            <div className="landing-stop-line" />
            <div className="landing-stop"><span className="route-dot route-dot-end" /><div><small>DESTINATION</small><strong>MM Alam Road</strong></div></div>
          </div>
          <div className="landing-route-bottom">
            <span>Economy or Comfort</span>
            <CarFront size={23} strokeWidth={1.5} aria-hidden="true" />
          </div>
        </div>
      </section>

      <section className="landing-notes" aria-label="How the demo works">
        <div><span className="note-index"><Route size={23} strokeWidth={1.5} aria-hidden="true" /></span><p><strong>A route of your own</strong>Choose from familiar Lahore stops.</p></div>
        <div><span className="note-index"><TicketCheck size={23} strokeWidth={1.5} aria-hidden="true" /></span><p><strong>Know before you go</strong>Review an estimate before requesting.</p></div>
        <div><span className="note-index"><CarFront size={23} strokeWidth={1.5} aria-hidden="true" /></span><p><strong>Every step in view</strong>Follow your request through completion.</p></div>
      </section>

      <footer className="app-footer landing-footer">
        <span>Ride Lahore</span>
        <span>Approximate demo fares · No live route estimates</span>
      </footer>
    </main>
  );
}
