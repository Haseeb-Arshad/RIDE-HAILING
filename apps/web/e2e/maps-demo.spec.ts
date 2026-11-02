import { expect, test } from "@playwright/test";

function requiredSetting(name: string) {
  const value = process.env[name];
  if (!value) throw new Error(`Set ${name} in the ignored repository-root .env before running this focused browser check.`);
  return value;
}

test("authenticated rider loads the Google map preview", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("Demo account").selectOption(requiredSetting("E2E_RIDER_EMAIL"));
  await page.getByLabel("Demo password").fill(requiredSetting("E2E_RIDER_PASSWORD"));
  const continueButton = page.getByRole("button", { name: "Continue" });
  await expect(continueButton).toBeEnabled();
  await continueButton.click();
  await expect(page).toHaveURL(/\/rider$/);
  await expect(page.getByLabel("Pickup", { exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(page.locator(".map-provider-status")).toHaveText("API LOADED", { timeout: 45_000 });
  await expect(page.locator(".google-map-canvas .gm-style")).toBeVisible();
  await page.getByLabel("Pickup", { exact: true }).selectOption("10000000-0000-4000-8000-000000000001");
  await page.getByLabel("Destination", { exact: true }).selectOption("10000000-0000-4000-8000-000000000002");
  await expect(page.locator('.google-map-canvas [title^="Pickup ·"]')).toBeVisible();
  await expect(page.locator('.google-map-canvas [title^="Destination ·"]')).toBeVisible();
  await expect(page.getByText("The map could not load")).toHaveCount(0);
});
