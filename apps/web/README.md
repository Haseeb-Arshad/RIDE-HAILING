# Ride Lahore web app

This Next.js App Router app is the rider and driver interface. Ride rules, identity, prices, and persisted state remain in the Go API. Browser requests use same-origin `/api/v1/*` routes, forwarded to Go by `next.config.ts`.

## Local run

```powershell
npm ci
Copy-Item .env.local.example .env.local
npm run dev
```

Open `http://127.0.0.1:3000`. Use `/login` to sign in with one of the seeded account emails. Enter the password configured by `DEMO_ACCOUNT_PASSWORD` during seeding. The login screen never fills or stores that password.

The rider workspace is `/rider`, the driver workspace is `/driver`, and saved ride status is `/rides/{id}`. The API must be running, and the PostgreSQL migration plus seed must be applied, for account and booking flows to work. With no Supabase connection, the web shell is available but the API-backed journey returns an unavailable state.

`API_ORIGIN` in `.env.local` configures the server-side rewrite target. Keep it server-only; do not rename it to `NEXT_PUBLIC_API_ORIGIN`.

## Checks

```powershell
npm test
npm run lint
npx tsc --noEmit
npm run build
```

Component tests mock the Go API. They verify UI behavior and do not establish database persistence or the full two-browser workflow.
