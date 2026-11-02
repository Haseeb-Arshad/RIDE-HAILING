# Current limitations and verification

This repository is an assessment demo, not a production ride-hailing service. Its connected Supabase flow was verified on 2026-11-02; the remaining boundaries below are still important.

## Verified against the connected project

- All application migrations were applied, the seed linked three Supabase Auth users to app profiles, and seven suggested Lahore locations plus two driver profiles were created.
- The full Go PostgreSQL integration suite passed against the project's database, including transactional ride lifecycle, retry and race cases, custom map-pin persistence, and fuel-based fare snapshots. The integration fixtures clean their temporary rows.
- Both Playwright browser tests passed with real Supabase sign-in and Google Maps pin selection. The rider-to-driver journey verified availability changes, fare quote, booking, acceptance of the matching ride, and completion on both screens. It uses dedicated `example.test` accounts and leaves completed demo ride records in Supabase; before each run it completes a prior driver assignment and cancels the dedicated rider's leftover open booking.
- The private `ride_hailing` schema was checked through the Supabase Data API: it returned `PGRST106` because the schema is not exposed. PostgreSQL also reported no `USAGE` grants for `anon`, `authenticated`, or `service_role`.
- Frontend tests (23), lint, type checking, production build, Go unit tests, and `go vet` passed. The current local ride journey was also replayed through `localhost:3000` after its Next.js rewrite was pointed at the refreshed Go API. Hosted GitHub Actions checks are tracked separately and do not connect to Supabase.

These are point-in-time checks for the current local configuration. Re-run them after relevant code, schema, Auth, or project setting changes. The supplied five-page assessment is now mapped in [Requirements](REQUIREMENTS.md), with prepared answers in [Technical Assessment Response](TECHNICAL_ASSESSMENT_RESPONSE.md).

## Product and operational boundaries

- Riders can choose custom pickup and destination pins within the Lahore service bounds; the configured Google Maps browser key renders the map and the API persists those pins with the ride. There is no free-form Places search, road routing, live GPS, GPS freshness, or live dispatch. Distance uses a Haversine estimate with a configurable road factor, and the fuel-based fare uses a manually maintained rate card. Fare and ETA remain illustrative.
- There is one rider-facing booking and one active assignment per ride. Reassignment, offer expiry, payments, refunds, notifications, public registration, and OAuth are not implemented.
- Active ride views poll. There is no WebSocket, SSE, Redis presence store, or event broker.
- Supabase access tokens can remain valid until expiry after the browser signs out. Immediate revocation is not implemented.
- There is no hosted deployment, production monitoring, load test, backup/restore proof, or independent multi-instance database race run.

See [Requirements](REQUIREMENTS.md), [Architecture](ARCHITECTURE.md), and [Contract](CONTRACT.md) for the requirement map and current implementation details.
