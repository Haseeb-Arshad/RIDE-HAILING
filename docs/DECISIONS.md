# Architecture decisions

These decisions establish the assessment baseline. Revisit only when implementation evidence shows a correctness issue or a requirement is missing.

## ADR 1 — Go and Chi own ride behavior

Use Go with Chi for a compact HTTP API, explicit request handling, standard-library context/logging/testing, and direct control over transaction boundaries. Business rules live in this service; Next.js is not a second backend. This keeps role checks, fare authority, idempotency, and state transitions in one place.

## ADR 2 — Next.js and Go remain separate runtimes in one repository

Keep `apps/web` and `apps/api` together so one assessment checkout can run the complete journey, while keeping the presentation runtime independent from the business API. Run them as separate local processes. A server-side Next rewrite gives browser requests a same-origin `/api/v1` URL and avoids exposing internal API hosts or adding a CORS workaround. Use the user's Supabase PostgreSQL project for database work rather than starting a local database service.

## ADR 3 — PostgreSQL is the source of truth

Use SQL migrations, pgx, explicit transactions, row locks, foreign keys, and uniqueness constraints. Race-sensitive rules must survive multiple API processes and restarts. An in-process mutex, cache, mock database, or ORM convention cannot replace database invariants. The trade-off is more explicit SQL and integration setup, which is justified by the concurrency evaluation.

## ADR 4 — Modular monolith, no broker in the booking path

One Go service and one PostgreSQL database are sufficient for the bounded demo. Redis, Kafka, and separate services add operational work without improving the core atomic booking invariant at this scale. If future asynchronous consumers are introduced, use an outbox and idempotent consumers rather than publishing an event in a gap after the database commit.

## ADR 5 — Poll before adding realtime infrastructure

Poll active ride and driver views every 2–3 seconds, stop at terminal states, and avoid unnecessary requests for hidden tabs. This is easy to reason about and sufficient for a two-browser assessment. It trades update latency and request volume for a small operational surface; live presence and push delivery are future work.

## ADR 6 — Seeded identity and locations keep the scope testable

Seed one rider and two driver accounts with hashed passwords and fixed Lahore demo locations/positions. Do not add signup, OAuth, map providers, or GPS collection. These fixtures enable deterministic authorization, matching, and race tests, but they are not a production identity or location model.

## ADR 7 — Estimates are transparent demo calculations

Persist fare as integer paisa. Estimate distance from fixed coordinates with Haversine multiplied by 1.3; apply the documented Economy/Comfort schedule; derive pickup ETA from the nearest eligible demo driver at 25 km/h. Label all estimates approximate and return a null pickup ETA when no eligible driver exists. This makes the demo reproducible without claiming route or market accuracy.

## ADR 8 — Keep application tables outside the Supabase Data API

Store ride-hailing data in a dedicated `ride_hailing` schema and omit it from both local and remote Supabase Data API schema lists. Enable and force RLS on every application table without adding browser-facing policies or grants. The Go service connects server-side and uses qualified SQL. This keeps the backend as the sole business API while still using Supabase-hosted PostgreSQL; the service connection remains privileged and must stay in the ignored server environment.

## ADR 9 — Scoped idempotency and database constraints protect retries

Require an idempotency key per rider and fingerprint the normalized request. Replays identify the same ride; a changed request under the same key is a conflict. A separate unique open-ride constraint rejects a second booking even when a client invents a new key. The trade-off is a small amount of idempotency state and explicit conflict handling in the UI.
