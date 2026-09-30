import { expect, test } from "@playwright/test";

test("@fixtures renders server content as escaped text", async ({ page }) => {
  const payload = "<script>synthetic()</script> & \"quoted\"";
  const response = await page.goto(`/?message=${encodeURIComponent(payload)}`);

  expect(response).not.toBeNull();
  expect(response?.status()).toBe(200);
  await expect(page).toHaveTitle("AMOS browser fixture");
  await expect(page.getByRole("heading", { name: "AMOS browser fixture" })).toBeVisible();
  await expect(page.getByTestId("message")).toHaveText(payload);
  await expect(page.locator("body script")).toHaveCount(0);
});

test("@fixtures reports a failed route with its real HTTP status and body", async ({ page }) => {
  const response = await page.goto("/missing");

  expect(response).not.toBeNull();
  expect(response?.status()).toBe(404);
  await expect(page.locator("body")).toHaveText("fixture route not found");
});
