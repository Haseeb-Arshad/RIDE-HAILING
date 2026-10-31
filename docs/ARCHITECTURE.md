# Architecture

## Current target

The assessment is a small modular monolith. The browser uses one Next.js application. Its same-origin `/api/v1/*` requests are rewritten server-side to a single Go/Chi service. Go owns identity, validation, pricing, matching, and ride state. PostgreSQL is the sole durable source of truth. Assignment and driver availability changes commit in one database transaction.

```mermaid
flowchart LR
    Browser[ Rider / Driver browser ] -->|same-origin /api/v1/*| Web[ Next.js app and proxy ]
    Web -->|server-side rewrite| API[ Go / Chi API ]
    API -->|SQL transactions and queries| DB[(PostgreSQL)]
    DB -->|seeded Lahore locations and demo users| API
```

For local development, run Next.js and Go as separate host processes. PostgreSQL is an external Supabase database; the schema and seed are prepared but await the user's connection string. No local database or Docker container is part of this workflow. The web rewrite sends browser requests through the Next.js server to Go, and the database connection string remains server-side. No message broker or cache is in the booking path.

Application tables are created in the private `ride_hailing` schema, which is omitted from this repository's Supabase Data API schema list. RLS is enabled and forced on each table without browser policies or grants. Confirm the remote project's exposed-schema settings also omit `ride_hailing`. The trusted Go service uses qualified table names and the server-side PostgreSQL connection. The migration and repeatable seed command are prepared locally; applying and exercising them awaits the user's Supabase connection string.

## Ride lifecycle

```mermaid
stateDiagram-v2
    [*] --> requested: rider creates ride
    requested --> accepted: eligible driver accepts
    requested --> cancelled: owning rider cancels
    accepted --> completed: assigned driver completes
    accepted --> cancelled: owning rider cancels
    completed --> [*]
    cancelled --> [*]
```

Terminal states have no outgoing transition. An accepted ride's cancellation or completion also closes its assignment and returns the driver to `available`, in the same PostgreSQL transaction. `busy` is derived from an active assignment and is never a user-selected state.

## Acceptance transaction

1. Start a transaction and lock the requested ride row.
2. Lock the acting driver row in the documented ride-then-driver order.
3. Recheck ride state, driver role/status, city, category, approximate radius, and existing active assignment.
4. Insert the unique assignment and update ride to `accepted` and driver to `busy`.
5. Commit all changes. Any failed precondition rolls the transaction back.

Unique constraints protect one assignment per ride and one active assignment per driver. A partial unique index protects one open ride per rider. These database rules work across API processes; a Go mutex does not. Cancellation and completion use the same lock order and update all related rows atomically.

## Demo boundaries

Current planned behavior uses fixed Lahore coordinates, Haversine distance multiplied by 1.3, illustrative category fares, a 20 km configurable matching radius, and a nearest-driver ETA at an illustrative 25 km/h. Active pages poll every 2–3 seconds. These choices support a deterministic demo; they do not provide routing, live GPS, dispatch guarantees, payment, or real-market fare data.

Redis/GEOSEARCH or PostGIS, live location updates, WebSockets/SSE, event brokers, service extraction, payments, and production deployment are future evolution only. They are not part of this assessment runtime.
