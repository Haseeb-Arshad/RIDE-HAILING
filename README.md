# Lahore Ride Hailing

A single-repository ride-booking demo for Lahore. The planned stack is a Next.js and TypeScript rider/driver app, a Go and Chi API, and PostgreSQL as the source of truth.

## Project status

The architecture and API contract are recorded, and the initial Next.js and Go services are scaffolded. This stage includes liveness/readiness endpoints and placeholder pages only; ride, authentication, and database features are not implemented yet.

## Project guide

- [Requirements and acceptance map](docs/REQUIREMENTS.md)
- [API and domain contract](docs/CONTRACT.md)
- [Architecture and state diagrams](docs/ARCHITECTURE.md)
- [Architecture decisions](docs/DECISIONS.md)
- [OpenAPI draft](docs/openapi.yaml)

The contract describes a deliberately bounded demo: seeded Lahore locations and accounts, approximate fare and pickup estimates, cookie sessions, and transactional ride assignment. It does not claim real routing, live driver tracking, payments, or production dispatch.

## Run the services separately

Prerequisites: Go 1.25 or newer, Node.js 22.13.x or a supported LTS release (24+), and npm. Node 24 LTS is recommended for ongoing work. The Go module selects toolchain 1.27.3. The API and web app run as separate local processes. This project does not start or connect to a local PostgreSQL service or container. The API can run without a database URL in this scaffold stage; `/readyz` returns `503` until a PostgreSQL connection is configured.

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

The application will use PostgreSQL through Go's `pgx` driver. Supabase credentials are not needed for this scaffold. When database migrations and seed data are ready, provide a PostgreSQL connection string for the API's ignored root `.env` file. For a persistent API, use the direct connection when IPv6 is available or Supavisor session mode on IPv4-only networks. Transaction mode is intended for transient/serverless clients and does not support prepared statements ([Supabase connection guide](https://supabase.com/docs/guides/database/connecting-to-postgres)). We will confirm the connection mode and pgx settings when the database stage begins. Never commit `.env` or expose the connection string in frontend variables. No Supabase Auth or browser-side database client is planned; Go remains the only business API.

## Current checks

```powershell
Set-Location apps/api
go build ./...

Set-Location ../web
npm run lint
npx tsc --noEmit
npm run build
```

These commands check the scaffold only. There are no ride flows or application tests yet; those arrive in later stages.
