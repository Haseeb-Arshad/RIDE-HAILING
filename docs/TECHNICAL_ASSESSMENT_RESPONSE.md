# Lahore Ride-Hailing

## Technical Assessment Response and Implementation Review

**Assessment:** Lead Full Stack / Backend Engineer take-home

**Review date:** 2 November 2026

### Executive summary

The repository implements the core ride-booking assessment as a runnable Lahore demo: a responsive Next.js and TypeScript client, a Go API, Supabase Auth, and durable PostgreSQL state. Riders can request Economy or Comfort trips, review server-calculated estimates, follow ride status, and cancel when allowed. Drivers can control availability, accept eligible rides, and complete assigned trips.

The central reliability requirement is handled in PostgreSQL transactions and constraints, with coverage for competing assignments, retries, and lifecycle races. The assessment's real-map requirement is optional; this repository has a conditional Google Maps JavaScript integration and a clear setup state, but the current local web environment has no Google Maps browser key, so live map tiles were not verified. This is an assessment demo, not a production ride-hailing service.

### How this review treats the attachment

The supplied PDF is the assessment rubric for this review. Its submission and engineering requirements are treated as criteria to evaluate and answer. The project's direct operating instruction remains to run the Next.js and Go services separately and use the supplied Supabase database; the PDF's alternative mention of Docker does not require a Docker workflow.

### Requirement-by-requirement review

| Assessment area | Result | Evidence and boundary |
|---|---|---|
| Lahore booking flow | Implemented | Seven fixed Lahore locations; pickup and destination selection; Economy and Comfort; server-side PKR fare, approximate distance, and pickup ETA; ride confirmation, active status, terminal status, and permitted cancellation. Estimates are illustrative. |
| Responsive UI and states | Implemented | Rider and driver workspaces include loading, validation, success, empty, conflict, and error handling. Component tests cover quote loading/failure, duplicate-submit suppression, stable retry, and ride states. |
| Backend API | Implemented | Go/Chi exposes the requested create, read, accept, cancel, and complete operations under `/api/v1`, plus quote, current-ride, locations, driver requests, assignment, availability, health, and readiness routes. See [OpenAPI](openapi.yaml) and [API contract](CONTRACT.md). |
| Matching and lifecycle | Implemented | Eligibility checks availability, city, category, and approximate pickup radius. Valid transitions are enforced; invalid transitions return conflicts. Driver occupancy is released transactionally after cancellation or completion. |
| Durable data model | Implemented | Versioned Supabase migrations create users, driver profiles/categories, locations, rides, assignments, and idempotency records in the private `ride_hailing` schema. Ride state is not held only in memory. |
| Concurrency and retries | Implemented | PostgreSQL row locks, a consistent ride-then-driver lock order, unique assignment/open-ride/idempotency constraints, and request fingerprints protect state across API processes. The live database race suite is the decisive verification for these guarantees. |
| Security and configuration | Implemented for the demo | Supabase Auth owns credentials; Go verifies asymmetric JWT signatures through public JWKS and loads role/identity from PostgreSQL. The app checks ownership, role, Origin, and request shape. Local `.env` files are ignored; only example templates are tracked. |
| Local setup and operations | Implemented for development | The README documents separate Go and Next.js processes, migrations, seed, environment variables, health/readiness, and checks. Docker is not required. There is no hosted production deployment, production monitoring, load test, or backup-restore proof. |
| Maps | Partial / optional | The UI loads Google Maps JavaScript when `NEXT_PUBLIC_GOOGLE_MAPS_API_KEY` is configured and plots seeded stops and selected endpoints. In this checkout that key is absent, so the UI displays its setup placeholder. There is no free-form Places search, road-route calculation, or live driver GPS. The assessment explicitly permits a placeholder. |
| Documentation and submission | Implemented | README, SQL migrations, automated tests, OpenAPI, architecture/state diagrams, design decisions, limitations, and this response cover the requested submission material. AI assistance is disclosed below. |

Useful implementation evidence includes [booking form](../apps/web/src/components/booking-form.tsx), [ride page](../apps/web/src/app/rides/%5Bid%5D/page.tsx), [Go HTTP routes](../apps/api/internal/transport/http/router.go), [PostgreSQL ride repository](../apps/api/internal/rides/postgres.go), [core schema migration](../supabase/migrations/20261031150815_ride_hailing_core.sql), and [Auth identity migration](../supabase/migrations/20261101151954_supabase_auth_identity.sql). The database race cases are in [ride lifecycle integration tests](../apps/api/internal/rides/lifecycle_integration_test.go) and [idempotency integration tests](../apps/api/internal/rides/postgres_integration_test.go).

### Architecture questions

#### 1. Why this stack and architecture?

I chose Next.js and TypeScript for the rider and driver interfaces, including the same-origin API rewrite and Supabase's cookie-backed browser session. Go with Chi keeps business rules in one explicit API process, while `pgx` gives direct control over PostgreSQL transactions and row locks. Supabase Auth manages credentials and refresh sessions; Supabase PostgreSQL stores application profiles and ride state.

This modular monolith keeps pricing, authorization, matching, and lifecycle behavior in one place, and keeps the UI, API, schema, and contract reviewable in one repository. It also avoids adding distributed infrastructure before the core transaction rules need it. The trade-off is operating separate Node and Go processes and requiring PostgreSQL for the authoritative flow.

#### 2. How do you prevent two drivers from accepting the same ride?

Acceptance starts a PostgreSQL transaction, locks the requested ride row, then locks the acting driver row in a consistent order. After acquiring those locks, the API rechecks ride state, driver availability, city, category, radius, and active assignments. It inserts the assignment and updates the ride and driver together before commit. A unique assignment per ride and a partial unique index limiting each driver to one active assignment provide database-level backstops. A losing request receives a conflict; an in-process mutex is not relied upon.

#### 3. How do you handle retries and duplicate requests?

The browser creates an idempotency key for a booking intent, scopes its saved attempt to the signed-in user, and reuses the key after an uncertain response. The API fingerprints the normalized pickup, destination, and category and persists the key, fingerprint, and ride in the same transaction. Repeating the same request returns the original ride; reusing the key for different input returns a conflict. A unique open-ride constraint also prevents a second active booking under a different key.

#### 4. How does the frontend respond when an API request fails?

The interface shows pending states and disables duplicate actions while a request is in flight. Validation and API failures appear as actionable messages. On a `401`, the Supabase browser session is cleared; on a `409`, the rider or driver view reconciles with the current server state. If ride creation has an uncertain network result, the client retains the request key and checks the rider's current ride before offering a safe retry. Active rides are refreshed by polling, which stops at a terminal state.

#### 5. How would you scale to multiple cities and thousands of concurrent users?

Keep the Go API stateless behind a load balancer and scale instances horizontally. Keep city and ownership constraints in PostgreSQL, use a managed pooler, inspect query plans, and measure pool saturation, lock waits, and request latency before changing database topology. Add PostGIS when real coordinates and spatial queries replace the current seeded points and approximate distance. Give driver presence a freshness/expiry model before introducing a separate high-write presence store. No load test has yet established capacity for thousands of concurrent users.

#### 6. When would you introduce Redis, a message broker, or separate services?

Use Redis when fresh driver presence and nearby-driver lookup volume make database polling too costly or stale; presence should expire and reconcile back to PostgreSQL. Add a broker when independent consumers need durable ride events, replay, or backpressure. Write an outbox record in the same transaction as each ride transition and make consumers idempotent. Split services only when separate ownership, scaling, or fault isolation outweigh the operational and network-contract cost.

#### 7. How would you deploy, monitor, and roll back a faulty release?

Build immutable web and API artifacts in CI; run unit, contract, migration-compatibility, and PostgreSQL integration checks before release. Apply additive, backward-compatible migrations before deploying code that depends on them, and remove obsolete schema in a later release. Use managed secrets, HTTPS, backups, and tested restore procedures. Preserve the existing request IDs and structured logs, then add latency/error, authorization, pool, lock-wait, and ride-transition metrics with alerts. Roll back application artifacts to a compatible release; use a forward fix for a migration that has already changed durable data.

There is no hosted deployment or production monitoring in the current repository, so this is the proposed operating model rather than a claim about a running service.

#### 8. What are the three most important improvements before production?

1. **Real dispatch inputs and behavior:** replace fixed driver coordinates with authenticated, expiring location updates; add offer expiry and a dispatch loop; use road-aware routing and market-reviewed pricing.
2. **Production identity and abuse controls:** add controlled onboarding and account recovery, a clear revocation/key-rotation policy, rate limits, and a reviewed privacy/data-access model.
3. **Operational readiness:** deploy to managed hosting, add alerts and service metrics, exercise backup restoration and migration rollback procedures, and run representative load tests.

Payment capture and refunds should follow secure dispatch and operational foundations, not be presented as current capabilities.

### Verification record

#### Current local checks

- `go test -v ./... -count=1`: Go unit and HTTP tests passed. PostgreSQL-only cases were skipped in this isolated run because `TEST_DATABASE_URL` was explicitly removed.
- `go vet ./...`: passed.
- `npm test`: 19 tests across 4 files passed.
- `npm run lint`: passed.
- `npx tsc --noEmit`: passed.
- `npm run build`: passed; Next.js generated the production build and routes.
- `scripts/test-supabase-integration.ps1`: passed against the configured Supabase PostgreSQL project. This run exercised the migration-dependent seed/schema checks, Auth profile lookup, ride creation/replay, driver assignment, and PostgreSQL concurrency/lifecycle cases; test fixtures use rollback or cleanup.

#### Connected-provider evidence

The real Supabase Auth rider-to-driver Playwright journey passed on 2 November 2026, as recorded in the [README](../README.md), [requirements map](REQUIREMENTS.md), and [verification notes](LIMITATIONS.md). The PostgreSQL integration suite was also rerun successfully for this review. The Playwright journey creates completed demo ride rows and was not rerun here. Live Google Maps rendering was not verified because the local web environment has no browser key.

### AI-use disclosure

AI-assisted coding tools supported implementation and technical documentation. The claims in this response were checked against the repository, and the current local test, lint, type-check, and build results are listed above. Live provider behavior is described only where dated verification evidence exists.

### Scope limits

This implementation does not claim free-form address search, road routing, live GPS, dynamic dispatch or reassignment, payment processing, refunds, customer notifications, public signup, OAuth, real-time push updates, production deployment, load-test results, or backup/restore proof. Current fare, distance, ETA, driver positions, and Lahore locations are demo data and estimates. See [current limitations](LIMITATIONS.md) and [architecture notes](ARCHITECTURE.md) for the proposed growth path.
