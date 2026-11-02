import { defineConfig, devices } from "@playwright/test";
import { resolve } from "node:path";

const host = process.env.E2E_HOSTNAME ?? "127.0.0.1";
const webPort = Number(process.env.E2E_WEB_PORT ?? 3300);
const apiPort = Number(process.env.E2E_API_PORT ?? 18080);
const webURL = `http://${host}:${webPort}`;
const apiURL = `http://${host}:${apiPort}`;
const reuseExistingServers = process.env.E2E_REUSE_EXISTING_SERVERS === "true" && !process.env.CI;

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: "list",
  timeout: 120_000,
  expect: { timeout: 20_000 },
  use: {
    baseURL: webURL,
    trace: "retain-on-failure",
    ...devices["Desktop Chrome"],
  },
  webServer: [
    {
      command: "go run ./cmd/api",
      cwd: resolve(process.cwd(), "../api"),
      url: `${apiURL}/readyz`,
      reuseExistingServer: reuseExistingServers,
      timeout: 120_000,
      env: {
        PORT: String(apiPort),
        APP_ORIGIN: webURL,
        SUPABASE_URL: process.env.SUPABASE_URL ?? "",
      },
    },
    {
      command: `npm run dev -- --port ${webPort}`,
      cwd: process.cwd(),
      url: webURL,
      reuseExistingServer: reuseExistingServers,
      timeout: 120_000,
      env: {
        API_ORIGIN: apiURL,
        NEXT_DIST_DIR: ".next-e2e",
        NEXT_PUBLIC_SUPABASE_URL: process.env.NEXT_PUBLIC_SUPABASE_URL ?? process.env.SUPABASE_URL ?? "",
        NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY: process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY ?? "",
      },
    },
  ],
});
