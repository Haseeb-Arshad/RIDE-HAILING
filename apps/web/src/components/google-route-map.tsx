"use client";

import { importLibrary, setOptions } from "@googlemaps/js-api-loader";
import { useEffect, useRef, useState } from "react";
import type { Location } from "@/lib/api";
import { MapPinned, TriangleAlert } from "lucide-react";

const apiKey = process.env.NEXT_PUBLIC_GOOGLE_MAPS_API_KEY?.trim() ?? "";
const lahoreCenter = { lat: 31.5204, lng: 74.3587 };

let mapsLibraryPromise: Promise<google.maps.MapsLibrary> | null = null;

function loadMapsLibrary() {
  if (!apiKey) return Promise.reject(new Error("Google Maps browser key is not configured."));
  if (!mapsLibraryPromise) {
    setOptions({ key: apiKey, v: "weekly" });
    mapsLibraryPromise = importLibrary("maps") as Promise<google.maps.MapsLibrary>;
  }
  return mapsLibraryPromise;
}

type GoogleRouteMapProps = {
  locations: Location[];
  pickupID: string;
  destinationID: string;
};

type MapState = "missing-key" | "loading" | "ready" | "error";

export function GoogleRouteMap({ locations, pickupID, destinationID }: GoogleRouteMapProps) {
  const hasCompleteRoute = pickupID !== "" && destinationID !== "";
  const canvasRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<google.maps.Map | null>(null);
  const markersRef = useRef<google.maps.Marker[]>([]);
  const [state, setState] = useState<MapState>(apiKey ? "loading" : "missing-key");

  useEffect(() => {
    let active = true;
    if (!apiKey) return () => { active = false; };

    void loadMapsLibrary()
      .then(({ Map }) => {
        if (!active || !canvasRef.current) return;
        mapRef.current = new Map(canvasRef.current, {
          center: lahoreCenter,
          zoom: 11,
          mapTypeControl: false,
          streetViewControl: false,
          fullscreenControl: false,
          clickableIcons: false,
          gestureHandling: "cooperative",
        });
        setState("ready");
      })
      .catch(() => {
        if (active) setState("error");
      });

    return () => {
      active = false;
      markersRef.current.forEach((marker) => marker.setMap(null));
      markersRef.current = [];
      mapRef.current = null;
    };
  }, []);

  useEffect(() => {
    const map = mapRef.current;
    if (!map || state !== "ready") return;

    markersRef.current.forEach((marker) => marker.setMap(null));
    markersRef.current = [];

    const selectedLocations = locations.filter((location) => location.id === pickupID || location.id === destinationID);
    const visibleLocations = hasCompleteRoute && selectedLocations.length > 0 ? selectedLocations : locations;
    const bounds = new google.maps.LatLngBounds();

    for (const location of visibleLocations) {
      if (!Number.isFinite(location.latitude) || !Number.isFinite(location.longitude)) continue;
      const isPickup = location.id === pickupID;
      const isDestination = location.id === destinationID;
      const selected = isPickup || isDestination;
      const marker = new google.maps.Marker({
        map,
        position: { lat: location.latitude, lng: location.longitude },
        title: `${isPickup ? "Pickup · " : isDestination ? "Destination · " : ""}${location.name}, ${location.area}`,
        label: isPickup ? { text: "P", color: "#ffffff", fontWeight: "700" } : isDestination ? { text: "D", color: "#ffffff", fontWeight: "700" } : undefined,
        icon: {
          path: google.maps.SymbolPath.CIRCLE,
          scale: selected ? 9 : 6,
          fillColor: isPickup ? "#1d624c" : isDestination ? "#d99b35" : "#70817a",
          fillOpacity: 1,
          strokeColor: "#ffffff",
          strokeWeight: 2,
        },
        zIndex: selected ? 2 : 1,
      });
      markersRef.current.push(marker);
      bounds.extend({ lat: location.latitude, lng: location.longitude });
    }

    if (visibleLocations.length === 1) {
      const location = visibleLocations[0];
      map.setCenter({ lat: location.latitude, lng: location.longitude });
      map.setZoom(14);
    } else if (visibleLocations.length > 1) {
      map.fitBounds(bounds, 44);
    } else {
      map.setCenter(lahoreCenter);
      map.setZoom(11);
    }
  }, [destinationID, hasCompleteRoute, locations, pickupID, state]);

  return (
    <section className="google-map-card" aria-label="Google Maps route preview">
      <div className="google-map-heading">
        <span>GOOGLE MAPS</span>
        <span className={`map-provider-status map-provider-${state}`}>
          {state === "ready" ? "API LOADED" : state === "loading" ? "LOADING" : state === "error" ? "SETUP ISSUE" : "KEY NEEDED"}
        </span>
      </div>
      <div className={`google-map-surface map-surface-${state}`}>
        <div className="google-map-canvas" ref={canvasRef} style={{ visibility: state === "ready" ? "visible" : "hidden" }} aria-hidden={state !== "ready"} aria-label="Map showing selected Lahore pickup points" />
      {state === "ready" ? null : state === "loading" ? (
        <div className="google-map-message" role="status" aria-busy="true">
          <span className="loading-mark" aria-hidden="true" />
          <div><strong>Loading the Lahore map</strong><span>Connecting to the Maps JavaScript API.</span></div>
        </div>
      ) : state === "missing-key" ? (
        <div className="google-map-message" role="status">
          <span className="map-pin-mark"><MapPinned size={25} strokeWidth={1.5} aria-hidden="true" /></span>
          <div>
            <strong>Google Maps is ready to connect</strong>
            <span>The map preview is awaiting setup. You can still plan your journey with our Lahore demo stops.</span>
          </div>
        </div>
      ) : (
        <div className="google-map-message" role="status">
          <span className="map-pin-mark map-pin-alert"><TriangleAlert size={22} aria-hidden="true" /></span>
          <div>
            <strong>The map could not load</strong>
            <span>The map is unavailable right now. Your selected stops and booking options are still available.</span>
          </div>
        </div>
      )}
      </div>
      <div className="google-map-legend" aria-label="Map markers">
        {pickupID && <span><i className="legend-dot legend-pickup" />Pickup</span>}
        {destinationID && <span><i className="legend-dot legend-destination" />Destination</span>}
        {!pickupID && !destinationID && <span><i className="legend-dot legend-stop" />Seeded stops</span>}
        {Boolean(pickupID || destinationID) && !hasCompleteRoute && <span><i className="legend-dot legend-stop" />Other demo stops</span>}
      </div>
    </section>
  );
}
