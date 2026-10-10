import { expect, test } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { setTimeout as delay } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const repoRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../..",
);
const baseURL = "http://127.0.0.1:4178";
let server: ChildProcess;

async function waitForServer(): Promise<void> {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    if (server.exitCode !== null)
      throw new Error(`billing fixture exited with ${server.exitCode}`);
    try {
      if ((await fetch(`${baseURL}/billing`)).status < 500) return;
    } catch {
      /* wait for the Go fixture */
    }
    await delay(100);
  }
  throw new Error("billing fixture did not become ready");
}

test.beforeAll(async () => {
  server = spawn("go", ["run", "./ui/billingcheckout/testserver"], {
    cwd: repoRoot,
    env: { ...process.env, AMOS_BILLING_TEST_ADDR: "127.0.0.1:4178" },
    stdio: "ignore",
    detached: true,
  });
  await waitForServer();
});
test.afterAll(() => {
  if (server?.pid && server.exitCode === null)
    process.kill(-server.pid, "SIGTERM");
});

test("@fixtures checkout return stays pending until verified payment projection", async ({
  browser,
  request,
}) => {
  const context = await browser.newContext();
  await context.route("https://checkout.example.test/**", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "text/html",
      body: '<!doctype html><html><body><a href="http://127.0.0.1:4178/billing/return?success=1&state=paid">Return to AMOS</a></body></html>',
    });
  });
  const page = await context.newPage();
  await page.goto(`${baseURL}/billing`);
  await expect(
    page.getByText("Active workspace payer: Example workspace"),
  ).toBeVisible();
  await expect(page.getByLabel(/Basic.*USD 12.00.*month/)).toBeVisible();
  await page.getByLabel(/Basic.*USD 12.00.*month/).check();
  await page
    .getByRole("button", { name: "Continue to secure checkout" })
    .click();
  await expect(
    page.getByRole("link", { name: "Return to AMOS" }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Return to AMOS" }).click();

  await expect(page.locator("#payment-state")).toHaveAttribute(
    "data-state",
    "pending",
  );
  await expect(
    page.getByRole("link", { name: "Continue to your paid workspace" }),
  ).toHaveCount(0);
  await expect(page.getByRole("status")).toContainText(
    "Payment is still pending confirmation",
  );

  await expect(
    page.getByText("We have not confirmed payment yet. You can check again."),
  ).toBeVisible({ timeout: 10_000 });
  const webhook = await request.post(`${baseURL}/fixture/webhook`);
  expect(webhook.status()).toBe(204);
  await page
    .getByRole("button", { name: "Check payment status again" })
    .click();
  await expect(page.locator("#payment-state")).toHaveAttribute(
    "data-state",
    "paid",
  );
  const action = page.getByRole("link", {
    name: "Continue to your paid workspace",
  });
  await expect(action).toHaveAttribute("href", "/workspace/paid");
  await action.click();
  await expect(page.getByText("Paid action allowed")).toBeVisible();
  await context.close();
});
