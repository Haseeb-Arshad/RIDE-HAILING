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
  const pickupSelect = page.getByLabel("Pickup", { exact: true });
  const cancelRideButton = page.getByRole("button", { name: "Cancel ride" });
  await expect(pickupSelect.or(cancelRideButton)).toBeVisible({ timeout: 30_000 });
  if (await cancelRideButton.isVisible()) {
    page.once("dialog", (dialog) => dialog.accept());
    await cancelRideButton.click();
    await expect(page.getByText("Trip cancelled")).toBeVisible({ timeout: 30_000 });
    await page.goto("/rider");
  }
  await expect(pickupSelect).toBeVisible({ timeout: 30_000 });
  await expect(page.locator(".map-provider-status")).toHaveText("API LOADED", { timeout: 45_000 });
  await expect(page.locator(".google-map-canvas .gm-style")).toBeVisible();
  await page.getByLabel("Pickup", { exact: true }).selectOption("10000000-0000-4000-8000-000000000001");
  await page.getByLabel("Destination", { exact: true }).selectOption("10000000-0000-4000-8000-000000000002");
  await expect(page.locator('.google-map-canvas [title^="Pickup ·"]').first()).toBeVisible();
  await expect(page.locator('.google-map-canvas [title^="Destination ·"]').first()).toBeVisible();
  await page.getByLabel("Pickup", { exact: true }).selectOption("__map__");
  await expect(page.getByText("Click inside Lahore to set your pickup pin.")).toBeVisible();
  const map = page.locator(".google-map-canvas");
  const mapBounds = await map.boundingBox();
  if (!mapBounds) throw new Error("Google Maps canvas has no visible bounds.");
  await map.click({ position: { x: mapBounds.width / 2, y: mapBounds.height / 2 } });
  await expect(page.getByLabel("Pickup place")).toHaveValue("Pinned location");
  await page.getByLabel("Destination", { exact: true }).selectOption("__map__");
  await expect(page.getByText("Click inside Lahore to set your destination pin.")).toBeVisible();
  await map.click({ position: { x: mapBounds.width / 2 + 70, y: mapBounds.height / 2 + 24 } });
  await expect(page.getByLabel("Destination place")).toHaveValue("Pinned location");
  await page.getByRole("button", { name: "Review fare estimate" }).click();
  await page.getByText("How this estimate is calculated").click();
  await expect(page.getByText("Estimated fuel cost")).toBeVisible();
  await expect(page.getByText("The map could not load")).toHaveCount(0);
});
