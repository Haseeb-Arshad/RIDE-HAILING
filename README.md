# Lahore Ride Hailing

A focused ride-booking demo for Lahore. The Next.js app presents rider and driver journeys, the Go API owns business behavior, Supabase Auth manages sign-in, and Supabase PostgreSQL stores profiles and ride state.

## Project status

The connected demo was verified against the supplied Supabase project on 2026-11-02. Both application migrations were applied, the seed linked three Auth accounts and added seven Lahore locations plus two driver profiles, the PostgreSQL integration suite passed, and the Playwright rider-to-driver journey passed. Web lint, 19 frontend tests, the production build, Go tests, and the GitHub API/web checks also passed.

These checks prove the current demo flow against this project configuration; they do not establish production readiness. Re-run the database integration and browser checks after relevant code, schema, or Auth configuration changes. Local unit and UI tests can run without a database. GitHub Actions does not connect to Supabase, so its green checks are not a substitute for the PostgreSQL integration run.

The [technical assessment response](docs/TECHNICAL_ASSESSMENT_RESPONSE.md) reconciles the repository with the supplied five-page assessment, answers its architecture questions, and distinguishes current demo behavior from production work.

## Architecture

- `apps/web`: Next.js and TypeScript, with `@supabase/ssr` browser/server helpers and `src/proxy.ts` session refresh.
- `apps/api`: Go and Chi own quotes, matching, authorization, ride creation, assignment, cancellation, and completion.
- PostgreSQL: durable ride state, profile role mapping, idempotency, and concurrency constraints.
- Supabase Auth: email/password sign-in and token refresh. The Go API validates ES256 or RS256 access-token signatures through the project's public JWKS, then resolves the user's role and display name from `ride_hailing.users`. It does not trust token user metadata for authorization.

The browser calls the Go API through a same-origin Next.js rewrite. Only the publishable Supabase key belongs in the web environment. The API does not need a Supabase secret API key. Application tables stay in the private `ride_hailing` schema, outside the Supabase Data API, with RLS enabled and forced as defense in depth.

This project runs the Next.js and Go services as separate host processes. It does not start Docker or a local PostgreSQL service.

## Configure Supabase

Prerequisites: Go 1.26 or newer, Node.js 20.9 or newer, and npm. The Go module selects toolchain 1.27.3; local frontend checks used Node 22.

1. Copy `.env.example` to the ignored repository-root `.env`. Set `DATABASE_URL` to the project's PostgreSQL connection string and `SUPABASE_URL` to its HTTPS project URL. Keep both server-side.
2. In Supabase Auth, create and confirm three email/password accounts matching the demo emails in `.env.example`: `rider@example.test`, `driver.one@example.test`, and `driver.two@example.test`. Choose their passwords in Supabase Auth. For the browser test, put the rider and driver test credentials in the corresponding `E2E_*` variables in `.env`.
   In this configured development checkout, the active demo credentials are already stored in the ignored root `.env`; never copy them into tracked files.
3. Configure the Supabase project to sign access tokens with an asymmetric ES256 or RS256 signing key. The Go API verifies against the project's public JWKS and intentionally does not accept the legacy shared-secret HS256 algorithm.
4. Copy `apps/web/.env.local.example` to `apps/web/.env.local`; set `NEXT_PUBLIC_SUPABASE_URL` and `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY` from the project's API settings. The publishable key is intended for browser use. Do not put a secret API key there.
5. Confirm `ride_hailing` is not listed among the project's exposed Data API schemas. Then apply the migration:

   ```powershell
   .\scripts\supabase-db-push.ps1
   ```

   Set `DEMO_SEED_ENABLED=true` in `.env`, then from `apps/api` run:

   ```powershell
   go run ./cmd/seed
   ```

The seed links existing Supabase Auth users to application profiles by email and inserts seven fixed Lahore locations. It never creates or resets Supabase passwords. The seed is transactional and safe to repeat; it does not reset a driver's saved availability. A missing Auth account stops the seed with a setup message.

For a persistent API, use the direct database connection when IPv6 is available or Supavisor session mode on IPv4-only networks. Transaction-mode pooling is intended for transient clients and does not support prepared statements. See the [Supabase PostgreSQL connection guide](https://supabase.com/docs/guides/database/connecting-to-postgres). Never commit `.env` or expose the connection string through a `NEXT_PUBLIC_` variable.

## Run the services separately

Run the Go API in one terminal:

```powershell
Set-Location apps/api
go run ./cmd/api
```

Run Next.js in a second terminal:

```powershell
Set-Location apps/web
npm ci
npm run dev
```

Open <http://127.0.0.1:3000>. The API listens on <http://127.0.0.1:8080>; liveness is `/healthz` and PostgreSQL readiness is `/readyz`. The Next.js rewrite uses the server-only `API_ORIGIN` setting. With no database connection, liveness remains available while readiness and database-backed API routes return `503`.

## Google Maps preview

The rider workspace has a conditional Google Maps JavaScript preview for the seven seeded Lahore locations and selected pickup and destination. The API returns each public demo location's latitude and longitude for this purpose. Without a browser key, the UI shows its setup state; booking still uses the seeded locations and approximate Go estimates.

To enable the preview, add `NEXT_PUBLIC_GOOGLE_MAPS_API_KEY` to the ignored `apps/web/.env.local` and restart Next.js. For a standard Google Cloud project, enable the Maps JavaScript API, configure billing, and restrict the browser key to the web origins and APIs it needs. A browser key is visible to visitors by design; never put a server key in a `NEXT_PUBLIC_` variable. See Google's [Maps JavaScript API setup](https://developers.google.com/maps/documentation/javascript/get-api-key) and [API key security guidance](https://developers.google.com/maps/api-security-best-practices).

The map currently shows seeded stops only. It does not search arbitrary addresses, calculate road directions, or show live driver positions. Google Places and the server-side Routes API can be added in the next integration step; that will also require replacing the fixed-location-only quote and ride contract.

## Checks

Run local backend and frontend checks:

```powershell
Set-Location apps/api
go test ./... -count=1
go vet ./...
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "1"
go test -race ./... -count=1
Remove-Item Env:GOARCH
Remove-Item Env:CGO_ENABLED

Set-Location ../web
npm test
npm run lint
npx tsc --noEmit
npm run build
```

The Windows race run requires a GCC toolchain that targets amd64.

PostgreSQL integration tests use the configured Supabase database and are run with:

```powershell
Set-Location ..
.\scripts\test-supabase-integration.ps1
```

The live browser journey starts Next.js and Go as separate services on dedicated local ports and uses separate rider and driver browser contexts:

```powershell
Set-Location apps/web
npm run test:e2e
```

The browser journey requires a migrated and seeded database, the public Supabase web settings, and the `E2E_RIDER_*` and `E2E_DRIVER_*` credentials in the ignored root `.env`. It first completes any existing assignment on the dedicated demo driver account so reruns start from a usable state. The test leaves completed demo ride records in Supabase. Component tests use API mocks and demonstrate UI behavior only; they do not prove PostgreSQL persistence.

The GitHub Actions workflow runs Go format, unit/HTTP tests and vet, plus frontend tests, lint, type checking, and production build. It does not connect to PostgreSQL, so database integration tests skip there unless a separate database test job is configured.

## Boundaries

Fares and distances use fixed Lahore demo locations and illustrative calculations. Driver coordinates are static. The app does not include live GPS, road routing, payments, notifications, reassignment, production dispatch, or a hosted deployment. Active ride and driver pages poll; the app does not use WebSockets, Redis, or Kafka.

AI tools were used for implementation and documentation support. The repository owner remains responsible for review, credentials, and verification.

## Project documents

- [Requirements and acceptance map](docs/REQUIREMENTS.md)
- [API and domain contract](docs/CONTRACT.md)
- [Architecture and state diagrams](docs/ARCHITECTURE.md)
- [Architecture decisions](docs/DECISIONS.md)
- [Current limitations and unverified checks](docs/LIMITATIONS.md)
- [Technical assessment response (PDF)](output/pdf/technical-assessment-response.pdf)
- [Technical assessment response (Markdown)](docs/TECHNICAL_ASSESSMENT_RESPONSE.md)
- [OpenAPI contract](docs/openapi.yaml)
- [Continuous integration checks](.github/workflows/ci.yml)
