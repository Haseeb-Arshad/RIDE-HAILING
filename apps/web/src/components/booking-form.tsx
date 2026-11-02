"use client";

import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { api, ApiError, type Category, type Location, type LocationInput, type Quote, type QuoteInput, type Ride } from "@/lib/api";
import { formatDate, formatDistance, formatMoney } from "@/lib/format";
import { GoogleRouteMap } from "@/components/google-route-map";
import { Armchair, ArrowRight, CarFront, Check, MapPin, Navigation, ShieldCheck, Sparkles } from "lucide-react";

type BookingFormProps = {
  locations: Location[];
  ownerID: string;
  onRideCreated(ride: Ride): void;
};

const signature = (input: QuoteInput) => JSON.stringify(input);
const attemptStorageKey = "ride-lahore.booking-attempt";

type SavedAttempt = { key: string; input: QuoteInput; owner_id: string };

function newIdempotencyKey() {
  return `booking_${crypto.randomUUID()}`;
}

export function BookingForm({ locations, ownerID, onRideCreated }: BookingFormProps) {
  const [pickupID, setPickupID] = useState("");
  const [destinationID, setDestinationID] = useState("");
  const [pickupPoint, setPickupPoint] = useState<LocationInput | null>(null);
  const [destinationPoint, setDestinationPoint] = useState<LocationInput | null>(null);
  const [selectionTarget, setSelectionTarget] = useState<"pickup" | "destination" | null>(null);
  const [category, setCategory] = useState<Category>("economy");
  const [quote, setQuote] = useState<Quote | null>(null);
  const [quoteInputSignature, setQuoteInputSignature] = useState<string | null>(null);
  const [quotePending, setQuotePending] = useState(false);
  const [createPending, setCreatePending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const idempotencyKey = useRef<string | null>(null);
  const restoreAttempt = useRef(false);

  const input: QuoteInput = {
    ...(pickupPoint ? { pickup: pickupPoint } : { pickup_location_id: pickupID }),
    ...(destinationPoint ? { destination: destinationPoint } : { destination_location_id: destinationID }),
    category,
  };
  const pickupValue = pickupPoint ? "__custom__" : selectionTarget === "pickup" ? "__map__" : pickupID;
  const destinationValue = destinationPoint ? "__custom__" : selectionTarget === "destination" ? "__map__" : destinationID;
  const pickupSelectionReady = Boolean(pickupPoint || pickupID);
  const destinationSelectionReady = Boolean(destinationPoint || destinationID);
  const validRoute = pickupSelectionReady && destinationSelectionReady &&
    !(pickupID !== "" && pickupID === destinationID) &&
    !(pickupPoint && destinationPoint && pickupPoint.latitude === destinationPoint.latitude && pickupPoint.longitude === destinationPoint.longitude);
  const hasCurrentQuote = quote !== null && quoteInputSignature === signature(input);

  useEffect(() => {
    if (restoreAttempt.current || locations.length === 0) return;
    restoreAttempt.current = true;
    const restore = async () => {
      try {
        const savedValue = sessionStorage.getItem(attemptStorageKey);
        if (!savedValue) return;
        const saved = JSON.parse(savedValue) as SavedAttempt;
        const hasLocations = (saved.input?.pickup
          ? Number.isFinite(saved.input.pickup.latitude) && Number.isFinite(saved.input.pickup.longitude)
          : locations.some((location) => location.id === saved.input?.pickup_location_id))
          && (saved.input?.destination
            ? Number.isFinite(saved.input.destination.latitude) && Number.isFinite(saved.input.destination.longitude)
            : locations.some((location) => location.id === saved.input?.destination_location_id));
        const validKey = typeof saved.key === "string" && /^[!-~]{1,128}$/.test(saved.key);
        const validCategory = saved.input?.category === "economy" || saved.input?.category === "comfort";
        if (saved.owner_id !== ownerID || !hasLocations || !validKey || !validCategory || saved.input.pickup_location_id === saved.input.destination_location_id) {
          sessionStorage.removeItem(attemptStorageKey);
          return;
        }
        await Promise.resolve();
        if (saved.input.pickup) setPickupPoint(saved.input.pickup);
        else setPickupID(saved.input.pickup_location_id ?? "");
        if (saved.input.destination) setDestinationPoint(saved.input.destination);
        else setDestinationID(saved.input.destination_location_id ?? "");
        setCategory(saved.input.category);
        idempotencyKey.current = saved.key;
      } catch {
        try { sessionStorage.removeItem(attemptStorageKey); } catch { /* Storage may be unavailable. */ }
      }
    };
    void restore();
  }, [locations, ownerID]);

  useEffect(() => {
    if (!selectionTarget) return;
    document.getElementById("route-map-preview")?.scrollIntoView?.({ behavior: "smooth", block: "center" });
  }, [selectionTarget]);

  function resetEstimate() {
    setQuote(null);
    setQuoteInputSignature(null);
    idempotencyKey.current = null;
    setError(null);
    try { sessionStorage.removeItem(attemptStorageKey); } catch { /* Storage may be unavailable. */ }
  }

  const selectMapPoint = useCallback((coordinates: { latitude: number; longitude: number }) => {
    const point: LocationInput = { name: "Pinned location", area: "Lahore", ...coordinates };
    if (selectionTarget === "pickup") setPickupPoint(point);
    if (selectionTarget === "destination") setDestinationPoint(point);
    setSelectionTarget(null);
    resetEstimate();
  }, [selectionTarget]);

  const rejectMapPoint = useCallback(() => {
    setError("Choose a point inside the Lahore service area.");
  }, []);

  function changeEndpoint(target: "pickup" | "destination", value: string) {
    if (target === "pickup") {
      if (value === "__map__") {
        setPickupID("");
        setPickupPoint(null);
        setSelectionTarget("pickup");
      } else if (value === "__custom__") {
        setSelectionTarget(null);
      } else {
        setPickupID(value);
        setPickupPoint(null);
        setSelectionTarget(null);
      }
    } else if (value === "__map__") {
      setDestinationID("");
      setDestinationPoint(null);
      setSelectionTarget("destination");
    } else if (value === "__custom__") {
      setSelectionTarget(null);
    } else {
      setDestinationID(value);
      setDestinationPoint(null);
      setSelectionTarget(null);
    }
    resetEstimate();
  }

  function updatePoint(target: "pickup" | "destination", update: Partial<LocationInput>) {
    if (target === "pickup") setPickupPoint((current) => current ? { ...current, ...update } : current);
    else setDestinationPoint((current) => current ? { ...current, ...update } : current);
    resetEstimate();
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

  const pickup = pickupPoint
    ? { id: "custom:pickup", ...pickupPoint, city: "Lahore" as const }
    : locations.find((location) => location.id === pickupID);
  const destination = destinationPoint
    ? { id: "custom:destination", ...destinationPoint, city: "Lahore" as const }
    : locations.find((location) => location.id === destinationID);

  return (
    <div className="booking-layout">
      <section className="panel" aria-labelledby="booking-title">
        <div className="panel-head">
          <div>
            <p className="eyebrow booking-step">01 / YOUR JOURNEY</p>
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
                <label htmlFor="pickup"><Navigation size={15} aria-hidden="true" />Pickup</label>
                <select id="pickup" value={pickupValue} onChange={(event) => changeEndpoint("pickup", event.target.value)}>
                  <option value="">Choose a pickup</option>
                  {pickupPoint && <option value="__custom__">{pickupPoint.name} · {pickupPoint.area}</option>}
                  <option value="__map__">Choose anywhere on the map…</option>
                  {locations.map((location) => (
                    <option key={location.id} value={location.id}>{location.name} · {location.area}</option>
                  ))}
                </select>
                {pickupPoint && (
                  <div className="custom-stop-fields">
                    <label htmlFor="pickup-place-label">Pickup place</label>
                    <input id="pickup-place-label" value={pickupPoint.name} maxLength={120} onChange={(event) => updatePoint("pickup", { name: event.target.value })} />
                    <label htmlFor="pickup-area-label">Area or landmark</label>
                    <input id="pickup-area-label" value={pickupPoint.area} maxLength={120} onChange={(event) => updatePoint("pickup", { area: event.target.value })} />
                  </div>
                )}
              </div>
              <div className="field-group">
                <label htmlFor="destination"><MapPin size={15} aria-hidden="true" />Destination</label>
                <select id="destination" value={destinationValue} onChange={(event) => changeEndpoint("destination", event.target.value)}>
                  <option value="">Choose a destination</option>
                  {destinationPoint && <option value="__custom__">{destinationPoint.name} · {destinationPoint.area}</option>}
                  <option value="__map__">Choose anywhere on the map…</option>
                  {locations.map((location) => (
                    <option key={location.id} value={location.id} disabled={location.id === pickupID}>{location.name} · {location.area}</option>
                  ))}
                </select>
                {destinationPoint && (
                  <div className="custom-stop-fields">
                    <label htmlFor="destination-place-label">Destination place</label>
                    <input id="destination-place-label" value={destinationPoint.name} maxLength={120} onChange={(event) => updatePoint("destination", { name: event.target.value })} />
                    <label htmlFor="destination-area-label">Area or landmark</label>
                    <input id="destination-area-label" value={destinationPoint.area} maxLength={120} onChange={(event) => updatePoint("destination", { area: event.target.value })} />
                  </div>
                )}
              </div>
            </div>
            {selectionTarget && <p className="field-hint" role="status">Click the map to place your {selectionTarget} pin. The pin must be within the Lahore service area.</p>}
            {pickupID !== "" && pickupID === destinationID && (
              <p className="field-hint" role="alert">Pickup and destination must be different.</p>
            )}

            <fieldset className="field-group choice-fieldset">
              <legend>Ride category</legend>
              <div className="category-options">
                <label className="category-option">
                  <input type="radio" name="category" value="economy" checked={category === "economy"} onChange={() => { setCategory("economy"); resetEstimate(); }} />
                  <span className="category-art"><CarFront size={32} strokeWidth={1.5} aria-hidden="true" /></span>
                  <strong>Economy</strong>
                  <span>A practical option for everyday trips.</span>
                </label>
                <label className="category-option">
                  <input type="radio" name="category" value="comfort" checked={category === "comfort"} onChange={() => { setCategory("comfort"); resetEstimate(); }} />
                  <span className="category-art"><Armchair size={32} strokeWidth={1.5} aria-hidden="true" /></span>
                  <strong>Comfort</strong>
                  <span>More room for your journey.</span>
                </label>
              </div>
            </fieldset>

            <div className="button-stack">
              <button className="button button-primary button-wide" type="button" onClick={() => void reviewEstimate()} disabled={!validRoute || quotePending || createPending}>
                {quotePending ? "Calculating estimate…" : "Review fare estimate"}
                <ArrowRight size={18} aria-hidden="true" />
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
                <details className="fare-breakdown">
                  <summary>How this estimate is calculated</summary>
                  <div><span>Fuel price · effective {formatDate(quote.fuel_price_effective_date)}</span><strong>{formatMoney(quote.fuel_price_paisa_per_liter)} / litre</strong></div>
                  <div><span>Estimated fuel cost</span><strong>{formatMoney(quote.fuel_cost_paisa)}</strong></div>
                  <div><span>Base fare</span><strong>{formatMoney(quote.base_fare_paisa)}</strong></div>
                  <div><span>Distance operating cost</span><strong>{formatMoney(quote.distance_cost_paisa)}</strong></div>
                </details>
                <p className="quote-disclaimer">Distance is a Haversine estimate with a 1.3 city-road factor. It is not turn-by-turn routing.</p>
                <button className="button button-primary button-wide" type="submit" disabled={createPending}>
                  {createPending ? "Requesting ride…" : "Request this ride"}
                  {!createPending && <ArrowRight size={18} aria-hidden="true" />}
                </button>
                {createPending && <p className="quote-disclaimer" role="status">Your request is being saved. Please keep this page open.</p>}
              </form>
            )}
          </div>
        )}
      </section>

      <aside className="booking-aside">
        <section id="route-map-preview" className="panel" aria-labelledby="route-preview-title">
          <div className="panel-head">
            <div>
              <p className="eyebrow booking-step">02 / THE WAY THERE</p>
              <h2 id="route-preview-title">Your route</h2>
              <p>A little context for your next journey.</p>
            </div>
          </div>
          <GoogleRouteMap
            locations={locations}
            pickupID={pickupPoint ? "custom:pickup" : pickupID}
            destinationID={destinationPoint ? "custom:destination" : destinationID}
            pickupCustom={pickup ?? null}
            destinationCustom={destination ?? null}
            selectionTarget={selectionTarget}
            onMapPointSelected={selectMapPoint}
            onMapPointRejected={rejectMapPoint}
          />
          {pickup && destination ? (
            <div className="route-summary">
              <div className="route-rail" aria-hidden="true"><span className="route-dot route-dot-start" /><span className="route-line" /><span className="route-dot route-dot-end" /></div>
              <div className="route-stops">
                <div className="route-stop"><span className="route-label">PICKUP</span><strong>{pickup.name}</strong><span>{pickup.area}</span></div>
                <div className="route-stop route-stop-destination"><span className="route-label">DESTINATION</span><strong>{destination.name}</strong><span>{destination.area}</span></div>
              </div>
            </div>
          ) : (
            <div className="estimate-empty"><Navigation size={24} strokeWidth={1.5} aria-hidden="true" /><p>Your journey starts with two stops. Choose a pickup and destination.</p></div>
          )}
        </section>

        <section className="panel">
          <div className="panel-head">
            <div className="section-icon-title"><ShieldCheck size={20} aria-hidden="true" /><h2>Good to know</h2></div>
          </div>
          <ul className="plain-list booking-notes">
            <li><Check size={15} aria-hidden="true" />Review your estimate before you book.</li>
            <li><Check size={15} aria-hidden="true" />Follow each update from your ride page.</li>
            <li><Sparkles size={15} aria-hidden="true" />Demo availability depends on eligible drivers.</li>
          </ul>
        </section>
      </aside>
    </div>
  );
}
