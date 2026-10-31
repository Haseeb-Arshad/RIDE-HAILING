# Requirements and acceptance map

## Scope and source boundary

The target is a runnable, tested Lahore ride-booking assessment implemented as one repository with `apps/web` (Next.js and TypeScript), `apps/api` (Go and Chi), and PostgreSQL. The supplied execution plan sets a six-to-eight-hour ceiling, prioritizes transactional correctness over infrastructure breadth, and defines ten gated stages. This document maps the requirements present in that plan.

The plan refers to a separate five-page “Technical Assessment — Lead Full Stack / Backend Engineer.” That source document was not included with the pasted plan, so this first-pass map cannot independently confirm criteria that appear only in the original assessment. Reconcile this map against that document before treating the Stage 1 acceptance gate as complete.

## MUST BUILD

| Requirement | API and persistence evidence | Frontend evidence | Verification evidence | Boundary / limitation |
|---|---|---|---|---|
| One monorepo, two apps, one business backend | Go/Chi owns auth, quotes, rides, matching, and lifecycle; PostgreSQL owns durable state | Next.js calls same-origin `/api/v1/*`; server rewrite forwards to Go | Separate-process startup, API health/readiness, web build and proxy smoke | Modular monolith; no second ride backend in Next.js |
| Reproducible local runtime | Versioned SQL migrations and repeatable seeds; environment configuration; Supabase-hosted PostgreSQL | Next.js and Go run as separate local processes; same-origin rewrite connects the browser to Go | Clean startup, migration and seed against the supplied database, health/readiness checks | No local PostgreSQL or Docker runtime; no hosted app deployment or production claim |
| Seeded rider and two drivers with role isolation | Hashed demo passwords, server-side sessions, role checks on every route | Separate rider and driver screens; separate browser contexts for demonstration | Login, expiry/revocation, missing cookie, role, ownership, CSRF and Origin tests | No signup, OAuth, or external identity provider |
| Lahore location selection and quote | Seeded location records; server validates category and distinct locations; fare/ETA computed server-side | Location selection, Economy/Comfort choice, quote loading/error and no-driver ETA state | Both-category pricing, invalid IDs/locations, and no-eligible-driver tests | Fixed demo coordinates; no map or road-routing provider |
| Safe ride creation and retry | Persist quote snapshot and integer-paisa amount; rider-scoped idempotency key plus canonical request hash; unique open-ride constraint | Pending state blocks duplicate clicks; retry reuses the same key | Same-key/same-payload sequential and concurrent tests; key conflict; second open booking | Estimate is illustrative; client never chooses fare or driver |
| Ride status, refresh, and permitted cancellation | Persisted `requested → accepted/completed/cancelled` lifecycle; ownership checks; terminal states | Rider ride page polls while active, stops at terminal state, supports permitted cancellation | Success, forbidden/hidden resource, invalid transitions, cancel/completion races | Polling every 2–3 seconds; no WebSockets |
| Driver availability and eligible requests | Driver status; city/category/radius matching; bounded, stable request listing | Availability control, empty state, request list, accept feedback | Offline, wrong category, out-of-radius, busy and successful cases | Static demo driver positions; no GPS heartbeat |
| Race-safe assignment and release | Explicit PostgreSQL transaction and row locks; unique ride assignment; partial unique active assignment per driver; update ride, assignment, and driver together | Winning assignment view; conflict refreshes/removes stale request; completion returns driver to available | Real-PostgreSQL two-driver/one-ride race, one-driver/two-ride race, accept-vs-cancel and complete-vs-cancel; assert final DB rows | A Go mutex or mock DB is not evidence of cross-instance safety |
| Consistent validation and errors | Parameterized SQL, request IDs, bounded bodies/timeouts, consistent JSON error envelope | Clear pending, validation, 401, 404, 409, and unavailable-server states | Malformed JSON, invalid fields, auth/role/ownership and error-shape tests | Error messages must not disclose credentials or concealed resources |
| Documentation reflects actual behavior | Contract and OpenAPI agree with implemented routes and schemas | README includes real local steps and demo accounts when available | Fresh-clone audit compares docs, routes, migrations, tests, and scripts | Do not document planned features as implemented |

## NICE TO HAVE

| Item | Acceptance when attempted | Time-box treatment |
|---|---|---|
| Playwright end-to-end browser journey | Rider login → quote → request → separate driver login → accept → rider status → complete | The plan calls browser E2E ideal and includes it in hardening; if the eight-hour ceiling prevents it, mark it NOT RUN and retain backend and component evidence |
| Two independent API processes in a concurrency smoke | Both instances use the same PostgreSQL database; invariants still hold | Attempt after core real-database race tests |
| Minimal CI workflow | Go checks, web checks, and PostgreSQL-backed integration job use documented commands | Add only after a clean local path exists |
| Visual polish and animation | Accessible, responsive states remain readable and operable | Cut before transaction correctness or tests |

## FUTURE ONLY

The assessment demo does not implement real maps/routing, fresh GPS tracking, spatial dispatch, reassignment/offers, payment/refunds, notifications, signup/OAuth, Redis, Kafka, WebSockets, AI services, or a hosted production deployment. These may be discussed as future evolution; they must not appear as current capabilities.

## Acceptance and test map

| Test group | Required cases | Evidence target |
|---|---|---|
| PostgreSQL ride creation | Success; identical endpoints; unsupported category; invalid UUID; missing idempotency key; same key/same payload sequential and concurrent; same key/different payload; second open ride | API integration tests against actual PostgreSQL; database row counts and constraints inspected |
| Quote and matching | Both categories; no-driver ETA; eligible category/city/radius; offline, wrong-category, and out-of-radius drivers | Pricing/matching unit tests plus DB-backed service/API tests where state is read |
| Authorization | Rider cannot see another rider’s ride; wrong driver cannot complete; forged owner/driver/fare/status inputs ignored or rejected; role enforcement | HTTP tests covering status and error envelope |
| Lifecycle and races | Successful accept; two drivers race for one ride; one driver races for two rides; accept-vs-cancel; complete-vs-cancel; invalid terminal transitions; repeat action behavior; assignment release | PostgreSQL concurrency tests with barriers/coordination, followed by invariant queries |
| Session security | Expiry, revocation/logout, missing/invalid cookie, CSRF token, Origin validation | Auth/API integration tests |
| Frontend behavior | Login errors; quote loading/error/no ETA; request pending, duplicate suppression and stable retry key; 409 conflict; empty driver list; cancellation; accepted/completed terminal display; 401 expiry | Vitest and React Testing Library with API mocks; mocks prove UI behavior only |
| Runtime and submission | Separate Go/Next process startup, later Supabase migration/seed, liveness/readiness, same-origin proxy, frontend and backend build, clean restart/persistence | Exact commands and outcomes recorded in README/final audit; no unrun check described as passing |

The original assessment criteria and the plan’s requirement matrix remain the source of truth. Add any source-only criteria to this map before implementing later stages.
