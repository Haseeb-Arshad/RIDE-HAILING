"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { AppFrame } from "@/components/app-frame";
import { AuthGuard } from "@/components/auth-guard";
import { RouteSummary } from "@/components/route-summary";
import { RideProgress, StatusPill } from "@/components/ride-status";
import { api, ApiError, type Ride } from "@/lib/api";
import { useAuth } from "@/lib/auth-provider";
import { formatCategory, formatDistance, formatMoney, shortRideId } from "@/lib/format";

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const isActive = (status: Ride["status"] | undefined) => status === "requested" || status === "accepted";

function RideWorkspace({ rideID }: Readonly<{ rideID: string }>) {
  const { state } = useAuth();
  const router = useRouter();
  const [ride, setRide] = useState<Ride | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [cancelPending, setCancelPending] = useState(false);
  const [retry, setRetry] = useState(0);
  const rideRef = useRef<Ride | null>(null);
  const requestVersion = useRef(0);
  const session = state.status === "signed_in" ? state.session : null;
  const currentRide = ride?.id === rideID ? ride : null;
  const validRideID = uuidPattern.test(rideID);
  const showNotFound = !validRideID || notFound;

  const applyRide = useCallback((nextRide: Ride) => {
    rideRef.current = nextRide;
    setRide(nextRide);
  }, []);

  useEffect(() => {
    if (!validRideID) return;
    rideRef.current = null;

    let disposed = false;
    let inFlight = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let controller: AbortController | undefined;
    const load = async () => {
      if (disposed || document.visibilityState === "hidden" || inFlight) return;
      inFlight = true;
      let nextDelay = 2500;
      controller = new AbortController();
      const version = requestVersion.current;
      try {
        const nextRide = await api.ride(rideID, controller.signal);
        if (disposed || version !== requestVersion.current) return;
        applyRide(nextRide);
        setError(null);
        setNotFound(false);
      } catch (reason) {
        if (disposed || controller.signal.aborted) return;
        if (reason instanceof ApiError && (reason.status === 404 || reason.status === 422)) {
          setNotFound(true);
          return;
        }
        nextDelay = 5000;
        setError(reason instanceof ApiError ? reason.message : "Could not refresh this ride.");
      } finally {
        inFlight = false;
        setLoading(false);
        if (!disposed && !document.hidden && isActive(rideRef.current?.status)) {
          timer = setTimeout(() => void load(), nextDelay);
        }
      }
    };

    const onVisibilityChange = () => {
      if (document.visibilityState === "visible") {
        if (timer) clearTimeout(timer);
        void load();
      } else if (timer) {
        clearTimeout(timer);
      }
    };

    void load();
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => {
      disposed = true;
      if (timer) clearTimeout(timer);
      controller?.abort();
      document.removeEventListener("visibilitychange", onVisibilityChange);
    };
  }, [applyRide, retry, rideID, validRideID]);

  function retryRide() {
    setLoading(true);
    setError(null);
    setNotFound(false);
    rideRef.current = null;
    setRide(null);
    setRetry((value) => value + 1);
  }

  async function cancelRide() {
    if (!ride || !session || !isActive(ride.status) || cancelPending) return;
    if (!window.confirm("Cancel this ride? The ride service will update its saved status.")) return;
    setCancelPending(true);
    setError(null);
    requestVersion.current += 1;
    try {
      const updated = await api.cancelRide(ride.id);
      applyRide(updated);
    } catch (reason) {
      setError(reason instanceof ApiError ? reason.message : "The ride could not be cancelled.");
    } finally {
      setCancelPending(false);
    }
  }

  return (
    <AppFrame>
      <main className="workspace">
        <div className="page-heading">
          <div>
            <p className="eyebrow">RIDER WORKSPACE · RIDE STATUS</p>
            <h1>Your ride</h1>
            <p>Status is refreshed from the ride service while this ride is active.</p>
          </div>
          <button className="button button-quiet button-small" type="button" onClick={() => router.push("/rider")}>Book another</button>
        </div>

        {loading && !currentRide && validRideID && <div className="panel state-inline" aria-live="polite"><span className="loading-mark" aria-hidden="true" /> Loading your saved ride…</div>}
        {showNotFound && <div className="panel error-panel"><p className="notice notice-error" role="alert">This ride could not be found, or it is no longer available to this account.</p><button className="button button-quiet" type="button" onClick={() => router.replace("/rider")}>Return to booking</button></div>}
        {error && !showNotFound && (
          <div className="notice notice-error retry-notice" role="alert">
            <span>{error}</span>
            <button className="button-link" type="button" onClick={retryRide}>Refresh now</button>
          </div>
        )}
        {!currentRide && !loading && !showNotFound && !error && (
          <div className="panel error-panel"><p className="notice">No ride is loaded.</p><button className="button button-quiet" type="button" onClick={retryRide}>Try again</button></div>
        )}

        {currentRide && !showNotFound && (
          <div className="dashboard-grid ride-dashboard">
            <section className="panel ride-card" aria-labelledby="ride-route-title">
              <div className="ride-card-top">
                <div>
                  <p className="ride-id">RIDE {shortRideId(currentRide.id)}</p>
                  <h2 id="ride-route-title" className="subheading">Your route</h2>
                </div>
                <StatusPill status={currentRide.status} />
              </div>
              <RouteSummary pickup={currentRide.pickup} destination={currentRide.destination} />
              {currentRide.assignment && currentRide.status !== "requested" && (
                <div className="driver-match">
                  <span className="driver-match-mark" aria-hidden="true">{currentRide.assignment.driver_display_name.slice(0, 1).toUpperCase()}</span>
                  <div><strong>{currentRide.assignment.driver_display_name}</strong><span>Your driver is assigned to this ride.</span></div>
                </div>
              )}
              <div className="ride-facts">
                <div className="ride-fact"><span>Category</span><strong>{formatCategory(currentRide.category)}</strong></div>
                <div className="ride-fact"><span>Estimated fare</span><strong>{formatMoney(currentRide.estimated_fare_paisa)}</strong></div>
                <div className="ride-fact"><span>Approx. distance</span><strong>{formatDistance(currentRide.estimated_distance_meters)}</strong></div>
                <div className="ride-fact"><span>Pickup estimate</span><strong>{currentRide.pickup_eta_minutes === null ? "Unavailable" : `About ${currentRide.pickup_eta_minutes} min`}</strong></div>
              </div>
              <p className="quote-disclaimer">Saved estimate · PKR · Approximate demo distance and pricing.</p>
              {isActive(currentRide.status) && (
                <div className="ride-actions">
                  <button className="button button-danger" type="button" onClick={() => void cancelRide()} disabled={cancelPending}>
                    {cancelPending ? "Cancelling…" : "Cancel ride"}
                  </button>
                  <span className="ride-polling"><span className="polling-dot" aria-hidden="true" /> Refreshes every few seconds</span>
                </div>
              )}
            </section>

            <aside className="ride-aside">
              <section className="panel">
                <div className="panel-head"><div><h2>Ride progress</h2><p>Changes are confirmed by the ride service.</p></div></div>
                <RideProgress status={currentRide.status} />
              </section>
              <section className="panel">
                <div className="panel-head"><div><h2>Ride details</h2></div></div>
                <dl className="detail-list">
                  <div><dt>Ride ID</dt><dd>{currentRide.id}</dd></div>
                  <div><dt>Requested</dt><dd>{new Date(currentRide.created_at).toLocaleString()}</dd></div>
                </dl>
                {currentRide.status === "completed" && <p className="notice" role="status">Your trip is complete. Thanks for riding with us.</p>}
                {currentRide.status === "cancelled" && <p className="notice" role="status">This ride is closed. You can request another from your rider workspace.</p>}
              </section>
            </aside>
          </div>
        )}
      </main>
    </AppFrame>
  );
}

export default function RidePage() {
  const params = useParams<{ id: string }>();
  return <AuthGuard role="rider"><RideWorkspace rideID={params.id} /></AuthGuard>;
}
