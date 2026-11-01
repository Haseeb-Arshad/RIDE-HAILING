# Current limitations

This repository is an assessment demo. A working local build or passing unit test does not establish that the external Supabase project is configured or that the live ride journey has passed.

## Verification still depends on project setup

- The Supabase migration and seed require the project's database URL in the ignored root `.env` and existing confirmed Supabase Auth accounts for the demo emails.
- The Go JWT verifier accepts asymmetric ES256 and RS256 signing keys from the project's public JWKS. It does not accept legacy HS256 shared-secret tokens. The project must be configured to issue one of the supported asymmetric algorithms.
- PostgreSQL race tests require `TEST_DATABASE_URL` pointed at a migrated, isolated database. Without it, those integration tests skip; Go unit tests do not prove database locking.
- The Playwright journey requires a migrated and seeded database, public browser Auth settings, dedicated rider/driver credentials, and an installed browser. It has not passed until run against those services.
- The original five-page technical assessment was not supplied. Requirements that exist only in that source cannot be mapped or verified here.

## Product and operational boundaries

- Only fixed Lahore demo locations and static driver coordinates are available. Distance, fare, and ETA are illustrative; there is no map, road route, GPS freshness, or live dispatch.
- There is one rider-facing booking and one active assignment per ride. Reassignment, offer expiry, payments, refunds, notifications, public registration, and OAuth are not implemented.
- Active ride views poll. There is no WebSocket, SSE, Redis presence store, or event broker.
- Supabase access tokens can remain valid until their expiry after the browser signs out. Immediate revocation is not implemented.
- The private `ride_hailing` schema relies on server-side Go access, forced RLS, and remote project configuration that keeps the schema out of the Data API exposure list. The remote settings still need confirmation.
- There is no hosted deployment, production monitoring, load test, backup/restore proof, or independent multi-instance database race run.

See [Requirements](REQUIREMENTS.md), [Architecture](ARCHITECTURE.md), and [Contract](CONTRACT.md) for the requirement map and current implementation details.
