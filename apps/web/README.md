# Ride Lahore web app

This Next.js App Router app presents the rider and driver journeys. Supabase Auth manages browser sign-in and session refresh through `@supabase/ssr`; the Go API validates bearer access tokens and owns ride rules, roles, pricing, matching, and persisted state. Browser API requests use same-origin `/api/v1/*` routes, forwarded to Go by `next.config.ts`.

## Local run

```powershell
npm ci
Copy-Item .env.local.example .env.local
npm run dev
```

Set `NEXT_PUBLIC_SUPABASE_URL` and `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY` in `.env.local`. Set `API_ORIGIN` to the server-side Go API URL; do not rename it to a `NEXT_PUBLIC_` variable. `NEXT_PUBLIC_GOOGLE_MAPS_API_KEY` is optional; add a website-restricted key with Maps JavaScript API enabled to display the seeded Lahore map preview. The API and migrated PostgreSQL database must be available for sign-in and booking. Create the demo email/password accounts in Supabase Auth, then run the root seed command to map them to rider and driver profiles.

The rider workspace is `/rider`, the driver workspace is `/driver`, and saved ride status is `/rides/{id}`. Supabase role metadata is not used for authorization; Go resolves roles from the private `ride_hailing.users` profile table.

## Checks

```powershell
npm test
npm run lint
npx tsc --noEmit
npm run build
npm run test:e2e
```

Component tests mock the Go API and verify UI behavior only. `npm run test:e2e` starts the web and Go services separately, requires the migrated Supabase database and E2E credentials in the ignored root `.env`, and uses isolated rider and driver browser contexts.
