import type { RideStatus } from "@/lib/api";

const labels: Record<RideStatus, string> = {
  requested: "Finding a driver",
  accepted: "Driver assigned",
  completed: "Trip completed",
  cancelled: "Trip cancelled",
};

export function StatusPill({ status }: Readonly<{ status: RideStatus }>) {
  return <span className={`status-pill status-${status}`}>{labels[status]}</span>;
}

export function RideProgress({ status }: Readonly<{ status: RideStatus }>) {
  if (status === "cancelled") {
    return (
      <div className="terminal-progress" role="status">
        <span className="terminal-icon" aria-hidden="true">×</span>
        <span>This ride was cancelled.</span>
      </div>
    );
  }

  const complete = status === "completed";
  const requestedStep = status === "requested" ? "is-current" : "is-done";
  const acceptedStep = status === "accepted" ? "is-current" : complete ? "is-done" : "";
  return (
    <ol className="ride-progress" aria-label={`Ride progress: ${labels[status]}`}>
      <li className={`progress-step ${requestedStep}`}>
        <span className="progress-node" aria-hidden="true">1</span>
        <span>Ride requested</span>
      </li>
      <li className={`progress-step ${acceptedStep}`}>
        <span className="progress-node" aria-hidden="true">2</span>
        <span>Driver assigned</span>
      </li>
      <li className={`progress-step ${complete ? "is-done" : ""}`}>
        <span className="progress-node" aria-hidden="true">3</span>
        <span>Trip complete</span>
      </li>
    </ol>
  );
}
