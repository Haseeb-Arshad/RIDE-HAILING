import { defineConfig, devices } from "@playwright/test";
import { resolve } from "node:path";

const webPort = 3300;
const apiPort = 18080;
const webURL = `http://127.0.0.1:${webPort}`;
const apiURL = `http://127.0.0.1:${apiPort}`;

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
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
      reuseExistingServer: false,
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
      reuseExistingServer: false,
      timeout: 120_000,
      env: {
        API_ORIGIN: apiURL,
        NEXT_PUBLIC_SUPABASE_URL: process.env.NEXT_PUBLIC_SUPABASE_URL ?? process.env.SUPABASE_URL ?? "",
        NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY: process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY ?? "",
      },
    },
  ],
});
