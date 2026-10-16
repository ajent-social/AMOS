import { expect, test } from "@playwright/test";
const base = process.env.AMOS_AUTH_THEME_BASE_URL;
if (!base)
  throw new Error(
    "AMOS_AUTH_THEME_BASE_URL is required for the navigation fixture",
  );
// Synthetic action status tests browser navigation only. Real session admission
// and signout denial run separately in PostgreSQL-backed apphost tests.
for (const width of [1280, 390]) {
  test(`configured native returns stay local at ${width}px without JavaScript`, async ({
    browser,
  }) => {
    const context = await browser.newContext({
      javaScriptEnabled: false,
      viewport: { width, height: 900 },
    });
    try {
      const page = await context.newPage();
      for (const [next, destination] of [
        ["", "/consumer/account"],
        ["/consumer/other", "/consumer/other"],
        ["https://foreign.example/", "/consumer/account"],
        ["//foreign.example/", "/consumer/account"],
        ["/consumer/account/extra", "/consumer/account"],
        ["/consumer/account?private=value", "/consumer/account"],
      ]) {
        const signin = new URL("/signin", base);
        signin.searchParams.set("next", next);
        await page.goto(signin.toString());
        await expect(page.locator('input[name="next"]')).toHaveValue(
          destination,
        );
        await page.getByLabel("Email").fill("synthetic@example.test");
        await page.getByLabel("Password").fill("Synthetic-password-123");
        await page
          .getByRole("button", { name: "Sign in", exact: true })
          .click();
        await expect(page).toHaveURL(new URL(destination, base).toString());
        await expect(page.locator("body")).toHaveText(destination);
      }
    } finally {
      await context.close();
    }
  });
}
