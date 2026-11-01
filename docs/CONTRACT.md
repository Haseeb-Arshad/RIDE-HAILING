# API and domain contract

**Status:** Implementation source of truth. The Supabase migrations, seed, PostgreSQL integration suite, and real browser journey were run successfully against the configured project on 2026-11-02. Re-run them after relevant code, schema, or Auth changes. Update this document and `docs/openapi.yaml` together if a correctness-driven decision changes.

## 1. Domain vocabulary

- Roles: `rider`, `driver`. There is no admin role or public registration.
- Ride categories: `economy`, `comfort`.
- Ride statuses: `requested`, `accepted`, `completed`, `cancelled`.
- Driver statuses: `offline`, `available`, `busy`.
- Assignment statuses: `active`, `completed`, `cancelled`. The demo creates at most one assignment row per ride and never reassigns a ride.
- City: `Lahore` only. Location records and demo driver coordinates are seeded, fixed fixtures.
- IDs are UUIDs. Timestamps are UTC RFC 3339 values.

### Ride transition matrix

| Current | Action | Actor | Next | Side effects |
|---|---|---|---|---|
| requested | accept | Eligible available driver | accepted | Create one active assignment; set driver to busy, in the same transaction |
| requested | cancel | Owning rider | cancelled | No assignment; rider’s open-ride slot is released |
| accepted | complete | Assigned driver | completed | Mark assignment completed; set driver to available, atomically |
| accepted | cancel | Owning rider | cancelled | Mark assignment cancelled; set assigned driver to available, atomically |
| completed / cancelled | any lifecycle mutation | Any actor | No transition | Return 409, except the identical authorized repeat action described below |

No other transition is valid. `busy` is system-managed and cannot be selected by a driver. A driver can request `offline` or `available` only when no active assignment exists.

## 2. Authorization and Supabase Auth

Supabase Auth owns email/password credentials, refresh sessions, and browser sign-in. The Next.js browser/server helpers use `@supabase/ssr`; `src/proxy.ts` calls `getClaims()` to refresh the cookie-backed session. The browser sends the current access token explicitly as `Authorization: Bearer ...` to the same-origin Go API. API fetches omit Supabase cookies, so the API does not authenticate from ambient cookies.

Go accepts ES256 or RS256 access tokens and validates signature using the configured project's public JWKS, exact issuer, `authenticated` audience, expiry, and UUID subject. It resolves `sub` through `ride_hailing.users.supabase_auth_id`. The profile table supplies the app user ID, email, display name, and role. Go never authorizes from `user_metadata`, `app_metadata`, email alone, or client-supplied identity. The project's Auth signing key must be asymmetric; no Supabase secret API key or signing secret is required by this service.

The seed links three already-created Supabase Auth accounts to rider/driver profiles by normalized email. It does not create Auth users or set their passwords. Go exposes only `GET /api/v1/auth/me`; Supabase Auth handles sign-in and sign-out in the browser.

All unsafe API requests require an `Origin` matching the configured application origin. Writes do not require a CSRF token because authentication uses an explicit bearer header that browsers do not attach automatically. The browser calls same-origin `/api/v1/...`; Next.js rewrites requests server-side to Go. The API origin, database URL, and demo passwords remain server-side; only the Supabase publishable key may use a `NEXT_PUBLIC_` variable.

Authorization rules:

- Rider routes require role `rider`; driver routes require role `driver`.
- A rider can read/cancel only their own ride. A driver can read/complete only a ride assigned to them.
- A non-owner/non-assigned actor receives a concealed 404 for ride reads and cancellation/completion; role failures return 403.
- A driver may attempt acceptance only while the ride is requested and the driver is eligible. Ineligibility or a competing winner is a 409 conflict.
- A driver can read only their own persisted availability status; the dashboard reloads it from the API instead of inferring `offline` or `available` from the request list.
- `/driver/requests` returns only eligible requested rides and omits rider identity/contact details.

## 3. Invariants and transaction rules

1. At most one open ride (`requested` or `accepted`) exists per rider.
2. A ride has at most one assignment row.
3. A driver has at most one active assignment.
4. A driver is `busy` exactly while holding an active assignment. Acceptance and terminal release update ride, assignment, and driver rows in one PostgreSQL transaction.
5. Ride and driver state are rechecked after acquiring locks. Acceptance locks the ride row first and then the driver row, in a consistent order, and commits assignment plus both status changes together.
6. PostgreSQL constraints backstop one-open-ride, one-assignment-per-ride, one-active-assignment-per-driver, and idempotency-key uniqueness. A process mutex is not a concurrency guarantee.
7. Application tables live in the private `ride_hailing` schema, omitted from this repository's Supabase Data API schema list. On 2026-11-02, a request to the project's Data API for this schema returned `PGRST106` (not exposed), and PostgreSQL reported no schema usage grants for `anon`, `authenticated`, or `service_role`. RLS is enabled and forced on every table with no browser-facing policies or grants. Recheck the remote setting if project configuration changes. Go uses server-side qualified SQL, maps verified Supabase subjects to application profiles, and enforces role/ownership rules.
8. Quote inputs contain only pickup ID, destination ID, and category. Creation recomputes distance/fare on the server and persists the quote snapshot. The client cannot select a driver or submit a trusted price.
9. Every create request has a rider-scoped `Idempotency-Key` (1–128 visible ASCII characters) and canonical request fingerprint over the normalized pickup UUID, destination UUID, and category. Same rider/key and same fingerprint returns the same ride; the response is HTTP 200 and reflects that ride’s current persisted state. Same key with a different fingerprint is 409. A new key while the rider already has an open ride is 409.

Acceptance transaction outline: begin; lock ride `FOR UPDATE`; lock driver `FOR UPDATE`; recheck ride state, driver availability, category/city/radius, and active assignment; insert the unique assignment; update ride and driver; commit. Any failed condition rolls back without partial state. Cancellation/completion locks the ride then its assigned driver and updates all affected rows atomically.

## 4. Estimates and matching

Locations and driver positions are fixed Lahore demo coordinates. Distance is Haversine straight-line distance multiplied by `1.3`, rounded to an integer meter as a rough road-distance proxy. It is not route data.

| Category | Base fare | Per estimated kilometer |
|---|---:|---:|
| economy | PKR 150 (15,000 paisa) | PKR 60 (6,000 paisa) |
| comfort | PKR 250 (25,000 paisa) | PKR 90 (9,000 paisa) |

Fare formula uses integer paisa: `base_paisa + ceil(estimated_distance_meters * per_km_paisa / 1000)`. Currency is always `PKR`; no floating-point currency is stored. These values are illustrative demo numbers, not current or official market fares.

An eligible driver is in Lahore, `available`, category-compatible, and within the configurable demo radius (default 20 km) of the pickup using the same approximate distance method. Quote ETA uses the nearest eligible driver at an illustrative 25 km/h and a two-minute minimum: `max(2, ceil(estimated_driver_distance_meters * 60 / 25000))`. If no eligible driver exists, `pickup_eta_minutes` is `null`. ETA is an estimate, not a dispatch promise.

## 5. HTTP conventions

- JSON request and response bodies use UTF-8 and `application/json` unless the response is `204`.
- Responses include `X-Request-ID`; the same ID appears in an error envelope. Logs are structured and omit passwords, bearer tokens, refresh tokens, API keys, and database URLs.
- Error shape: `{ "error": { "code": "...", "message": "...", "request_id": "..." } }`.
- `400`: malformed JSON or malformed request syntax. `401`: missing, invalid, or expired Supabase access token. `403`: profile not enabled, wrong role, or Origin. `404`: unknown ID or concealed non-owned resource. `409`: state, eligibility, uniqueness, or idempotency conflict. `413`: body too large. `422`: valid JSON with invalid semantic fields. `500`: unexpected error with a generic message. `503`: readiness, JWKS, or authentication storage unavailable.
- Use `401` for unauthenticated requests, `403` for insufficient rights or an unlinked profile, `404` for concealed non-owned resources, `409` for state conflicts, and `422` for valid-JSON field validation.
- Repeated accept by the already assigned driver returns `200` with the current ride. Repeated cancel by the owning rider after cancellation and repeated complete by the assigned driver after completion return `200` with the current ride. Other invalid transitions return `409`.

## 6. Canonical routes

| Method and path | Actor | Request | Success |
|---|---|---|---|
| `GET /healthz` | Probe | None | `200` liveness |
| `GET /readyz` | Probe | None | `200` if PostgreSQL is reachable; otherwise `503` |
| `GET /api/v1/auth/me` | Authenticated | `Authorization: Bearer <Supabase access token>` | `200` current application user profile |
| `GET /api/v1/locations` | Authenticated | None | `200` seeded Lahore locations |
| `POST /api/v1/quotes` | Rider | `{pickup_location_id,destination_location_id,category}` | `200` server-generated estimate |
| `POST /api/v1/rides` | Rider | Same quote request + `Idempotency-Key` | `201` new ride; `200` idempotent replay |
| `GET /api/v1/rides/current` | Rider | None | `200` current open ride or `{ride:null}` |
| `GET /api/v1/rides/{id}` | Owning rider or assigned driver | UUID path | `200` authorized ride |
| `GET /api/v1/driver/requests` | Driver | Optional `limit` (default 25, max 50) and opaque `cursor` | `200` bounded eligible requests, oldest first |
| `GET /api/v1/driver/assignment` | Driver | None | `200` current assignment or `{assignment:null}` |
| `GET /api/v1/drivers/me/availability` | Driver | None | `200` current `offline`, `available`, or `busy` status |
| `PATCH /api/v1/drivers/me/availability` | Driver | `{status:offline\|available}` | `200` updated status; busy cannot be set or cleared manually |
| `POST /api/v1/rides/{id}/accept` | Eligible driver | No body | `200` assigned/current ride; conflict if another driver wins |
| `POST /api/v1/rides/{id}/cancel` | Owning rider | No body | `200` cancelled/current ride |
| `POST /api/v1/rides/{id}/complete` | Assigned driver | No body | `200` completed/current ride |

### Request and response shapes

Quote and create request:

```json
{
  "pickup_location_id": "11111111-1111-4111-8111-111111111111",
  "destination_location_id": "22222222-2222-4222-8222-222222222222",
  "category": "economy"
}
```

Quote response:

```json
{
  "estimated_fare_paisa": 28500,
  "estimated_distance_meters": 2250,
  "pickup_eta_minutes": 7,
  "currency": "PKR",
  "approximate": true
}
```

`pickup_eta_minutes` is integer or `null`. A ride resource contains its UUID, owner identity restricted as above, pickup and destination snapshots, category, persisted fare/distance/currency snapshot, status, creation/update times, and assignment details only after acceptance. Driver request summaries omit rider identity/contact data. Unknown enum values and extra authority-bearing client fields are rejected or ignored; they never override server state.

## 8. Verification status and remaining gates

- Reconcile the original five-page assessment; it was not included with the supplied execution plan, so criteria found only there are unknown.
- The configured Supabase project accepted a real browser sign-in and Go JWT validation; both migrations, seed, and the PostgreSQL integration suite passed on 2026-11-02.
- The two-context Playwright rider/driver journey passed on 2026-11-02. Its dedicated demo accounts and completed ride records remain in the connected project for repeatable manual testing.
- These checks do not cover hosted deployment, production monitoring, backup/restore, load testing, live location, routing, payments, or notifications.
