import { expect, test, type BrowserContext, type Page } from "@playwright/test";

function requiredSetting(name: string) {
  const value = process.env[name];
  if (!value) throw new Error(`Set ${name} in the ignored repository-root .env before running the live browser journey.`);
  return value;
}

async function signIn(page: Page, email: string, password: string, destination: "rider" | "driver") {
  await page.goto("/login");
  await page.getByLabel("Demo account").selectOption(email);
  await page.getByLabel("Demo password").fill(password);
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page).toHaveURL(new RegExp(`/${destination}$`));
}

test("rider books a ride that a separate driver context accepts and completes", async ({ browser }) => {
  const riderContext: BrowserContext = await browser.newContext();
  const driverContext: BrowserContext = await browser.newContext();
  const riderPage = await riderContext.newPage();
  const driverPage = await driverContext.newPage();

  try {
    await signIn(
      driverPage,
      requiredSetting("E2E_DRIVER_EMAIL"),
      requiredSetting("E2E_DRIVER_PASSWORD"),
      "driver",
    );
    await expect(driverPage.getByRole("heading", { name: /Good to see you/ })).toBeVisible();
    const availability = driverPage.locator(".availability-badge");
    await expect(availability).not.toHaveText("Checking", { timeout: 30_000 });
    const previousTrip = driverPage.getByRole("button", { name: "Complete trip" });
    if (await previousTrip.isVisible()) {
      await previousTrip.click();
      await expect(availability).toHaveText("Available", { timeout: 30_000 });
      await expect(driverPage.getByRole("status").filter({ hasText: /completed/i })).toBeVisible();
    }

    await signIn(
      riderPage,
      requiredSetting("E2E_RIDER_EMAIL"),
      requiredSetting("E2E_RIDER_PASSWORD"),
      "rider",
    );
    await expect(riderPage.getByLabel("Pickup")).toBeVisible({ timeout: 30_000 });
    await riderPage.getByLabel("Pickup").selectOption("10000000-0000-4000-8000-000000000001");
    await riderPage.getByLabel("Destination").selectOption("10000000-0000-4000-8000-000000000002");
    await riderPage.getByRole("button", { name: "Review fare estimate" }).click();
    await expect(riderPage.getByText("Approximate estimate")).toBeVisible();
    await riderPage.getByRole("button", { name: "Request this ride" }).click();
    await expect(riderPage).toHaveURL(/\/rides\/[0-9a-f-]+$/);
    await expect(riderPage.getByText("Ride requested")).toBeVisible();

    const acceptButton = driverPage.getByRole("button", { name: "Accept ride" });
    await expect(acceptButton).toBeVisible({ timeout: 30_000 });
    await acceptButton.click();
    await expect(driverPage.getByRole("button", { name: "Complete trip" })).toBeVisible();
    await expect(riderPage.getByText("Driver assigned")).toBeVisible({ timeout: 15_000 });

    await driverPage.getByRole("button", { name: "Complete trip" }).click();
    await expect(driverPage.getByRole("status").filter({ hasText: /completed/i })).toBeVisible();
    await expect(availability).toHaveText("Available", { timeout: 30_000 });
    await expect(riderPage.getByText("Trip completed")).toBeVisible({ timeout: 15_000 });
    await expect(riderPage.getByText("Your trip is complete. Thanks for riding with us.")).toBeVisible();
  } finally {
    await Promise.allSettled([riderContext.close(), driverContext.close()]);
  }
});
