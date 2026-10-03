import { chromium, expect, test } from "@playwright/test";

const required = [
  "AMOS_BILLING_CHECKOUT_BASE_URL",
  "AMOS_BILLING_CHECKOUT_SESSION_COOKIE_NAME",
  "AMOS_BILLING_CHECKOUT_SESSION_COOKIE_VALUE",
  "AMOS_BILLING_CHECKOUT_PENDING_INTENT_ID",
];
const missing = required.filter((name) => !process.env[name]);
const baseURL = process.env.AMOS_BILLING_CHECKOUT_BASE_URL;
const cookieName =
  process.env.AMOS_BILLING_CHECKOUT_SESSION_COOKIE_NAME ??
  "__Host-amos_session";
const cookieValue =
  process.env.AMOS_BILLING_CHECKOUT_SESSION_COOKIE_VALUE ??
  "fixture-placeholder";
const intentID = process.env.AMOS_BILLING_CHECKOUT_PENDING_INTENT_ID ?? "";
let appOrigin: URL;
try {
  appOrigin = new URL(baseURL ?? "https://billing-fixture.example");
} catch {
  appOrigin = new URL("https://billing-fixture.example");
}
const sessionCookie = {
  name: cookieName,
  value: cookieValue,
  url: appOrigin.origin,
  httpOnly: true,
  secure: true,
  sameSite: "Lax" as const,
};

test("@billing-checkout browser session cookie is host-only and secure", () => {
  expect(cookieName.startsWith("__Host-")).toBe(true);
  expect(appOrigin.protocol).toBe("https:");
  expect(sessionCookie).toHaveProperty("url", appOrigin.origin);
  expect(sessionCookie).not.toHaveProperty("domain");
  expect(sessionCookie).not.toHaveProperty("path");
});

test("@billing-checkout forged success stays pending and keeps paid action locked", async () => {
  if (missing.length > 0) {
    throw new Error(
      `T6.5 real-app browser prerequisites are absent: ${missing.join(", ")}; the composed authenticated billing fixture must provide a pending intent`,
    );
  }
  if (
    !baseURL ||
    !cookieName.startsWith("__Host-") ||
    appOrigin.protocol !== "https:"
  ) {
    throw new Error(
      "T6.5 browser fixture requires an HTTPS origin and a __Host- session cookie",
    );
  }
  const browser = await chromium.launch();
  try {
    const context = await browser.newContext();
    await context.addCookies([sessionCookie]);
    const scopedCookie = (await context.cookies(appOrigin.origin)).find(
      (cookie) => cookie.name === cookieName,
    );
    expect(scopedCookie).toMatchObject({
      name: cookieName,
      httpOnly: true,
      secure: true,
      sameSite: "Lax",
    });
    const subdomainCookies = await context.cookies(
      `https://untrusted-${appOrigin.hostname}`,
    );
    expect(
      subdomainCookies.some((cookie) => cookie.name === cookieName),
      "host-only session cookie must not be sent to a sibling host",
    ).toBe(false);

    const page = await context.newPage();
    const health = await page.request.get(
      new URL("/readyz", baseURL).toString(),
    );
    expect(
      health.ok(),
      "the real app must be ready before billing checks",
    ).toBeTruthy();

    const response = await page.goto(
      new URL(
        `/billing/return/${encodeURIComponent(intentID)}?success=1`,
        baseURL,
      ).toString(),
    );
    expect(response?.status()).toBe(200);
    expect(response?.headers()["cache-control"]).toContain("no-store");
    await expect(
      page.getByRole("heading", { name: "Workspace billing" }),
    ).toBeVisible();
    await expect(
      page.getByText("Checking payment status. Your plan is not active yet."),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Continue to workspace" }),
    ).toBeDisabled();

    const status = await page.request.get(
      new URL(
        `/billing/status/${encodeURIComponent(intentID)}`,
        baseURL,
      ).toString(),
    );
    expect(status.status()).toBe(200);
    const projection = (await status.json()) as { confirmed?: boolean };
    expect(projection.confirmed).toBe(false);
    await context.close();
  } finally {
    await browser.close();
  }
});
