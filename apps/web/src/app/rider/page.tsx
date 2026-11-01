"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { AppFrame } from "@/components/app-frame";
import { AuthGuard } from "@/components/auth-guard";
import { BookingForm } from "@/components/booking-form";
import { api, ApiError, type Location } from "@/lib/api";
import { useAuth } from "@/lib/auth-provider";

function RiderWorkspace() {
  const { state } = useAuth();
  const router = useRouter();
  const [locations, setLocations] = useState<Location[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [retry, setRetry] = useState(0);
  const session = state.status === "signed_in" ? state.session : null;
  const userID = session?.user.id;

  useEffect(() => {
    if (!userID) return;
    const controller = new AbortController();
    Promise.all([api.locations(controller.signal), api.currentRide(controller.signal)])
      .then(([locationResponse, currentResponse]) => {
        if (currentResponse.ride) {
          router.replace(`/rides/${encodeURIComponent(currentResponse.ride.id)}`);
          return;
        }
        setLocations(locationResponse.locations);
        setLoading(false);
      })
      .catch((reason: unknown) => {
        if (controller.signal.aborted) return;
        setError(reason instanceof ApiError ? reason.message : "Could not load the booking workspace.");
        setLoading(false);
      });
    return () => controller.abort();
  }, [retry, router, userID]);

  const name = session?.user.display_name.split(" ")[0] ?? "there";

  return (
    <AppFrame>
      <main className="workspace">
        <div className="page-heading">
          <div>
            <p className="eyebrow">RIDER WORKSPACE · LAHORE</p>
            <h1>Where to, {name}?</h1>
            <p>Choose two Lahore demo locations. The ride service will calculate an approximate fare and pickup estimate.</p>
          </div>
          <div className="heading-aside">
            <strong>Rider account</strong>
            <span>Seeded demo · No live map</span>
          </div>
        </div>

        {loading ? (
          <div className="panel state-inline" aria-live="polite"><span className="loading-mark" aria-hidden="true" /> Loading locations and checking your current ride…</div>
        ) : error ? (
          <div className="panel error-panel">
            <p className="notice notice-error" role="alert">{error}</p>
            <button className="button button-quiet" type="button" onClick={() => { setLoading(true); setError(null); setRetry((value) => value + 1); }}>Try again</button>
          </div>
        ) : (
          <BookingForm
            locations={locations}
            ownerID={session?.user.id ?? ""}
            csrfToken={session?.csrf_token ?? ""}
            onRideCreated={(ride) => router.push(`/rides/${encodeURIComponent(ride.id)}`)}
          />
        )}
      </main>
    </AppFrame>
  );
}

export default function RiderPage() {
  return <AuthGuard role="rider"><RiderWorkspace /></AuthGuard>;
}
