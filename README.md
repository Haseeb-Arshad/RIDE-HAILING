# Lahore Ride Hailing

A single-repository ride-booking demo for Lahore. The planned stack is a Next.js and TypeScript rider/driver app, a Go and Chi API, and PostgreSQL as the source of truth.

## Project status

The Next.js and Go services are scaffolded. Stages 3–6 now include a versioned Supabase migration, repeatable Go seed command, cookie-based API authentication, Lahore locations, server-calculated fare/ETA quotes, idempotent ride creation, owner-scoped reads, driver availability, and transactional ride assignment and lifecycle routes. The migration and seed have not been applied because the Supabase connection string has not been provided, so database-backed behavior is not yet verified. The web login remains a placeholder; rider/driver booking screens are the next stage.

## Project guide

- [Requirements and acceptance map](docs/REQUIREMENTS.md)
- [API and domain contract](docs/CONTRACT.md)
- [Architecture and state diagrams](docs/ARCHITECTURE.md)
- [Architecture decisions](docs/DECISIONS.md)
- [OpenAPI draft](docs/openapi.yaml)

The contract describes a deliberately bounded demo: seeded Lahore locations and accounts, approximate fare and pickup estimates, cookie sessions, and transactional ride assignment. It does not claim real routing, live driver tracking, payments, or production dispatch.

## Run the services separately

Prerequisites: Go 1.26 or newer, Node.js 24 LTS or newer active LTS, and npm. The Go module selects toolchain 1.27.3. The API and web app run as separate local processes. This project does not start or connect to a local PostgreSQL service or container. With no database URL, liveness stays available while `/readyz` and database-backed API routes return `503`.

1. Copy `.env.example` to `.env` at the repository root. Leave `DATABASE_URL` blank for the current scaffold.
2. In a terminal, run the Go API:

   ```powershell
   Set-Location apps/api
   go run ./cmd/api
   ```

3. In a second terminal, configure and run the web app:

   ```powershell
   Set-Location apps/web
   Copy-Item .env.local.example .env.local
   npm ci
   npm run dev
   ```

4. Open <http://127.0.0.1:3000>. The placeholder login page is at <http://127.0.0.1:3000/login>.

Both processes bind to loopback only. The API listens at <http://127.0.0.1:8080>. Check liveness at `/healthz`, database readiness at `/readyz`, and the Next.js same-origin proxy at <http://127.0.0.1:3000/api/v1/healthz>. The proxy destination comes from the server-only `API_ORIGIN` setting; do not prefix it with `NEXT_PUBLIC_`.

## Database

The application uses PostgreSQL through Go's `pgx` driver. The migration is in `supabase/migrations/`; it creates the private `ride_hailing` schema, database constraints, and indexes. The checked-in Supabase config omits `ride_hailing` from the Data API schema list, and the migration revokes schema/table grants and enables and forces RLS with no client policies. Confirm the remote project's exposed-schema settings also omit `ride_hailing` before applying the migration. Go uses qualified table names and the server-side connection.

Once a Supabase connection string is available:

1. Put it in `DATABASE_URL` in the ignored root `.env` file.
2. Run `.\scripts\supabase-db-push.ps1` from the repository root. It uses the pinned Supabase CLI and reads the connection from `.env` without writing it into a tracked file.
3. Set `DEMO_SEED_ENABLED=true` and a private `DEMO_ACCOUNT_PASSWORD` of 12–72 bytes in that same ignored `.env` file.
4. From `apps/api`, run `go run ./cmd/seed`. The command wraps all fixtures in a transaction and is safe to rerun; it does not reset active driver availability.

The seed creates `rider@example.test`, `driver.one@example.test`, and `driver.two@example.test`; all use the password configured locally. It also inserts seven approximate Lahore locations. Do not enable demo seeding against a shared database until you have chosen an appropriate demo password. The database stages remain unverified until the migration, seed, and integration checks have run against the supplied Supabase database. No local PostgreSQL process or Docker container is started by this project.

For a persistent API, use the direct connection when IPv6 is available or Supavisor session mode on IPv4-only networks. Transaction mode is intended for transient/serverless clients and does not support prepared statements ([Supabase connection guide](https://supabase.com/docs/guides/database/connecting-to-postgres)). Never commit `.env` or expose the connection string in frontend variables. No Supabase Auth or browser-side database client is planned; Go remains the only business API.

## Current checks

```powershell
Set-Location apps/api
go build ./...
go test ./...

Set-Location ../web
npm run lint
npx tsc --noEmit
npm run build
```

`go test ./...` runs the local auth, pricing, ride-service, and HTTP tests. PostgreSQL schema, session, ride, and lifecycle integration checks are skipped unless `TEST_DATABASE_URL` is set. After the Supabase migration and seed have been applied, run `.\scripts\test-supabase-integration.ps1`. Most database checks use rollback-only transactions; cross-connection race tests create unique temporary actors and remove their test rows during cleanup. The integration check requires a privileged PostgreSQL connection because RLS is forced on the application tables.

The current backend checks cover schema/authentication foundations, deterministic pricing, request validation, retry semantics, route-level role/CSRF behavior, and driver/lifecycle handlers. Real PostgreSQL booking and assignment behavior remains unverified until the supplied database is configured. Frontend booking and end-to-end checks are later stages.
