"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, ApiError, type Category, type Location, type Quote, type QuoteInput, type Ride } from "@/lib/api";
import { formatDistance, formatMoney } from "@/lib/format";

type BookingFormProps = {
  locations: Location[];
  ownerID: string;
  onRideCreated(ride: Ride): void;
};

const signature = (input: QuoteInput) => `${input.pickup_location_id}|${input.destination_location_id}|${input.category}`;
const attemptStorageKey = "ride-lahore.booking-attempt";

type SavedAttempt = { key: string; input: QuoteInput; owner_id: string };

function newIdempotencyKey() {
  return `booking_${crypto.randomUUID()}`;
}

export function BookingForm({ locations, ownerID, onRideCreated }: BookingFormProps) {
  const [pickupID, setPickupID] = useState("");
  const [destinationID, setDestinationID] = useState("");
  const [category, setCategory] = useState<Category>("economy");
  const [quote, setQuote] = useState<Quote | null>(null);
  const [quoteInputSignature, setQuoteInputSignature] = useState<string | null>(null);
  const [quotePending, setQuotePending] = useState(false);
  const [createPending, setCreatePending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const idempotencyKey = useRef<string | null>(null);
  const restoreAttempt = useRef(false);

  const input: QuoteInput = {
    pickup_location_id: pickupID,
    destination_location_id: destinationID,
    category,
  };
  const validRoute = pickupID !== "" && destinationID !== "" && pickupID !== destinationID;
  const hasCurrentQuote = quote !== null && quoteInputSignature === signature(input);

  useEffect(() => {
    if (restoreAttempt.current || locations.length === 0) return;
    restoreAttempt.current = true;
    const restore = async () => {
      try {
        const savedValue = sessionStorage.getItem(attemptStorageKey);
        if (!savedValue) return;
        const saved = JSON.parse(savedValue) as SavedAttempt;
        const hasLocations = locations.some((location) => location.id === saved.input?.pickup_location_id)
          && locations.some((location) => location.id === saved.input?.destination_location_id);
        const validKey = typeof saved.key === "string" && /^[!-~]{1,128}$/.test(saved.key);
        const validCategory = saved.input?.category === "economy" || saved.input?.category === "comfort";
        if (saved.owner_id !== ownerID || !hasLocations || !validKey || !validCategory || saved.input.pickup_location_id === saved.input.destination_location_id) {
          sessionStorage.removeItem(attemptStorageKey);
          return;
        }
        await Promise.resolve();
        setPickupID(saved.input.pickup_location_id);
        setDestinationID(saved.input.destination_location_id);
        setCategory(saved.input.category);
        idempotencyKey.current = saved.key;
      } catch {
        try { sessionStorage.removeItem(attemptStorageKey); } catch { /* Storage may be unavailable. */ }
      }
    };
    void restore();
  }, [locations, ownerID]);

  function resetEstimate() {
    setQuote(null);
    setQuoteInputSignature(null);
    idempotencyKey.current = null;
    setError(null);
    try { sessionStorage.removeItem(attemptStorageKey); } catch { /* Storage may be unavailable. */ }
  }

  async function reviewEstimate() {
    if (!validRoute || quotePending || createPending) return;
    setQuote(null);
    setQuoteInputSignature(null);
    setError(null);
    setQuotePending(true);
    try {
      const nextQuote = await api.quote(input);
      setQuote(nextQuote);
      setQuoteInputSignature(signature(input));
    } catch (reason) {
      setError(reason instanceof ApiError ? reason.message : "Could not calculate an estimate. Please retry.");
    } finally {
      setQuotePending(false);
    }
  }

  async function requestRide(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!hasCurrentQuote || createPending) return;
    setCreatePending(true);
    setError(null);
    const key = idempotencyKey.current ?? newIdempotencyKey();
    idempotencyKey.current = key;
    try { sessionStorage.setItem(attemptStorageKey, JSON.stringify({ key, input, owner_id: ownerID } satisfies SavedAttempt)); } catch { /* The in-memory key still protects retries on this page. */ }
    try {
      const ride = await api.createRide(input, key);
      idempotencyKey.current = null;
      try { sessionStorage.removeItem(attemptStorageKey); } catch { /* Storage may be unavailable. */ }
      onRideCreated(ride);
    } catch (reason) {
      if (reason instanceof ApiError && reason.status === 409) {
        try {
          const current = await api.currentRide();
          if (current.ride) {
            idempotencyKey.current = null;
            try { sessionStorage.removeItem(attemptStorageKey); } catch { /* Storage may be unavailable. */ }
            onRideCreated(current.ride);
            return;
          }
        } catch {
          // Keep the original conflict and allow the rider to explicitly retry it.
        }
      }
      if (reason instanceof ApiError && (reason.code === "network_error" || reason.code === "request_timeout")) {
        try {
          const current = await api.currentRide();
          if (current.ride) {
            idempotencyKey.current = null;
            try { sessionStorage.removeItem(attemptStorageKey); } catch { /* Storage may be unavailable. */ }
            onRideCreated(current.ride);
            return;
          }
        } catch {
          // The same key is retained below so a retry cannot create a second booking.
        }
        setError("We didn’t receive a confirmation. Check your current ride or retry; this attempt keeps the same request key.");
      } else {
        setError(reason instanceof ApiError ? reason.message : "The ride could not be requested. Please retry.");
      }
    } finally {
      setCreatePending(false);
    }
  }

  const pickup = locations.find((location) => location.id === pickupID);
  const destination = locations.find((location) => location.id === destinationID);

  return (
    <div className="booking-layout">
      <section className="panel" aria-labelledby="booking-title">
        <div className="panel-head">
          <div>
            <h2 id="booking-title">Plan a ride</h2>
            <p>Choose a route and review the estimate before requesting.</p>
          </div>
          <span className="demo-chip">LAHORE</span>
        </div>

        {locations.length === 0 ? (
          <div className="notice notice-error" role="alert">There are no active Lahore locations to choose from.</div>
        ) : (
          <div className="form-stack">
            <div className="field-row">
              <div className="field-group">
                <label htmlFor="pickup">Pickup</label>
                <select id="pickup" value={pickupID} onChange={(event) => { setPickupID(event.target.value); resetEstimate(); }}>
                  <option value="">Choose a pickup</option>
                  {locations.map((location) => (
                    <option key={location.id} value={location.id}>{location.name} · {location.area}</option>
                  ))}
                </select>
              </div>
              <div className="field-group">
                <label htmlFor="destination">Destination</label>
                <select id="destination" value={destinationID} onChange={(event) => { setDestinationID(event.target.value); resetEstimate(); }}>
                  <option value="">Choose a destination</option>
                  {locations.map((location) => (
                    <option key={location.id} value={location.id} disabled={location.id === pickupID}>{location.name} · {location.area}</option>
                  ))}
                </select>
              </div>
            </div>
            {pickupID !== "" && pickupID === destinationID && (
              <p className="field-hint" role="alert">Pickup and destination must be different.</p>
            )}

            <fieldset className="field-group choice-fieldset">
              <legend>Ride category</legend>
              <div className="category-options">
                <label className="category-option">
                  <input type="radio" name="category" value="economy" checked={category === "economy"} onChange={() => { setCategory("economy"); resetEstimate(); }} />
                  <strong>Economy</strong>
                  <span>A practical option for everyday trips.</span>
                </label>
                <label className="category-option">
                  <input type="radio" name="category" value="comfort" checked={category === "comfort"} onChange={() => { setCategory("comfort"); resetEstimate(); }} />
                  <strong>Comfort</strong>
                  <span>More room for your journey.</span>
                </label>
              </div>
            </fieldset>

            <div className="button-stack">
              <button className="button button-quiet button-wide" type="button" onClick={() => void reviewEstimate()} disabled={!validRoute || quotePending || createPending}>
                {quotePending ? "Calculating estimate…" : "Review fare estimate"}
              </button>
            </div>

            {error && <p className="notice notice-error" role="alert">{error}</p>}
            {quotePending && <p className="field-hint" role="status">Asking the ride service for an estimate…</p>}

            {hasCurrentQuote && quote && (
              <form className="quote-card" onSubmit={requestRide} aria-label="Ride estimate">
                <div className="quote-card-top">
                  <span className="approx-tag">Approximate estimate</span>
                  <span className="price-large">{formatMoney(quote.estimated_fare_paisa)}</span>
                </div>
                <div className="quote-meta">
                  <span>{formatDistance(quote.estimated_distance_meters)} route distance</span>
                  <span>{quote.pickup_eta_minutes === null ? "Pickup ETA unavailable" : `About ${quote.pickup_eta_minutes} min pickup`}</span>
                </div>
                {quote.pickup_eta_minutes === null && <p className="quote-disclaimer">No eligible available driver could be estimated for this route right now.</p>}
                <p className="quote-disclaimer">Fare and distance are estimated by the ride service using fixed Lahore demo locations. They are not live route or market prices.</p>
                <button className="button button-primary button-wide" type="submit" disabled={createPending}>
                  {createPending ? "Requesting ride…" : "Request this ride"}
                  {!createPending && <span aria-hidden="true">→</span>}
                </button>
                {createPending && <p className="quote-disclaimer" role="status">Your request is being saved. Please keep this page open.</p>}
              </form>
            )}
          </div>
        )}
      </section>

      <aside className="booking-aside">
        <section className="panel" aria-labelledby="route-preview-title">
          <div className="panel-head">
            <div>
              <h2 id="route-preview-title">Your route</h2>
              <p>A simple stop list, without a live map.</p>
            </div>
          </div>
          {pickup && destination ? (
            <div className="route-summary">
              <div className="route-rail" aria-hidden="true"><span className="route-dot route-dot-start" /><span className="route-line" /><span className="route-dot route-dot-end" /></div>
              <div className="route-stops">
                <div className="route-stop"><span className="route-label">PICKUP</span><strong>{pickup.name}</strong><span>{pickup.area}</span></div>
                <div className="route-stop route-stop-destination"><span className="route-label">DESTINATION</span><strong>{destination.name}</strong><span>{destination.area}</span></div>
              </div>
            </div>
          ) : (
            <div className="estimate-empty"><p>Select two different places to preview your route.</p></div>
          )}
        </section>

        <section className="panel">
          <div className="panel-head">
            <div><h2>Before you request</h2></div>
          </div>
          <ul className="plain-list">
            <li>The Go service calculates the fare and pickup estimate.</li>
            <li>Your ride is saved before its status appears here.</li>
            <li>A driver match is not guaranteed by this demo estimate.</li>
          </ul>
        </section>
      </aside>
    </div>
  );
}
