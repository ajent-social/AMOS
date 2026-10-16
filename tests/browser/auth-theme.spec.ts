import { expect, test } from "@playwright/test";

const base = process.env.AMOS_AUTH_THEME_BASE_URL;
if (!base)
  throw new Error(
    "AMOS_AUTH_THEME_BASE_URL is required for the presentation fixture",
  );
const query =
  "?challenge=0199a700-0000-7000-8000-000000000001&token=" + "a".repeat(43);

// This fixture renders actual native pages with synthetic backend outcomes.
// Real PostgreSQL host behavior is qualified separately by apphost tests.
for (const width of [1280, 390]) {
  test(`native auth theme loads safely at ${width}px without JavaScript`, async ({
    browser,
  }) => {
    const context = await browser.newContext({
      javaScriptEnabled: false,
      viewport: { width, height: 900 },
    });
    try {
      const page = await context.newPage();
      for (const path of [
        "/signin",
        "/signup",
        "/forgot-password",
        "/verify-email" + query,
        "/reset-password" + query,
        "/verify-email",
        "/reset-password",
      ]) {
        const response = await page.goto(new URL(path, base).toString());
        expect(response?.status()).toBe(200);
        expect(response?.headers()["content-security-policy"]).toContain(
          "style-src 'self'; font-src 'self'; img-src 'self'",
        );
        await expect(page.locator("script")).toHaveCount(0);
        await expect(page.locator(".amos-auth-brand")).toHaveText(
          "Example <script>unsafe</script>",
        );
        await expect(page.locator("body")).toHaveCSS(
          "background-color",
          "rgb(247, 243, 238)",
        );
        expect(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        ).toBe(true);
        expect(
          await page
            .locator(".amos-auth-brand img")
            .evaluate(
              (image: HTMLImageElement) =>
                image.complete && image.naturalWidth === 32,
            ),
        ).toBe(true);
        await expect(page.locator("main#main-content")).toHaveCount(1);
      }
      await page.goto(new URL("/signin", base).toString());
      await page.locator(".amos-auth-brand a").focus();
      await page.keyboard.press("Tab");
      await expect(page.locator("#email")).toBeFocused();
      await expect(page.locator("#email")).toHaveCSS("outline-width", "3px");
      await page.getByLabel("Email").fill("synthetic@example.test");
      await page.getByLabel("Password").fill("Synthetic-password-123");
      await page.getByRole("button", { name: "Sign in", exact: true }).click();
      await expect(page.locator(".amos-auth-message")).toContainText(
        "Email or password was not accepted",
      );
      await expect(page.getByLabel("Password")).toHaveValue("");
    } finally {
      await context.close();
    }
  });
}
