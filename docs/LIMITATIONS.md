# Current limitations and verification

This repository is an assessment demo, not a production ride-hailing service. Its connected Supabase flow was verified on 2026-11-02; the remaining boundaries below are still important.

## Verified against the connected project

- Both application migrations were applied, the seed linked three Supabase Auth users to app profiles, and seven fixed Lahore locations plus two driver profiles were created.
- PostgreSQL integration tests passed against the project's database, including transactional ride lifecycle, retry, and race cases. The tests clean their temporary rows.
- The Playwright rider-to-driver journey passed with real Supabase sign-in, quote, booking, acceptance, and completion. It uses dedicated `example.test` accounts and leaves completed demo ride records in Supabase. The test completes any prior active assignment on the dedicated driver account before starting.
- The private `ride_hailing` schema was checked through the Supabase Data API: it returned `PGRST106` because the schema is not exposed. PostgreSQL also reported no `USAGE` grants for `anon`, `authenticated`, or `service_role`.
- Web lint, 19 frontend tests, the production build, Go tests, and the GitHub API/web checks passed. GitHub Actions does not connect to Supabase; its checks are separate from the PostgreSQL integration evidence.

These are point-in-time checks for the current local configuration. Re-run them after relevant code, schema, Auth, or project setting changes. The supplied five-page assessment is now mapped in [Requirements](REQUIREMENTS.md), with prepared answers in [Technical Assessment Response](TECHNICAL_ASSESSMENT_RESPONSE.md).

## Product and operational boundaries

- The UI has fixed Lahore demo locations and static driver coordinates. A Google Maps JavaScript preview loads only when a restricted browser key is configured; no key is present in the current local environment, so live provider rendering is unverified. There is no free-form Places search, road routing, live GPS, GPS freshness, or live dispatch. Fare, distance, and ETA are illustrative.
- There is one rider-facing booking and one active assignment per ride. Reassignment, offer expiry, payments, refunds, notifications, public registration, and OAuth are not implemented.
- Active ride views poll. There is no WebSocket, SSE, Redis presence store, or event broker.
- Supabase access tokens can remain valid until expiry after the browser signs out. Immediate revocation is not implemented.
- There is no hosted deployment, production monitoring, load test, backup/restore proof, or independent multi-instance database race run.

See [Requirements](REQUIREMENTS.md), [Architecture](ARCHITECTURE.md), and [Contract](CONTRACT.md) for the requirement map and current implementation details.
