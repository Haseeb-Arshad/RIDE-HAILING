"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { AppFrame } from "@/components/app-frame";
import { AuthGuard } from "@/components/auth-guard";
import { RouteSummary } from "@/components/route-summary";
import { StatusPill } from "@/components/ride-status";
import { api, ApiError, type DriverRequest, type DriverStatus, type Ride } from "@/lib/api";
import { useAuth } from "@/lib/auth-provider";
import { formatCategory, formatDistance, formatMoney, shortRideId } from "@/lib/format";

function DriverWorkspace() {
  const { state } = useAuth();
  const [availability, setAvailability] = useState<DriverStatus | null>(null);
  const [assignment, setAssignment] = useState<Ride | null>(null);
  const [requests, setRequests] = useState<DriverRequest[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [availabilityPending, setAvailabilityPending] = useState(false);
  const [pendingRideID, setPendingRideID] = useState<string | null>(null);
  const [completing, setCompleting] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const refreshInFlight = useRef(false);
  const refreshAgain = useRef(false);
  const refreshRef = useRef<(forceAfterCurrent?: boolean) => Promise<void>>(async () => {});
  const dataVersion = useRef(0);
  const session = state.status === "signed_in" ? state.session : null;
  const csrfToken = session?.csrf_token;
  const userID = session?.user.id;

  const refresh = useCallback(async (forceAfterCurrent = false) => {
    if (!csrfToken || !userID) return;
    if (refreshInFlight.current) {
      if (forceAfterCurrent) refreshAgain.current = true;
      return;
    }
    refreshInFlight.current = true;
    const version = dataVersion.current;
    try {
      const [statusResponse, assignmentResponse] = await Promise.all([
        api.driverAvailability(),
        api.driverAssignment(),
      ]);
      if (version !== dataVersion.current) return;
      setError(null);
      const ride = assignmentResponse.assignment?.ride ?? null;
      setAvailability(statusResponse.status);
      setAssignment(ride);
      if (statusResponse.status === "available" && !ride) {
        const page = await api.driverRequests();
        setRequests(page.requests);
        setNextCursor(page.next_cursor);
      } else {
        setRequests([]);
        setNextCursor(null);
      }
    } catch (reason) {
      if (version === dataVersion.current) {
        setError(reason instanceof ApiError ? reason.message : "Could not refresh the driver workspace.");
      }
    } finally {
      refreshInFlight.current = false;
      setLoading(false);
      if (refreshAgain.current) {
        refreshAgain.current = false;
        window.setTimeout(() => void refreshRef.current(true), 0);
      }
    }
  }, [csrfToken, userID]);

  useEffect(() => { refreshRef.current = refresh; }, [refresh]);

  useEffect(() => {
    const initialLoad = window.setTimeout(() => void refresh(), 0);
    return () => window.clearTimeout(initialLoad);
  }, [refresh]);

  useEffect(() => {
    const poll = () => {
      if (document.visibilityState === "visible") void refresh();
    };
    const timer = window.setInterval(poll, 3000);
    document.addEventListener("visibilitychange", poll);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", poll);
    };
  }, [refresh]);

  async function changeAvailability(next: "offline" | "available") {
    if (!session || availabilityPending || availability === "busy") return;
    setAvailabilityPending(true);
    setError(null);
    setNotice(null);
    dataVersion.current += 1;
    try {
      const result = await api.setDriverAvailability(next, session.csrf_token);
      setAvailability(result.status);
      setNotice(result.status === "available" ? "You’re available for eligible requests." : "You’re now offline.");
      await refresh(true);
    } catch (reason) {
      setError(reason instanceof ApiError ? reason.message : "Availability could not be changed.");
      await refresh(true);
    } finally {
      setAvailabilityPending(false);
    }
  }

  async function acceptRequest(rideID: string) {
    if (!session || pendingRideID || availability !== "available" || assignment) return;
    setPendingRideID(rideID);
    setError(null);
    setNotice(null);
    dataVersion.current += 1;
    try {
      const accepted = await api.acceptRide(rideID, session.csrf_token);
      setAssignment(accepted);
      setAvailability("busy");
      setNotice("Ride assigned. Your active trip is shown below.");
      await refresh(true);
    } catch (reason) {
      if (reason instanceof ApiError && reason.status === 409) {
        setNotice("This request changed before it could be accepted. The list has been refreshed.");
      } else {
        setError(reason instanceof ApiError ? reason.message : "The request could not be accepted.");
      }
      await refresh(true);
    } finally {
      setPendingRideID(null);
    }
  }

  async function completeRide() {
    if (!session || !assignment || completing) return;
    setCompleting(true);
    setError(null);
    setNotice(null);
    dataVersion.current += 1;
    try {
      const completed = await api.completeRide(assignment.id, session.csrf_token);
      setAssignment(null);
      setRequests([]);
      setNotice(`Trip ${shortRideId(completed.id)} completed. Checking your refreshed availability…`);
      await refresh(true);
    } catch (reason) {
      setError(reason instanceof ApiError ? reason.message : "The trip could not be completed.");
      await refresh(true);
    } finally {
      setCompleting(false);
    }
  }

  async function loadMore() {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true);
    setError(null);
    try {
      const page = await api.driverRequests(nextCursor);
      setRequests((current) => [...current, ...page.requests]);
      setNextCursor(page.next_cursor);
    } catch (reason) {
      setError(reason instanceof ApiError ? reason.message : "More requests could not be loaded.");
    } finally {
      setLoadingMore(false);
    }
  }

  const firstName = session?.user.display_name.split(" ")[0] ?? "there";
  const availabilityLabel = availability ? availability[0].toUpperCase() + availability.slice(1) : "Checking";
  const isBusy = availability === "busy" || assignment !== null;

  return (
    <AppFrame>
      <main className="workspace">
        <div className="page-heading">
          <div>
            <p className="eyebrow">DRIVER WORKSPACE · LAHORE</p>
            <h1>Good to see you, {firstName}.</h1>
            <p>Manage availability and review requests that match your saved driver profile.</p>
          </div>
          <div className="heading-aside"><strong>Driver account</strong><span>Eligible requests only</span></div>
        </div>

        {error && <p className="notice notice-error" role="alert">{error}</p>}
        {notice && <p className="notice" role="status">{notice}</p>}

        <div className="dashboard-grid driver-dashboard">
          <div className="driver-main-column">
            {assignment ? (
              <section className="panel" aria-labelledby="active-trip-title">
                <div className="panel-head">
                  <div><p className="eyebrow">YOUR CURRENT ASSIGNMENT</p><h2 id="active-trip-title">Active trip</h2><p>Ride {shortRideId(assignment.id)}</p></div>
                  <StatusPill status={assignment.status} />
                </div>
                <div className="driver-assignment-content">
                  <RouteSummary pickup={assignment.pickup} destination={assignment.destination} />
                  <div className="ride-facts">
                    <div className="ride-fact"><span>Rider</span><strong>{assignment.rider.display_name}</strong></div>
                    <div className="ride-fact"><span>Category</span><strong>{formatCategory(assignment.category)}</strong></div>
                    <div className="ride-fact"><span>Estimated fare</span><strong>{formatMoney(assignment.estimated_fare_paisa)}</strong></div>
                    <div className="ride-fact"><span>Approx. distance</span><strong>{formatDistance(assignment.estimated_distance_meters)}</strong></div>
                  </div>
                  <p className="quote-disclaimer">Estimate only · This demo does not provide live navigation.</p>
                  <button className="button button-primary button-wide" type="button" onClick={() => void completeRide()} disabled={completing || assignment.status !== "accepted"}>
                    {completing ? "Completing trip…" : "Complete trip"}
                  </button>
                </div>
              </section>
            ) : (
              <section className="panel" aria-labelledby="requests-title">
                <div className="panel-head">
                  <div><h2 id="requests-title">Ride requests</h2><p>Only requests that match your category and approximate pickup radius appear here.</p></div>
                  <span className="ride-polling"><span className="polling-dot" aria-hidden="true" /> Auto-refresh</span>
                </div>
                {loading ? (
                  <div className="state-inline" aria-live="polite"><span className="loading-mark" aria-hidden="true" /> Loading your requests…</div>
                ) : availability === "offline" ? (
                  <div className="request-empty"><strong>You’re offline</strong><p>Go available when you’re ready to receive eligible Lahore requests.</p></div>
                ) : availability === "busy" ? (
                  <div className="request-empty"><strong>Checking your active assignment</strong><p>Availability is busy. Refresh this page if no trip appears.</p></div>
                ) : requests.length === 0 ? (
                  <div className="request-empty"><strong>No matching requests right now</strong><p>We’ll check again shortly while you stay available.</p></div>
                ) : (
                  <div className="request-list">
                    {requests.map((request) => (
                      <article className="request-row" key={request.id}>
                        <div className="request-row-main">
                          <div className="request-route">
                            <div className="route-rail" aria-hidden="true"><span className="route-dot route-dot-start" /><span className="route-line" /><span className="route-dot route-dot-end" /></div>
                            <div className="route-stops">
                              <div className="route-stop"><span className="route-label">PICKUP</span><strong>{request.pickup.name}</strong><span>{request.pickup.area}</span></div>
                              <div className="route-stop"><span className="route-label">DESTINATION</span><strong>{request.destination.name}</strong><span>{request.destination.area}</span></div>
                            </div>
                          </div>
                          <div className="request-facts">
                            <span><strong>{formatCategory(request.category)}</strong></span>
                            <span><strong>{formatMoney(request.estimated_fare_paisa)}</strong> estimate</span>
                            <span>{formatDistance(request.pickup_distance_meters)} from pickup</span>
                          </div>
                        </div>
                        <button className="button button-primary" type="button" onClick={() => void acceptRequest(request.id)} disabled={pendingRideID !== null || isBusy}>
                          {pendingRideID === request.id ? "Accepting…" : "Accept ride"}
                        </button>
                      </article>
                    ))}
                    {nextCursor && <div className="load-more"><button className="button button-quiet" type="button" onClick={() => void loadMore()} disabled={loadingMore}>{loadingMore ? "Loading…" : "Show more requests"}</button></div>}
                  </div>
                )}
              </section>
            )}
          </div>

          <aside className="driver-side-column">
            <section className="panel" aria-labelledby="availability-title">
              <div className="panel-head"><div><h2 id="availability-title">Availability</h2><p>Your driver status is saved by the ride service.</p></div></div>
              <div className="driver-summary">
                <div className="driver-summary-copy"><strong>Current status</strong><span>{availability === "busy" ? "An active assignment keeps you busy." : availability === "available" ? "Eligible requests can be offered to you." : availability === "offline" ? "You are not receiving requests." : "Reading your saved status…"}</span></div>
                <span className={`availability-badge ${availability ? `status-${availability}` : ""}`}>{availabilityLabel}</span>
              </div>
              <div className="availability-controls">
                <button className={`button ${availability === "available" ? "button-primary" : "button-quiet"}`} type="button" onClick={() => void changeAvailability("available")} disabled={loading || availabilityPending || isBusy || availability === "available"}>
                  {availabilityPending && availability !== "available" ? "Saving…" : "Go available"}
                </button>
                <button className={`button ${availability === "offline" ? "button-primary" : "button-quiet"}`} type="button" onClick={() => void changeAvailability("offline")} disabled={loading || availabilityPending || isBusy || availability === "offline"}>
                  {availabilityPending && availability !== "offline" ? "Saving…" : "Go offline"}
                </button>
              </div>
              {isBusy && <p className="field-hint availability-hint">Availability is locked while a ride is assigned. It will be refreshed after completion.</p>}
              {!availability && <button className="button-link refresh-link" type="button" onClick={() => void refresh()}>Refresh status</button>}
            </section>

            <section className="panel">
              <div className="panel-head"><div><h2>How requests are matched</h2></div></div>
              <ul className="plain-list">
                <li>Same Lahore city and ride category.</li>
                <li>Available driver within the demo radius.</li>
                <li>Pickup distances use fixed demo coordinates.</li>
              </ul>
            </section>
          </aside>
        </div>
      </main>
    </AppFrame>
  );
}

export default function DriverPage() {
  return <AuthGuard role="driver"><DriverWorkspace /></AuthGuard>;
}
