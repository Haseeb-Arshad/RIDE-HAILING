import type { Location } from "@/lib/api";

export function RouteSummary({ pickup, destination }: Readonly<{ pickup: Location; destination: Location }>) {
  return (
    <div className="route-summary">
      <div className="route-rail" aria-hidden="true">
        <span className="route-dot route-dot-start" />
        <span className="route-line" />
        <span className="route-dot route-dot-end" />
      </div>
      <div className="route-stops">
        <div className="route-stop">
          <span className="route-label">PICKUP</span>
          <strong>{pickup.name}</strong>
          <span>{pickup.area}</span>
        </div>
        <div className="route-stop route-stop-destination">
          <span className="route-label">DESTINATION</span>
          <strong>{destination.name}</strong>
          <span>{destination.area}</span>
        </div>
      </div>
    </div>
  );
}
