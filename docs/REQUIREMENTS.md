# Requirements and acceptance map

## Scope and source boundary

The target is a runnable, tested Lahore ride-booking assessment implemented as one repository with `apps/web` (Next.js and TypeScript), `apps/api` (Go and Chi), and PostgreSQL. The supplied execution plan sets a six-to-eight-hour ceiling, prioritizes transactional correctness over infrastructure breadth, and defines ten gated stages. This map now includes both that plan and the supplied five-page Technical Assessment.

The detailed requirement-by-requirement review and responses to all eight architecture questions are in [Technical Assessment Response](TECHNICAL_ASSESSMENT_RESPONSE.md). The source PDF is treated as evaluation criteria; project-specific operating decisions, including separate services and no Docker, remain as documented in the README.

## Current verification snapshot

On 2026-11-02, the configured Supabase project was migrated and seeded; the PostgreSQL integration suite and real Supabase Auth rider/driver Playwright journey passed. The web unit tests, lint, production build, Go test suite, and GitHub API/web checks also passed. This is point-in-time evidence for the current demo, not a production-readiness sign-off.

## MUST BUILD

| Requirement | API and persistence evidence | Frontend evidence | Verification evidence | Boundary / limitation |
|---|---|---|---|---|
| One monorepo, two apps, one business backend | Go/Chi owns auth, quotes, rides, matching, and lifecycle; PostgreSQL owns durable state | Next.js calls same-origin `/api/v1/*`; server rewrite forwards to Go | Separate-process startup, API health/readiness, web build and proxy smoke | Modular monolith; no second ride backend in Next.js |
| Reproducible local runtime | Versioned SQL migrations and repeatable seeds; environment configuration; Supabase-hosted PostgreSQL | Next.js and Go run as separate local processes; same-origin rewrite connects the browser to Go | Clean startup, migration and seed against the supplied database, health/readiness checks | No local PostgreSQL or Docker runtime; no hosted app deployment or production claim |
| Supabase Auth rider and two drivers with role isolation | Supabase Auth users map by verified JWT subject to database profiles; roles come from PostgreSQL | Separate rider and driver screens; SSR cookie refresh; API uses explicit bearer tokens | JWT signature/issuer/audience/expiry checks; profile mapping, role, ownership, Origin tests | No public signup or OAuth providers; access tokens remain valid until configured expiry |
| Lahore location selection and quote | Seeded stops plus validated custom pins inside the service bounds; server computes approximate distance, fuel-based fare, ETA, and fare snapshot | Google Maps pin selection anywhere within the service bounds, editable labels, Economy/Comfort, quote loading/error and no-driver ETA state | Both-category pricing, invalid/out-of-bounds points, rate changes, fare snapshot/persistence, no-eligible-driver tests, map click component test, and live browser check | Approximate Haversine × road factor; no free-form Places search, traffic, or road routing; fuel rates are maintained manually |
| Safe ride creation and retry | Persist quote snapshot and integer-paisa amount; rider-scoped idempotency key plus canonical request hash; unique open-ride constraint | Pending state blocks duplicate clicks; retry reuses the same key | Same-key/same-payload sequential and concurrent tests; key conflict; second open booking | Estimate is illustrative; client never chooses fare or driver |
| Ride status, refresh, and permitted cancellation | Persisted `requested → accepted/completed/cancelled` lifecycle; ownership checks; terminal states | Rider ride page polls while active, stops at terminal state, supports permitted cancellation | Success, forbidden/hidden resource, invalid transitions, cancel/completion races | Polling every 2–3 seconds; no WebSockets |
| Driver availability and eligible requests | Driver status; city/category/radius matching; bounded, stable request listing | Availability control, empty state, request list, accept feedback | Offline, wrong category, out-of-radius, busy and successful cases | Static demo driver positions; no GPS heartbeat |
| Race-safe assignment and release | Explicit PostgreSQL transaction and row locks; unique ride assignment; partial unique active assignment per driver; deferred SQL consistency triggers verify ride, assignment, and driver state together | Winning assignment view; conflict refreshes/removes stale request; completion returns driver to available | Real-PostgreSQL two-driver/one-ride race, one-driver/two-ride race, accept-vs-cancel, complete-vs-cancel, and invalid direct SQL write checks; assert final DB rows | A Go mutex or mock DB is not evidence of cross-instance safety |
| Complete browser ride journey | Go verifies real Supabase JWTs and PostgreSQL enforces ride state | Playwright uses separate rider and driver contexts for login, quote, booking, accept, and completion | Run `npm run test:e2e` against the configured Supabase database and record actual result | No mocked browser journey counts as database or Auth proof |
| Consistent validation and errors | Parameterized SQL, request IDs, bounded bodies/timeouts, consistent JSON error envelope | Clear pending, validation, 401, 404, 409, and unavailable-server states | Malformed JSON, invalid fields, auth/role/ownership and error-shape tests | Error messages must not disclose credentials or concealed resources |
| Documentation reflects actual behavior | Contract and OpenAPI agree with implemented routes and schemas | README includes real local steps and demo accounts when available | Fresh-clone audit compares docs, routes, migrations, tests, and scripts | Do not document planned features as implemented |

## NICE TO HAVE

| Item | Acceptance when attempted | Time-box treatment |
|---|---|---|
| Two independent API processes in a concurrency smoke | Both instances use the same PostgreSQL database; invariants still hold | Attempt after core real-database race tests |
| Minimal CI workflow | Go checks, web checks, and PostgreSQL-backed integration job use documented commands | Add only after a clean local path exists |
| Visual polish and animation | Accessible, responsive states remain readable and operable | Cut before transaction correctness or tests |

## FUTURE ONLY

The assessment demo has Google Maps with custom rider-selected pins within Lahore service bounds and a transparent fuel-based fare estimate. It has no free-form Places search, road routing, fresh GPS tracking, spatial dispatch, reassignment/offers, payment/refunds, notifications, signup/OAuth, Redis, Kafka, WebSockets, AI services, automatic fuel-price fetching, or hosted production deployment. These may be discussed as future evolution; they must not appear as current capabilities.

## Acceptance and test map

| Test group | Required cases | Evidence target |
|---|---|---|
| PostgreSQL ride creation | Success; identical endpoints; unsupported category; invalid UUID; missing idempotency key; same key/same payload sequential and concurrent; same key/different payload; second open ride | API integration tests against actual PostgreSQL; database row counts and constraints inspected |
| Quote and matching | Both categories; no-driver ETA; eligible category/city/radius; offline, wrong-category, and out-of-radius drivers | Pricing/matching unit tests plus DB-backed service/API tests where state is read |
| Authorization | Rider cannot see another rider’s ride; wrong driver cannot complete; forged owner/driver/fare/status inputs ignored or rejected; role enforcement | HTTP tests covering status and error envelope |
| Lifecycle and races | Successful accept; two drivers race for one ride; one driver races for two rides; accept-vs-cancel; complete-vs-cancel; invalid terminal transitions; repeat action behavior; assignment release | PostgreSQL integration suite passed against the configured Supabase database on 2026-11-02, including lifecycle/concurrency cases and fixture cleanup |
| Auth and authorization | Valid/invalid/expired signature, issuer, audience, unknown profile, role, ownership, Origin, metadata not trusted | Go JWT and HTTP tests passed; real Supabase Auth sign-in and profile mapping passed in the live browser journey on 2026-11-02 |
| Frontend behavior | Login errors; quote loading/error/no ETA; request pending, duplicate suppression and stable retry key; 409 conflict; empty driver list; cancellation; accepted/completed terminal display; 401 expiry | Vitest and React Testing Library with API mocks; mocks prove UI behavior only |
| Runtime and submission | Separate Go/Next process startup, Supabase migration/seed, liveness/readiness, same-origin proxy, frontend and backend build, clean restart/persistence | Exact commands and outcomes recorded in README/final audit; no unrun check described as passing |

This map has been reconciled against the supplied assessment. Keep it and the response document aligned if scope or implementation changes.
