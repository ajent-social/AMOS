import { expect, test } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { setTimeout as delay } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const port = Number(process.env.AMOS_UI_OVERRIDES_PORT ?? "4177");
const baseURL = `http://127.0.0.1:${port}`;
test.use({ launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined } });
let server: ChildProcess;

async function waitForServer(): Promise<void> {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    if (server.exitCode !== null) throw new Error(`override example exited with ${server.exitCode}`);
    try {
      if ((await fetch(`${baseURL}/health`)).status === 204) return;
    } catch { /* The example server is still starting. */ }
    await delay(100);
  }
  throw new Error("override example did not become ready; Go prerequisite is required");
}

test.beforeAll(async () => {
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error("AMOS_UI_OVERRIDES_PORT must be a valid port");
  server = spawn("go", ["run", "./examples/ui-overrides"], {
    cwd: repositoryRoot,
    env: { ...process.env, AMOS_UI_OVERRIDES_ADDR: `127.0.0.1:${port}` },
    stdio: "ignore",
  });
  await waitForServer();
});
test.afterAll(() => { server?.kill("SIGTERM"); });

test("@ui-overrides customized signin and checkout retain escaped POST action and CSRF slots", async ({ browser }) => {
  for (const [route, heading, field] of [["signin", "Sign in to Northstar", "email"], ["checkout", "Review your order", "address"]]) {
    const context = await browser.newContext({ javaScriptEnabled: false });
    const page = await context.newPage();
    await page.goto(`${baseURL}/${route}`);
    await expect(page).toHaveTitle(route === "signin" ? "Northstar sign in" : "Northstar checkout");
    await expect(page.getByRole("heading", { name: heading })).toBeVisible();
    await expect(page.locator("form")).toHaveAttribute("method", "POST");
    await expect(page.locator("form")).toHaveAttribute("action", `/${route}`);
    await expect(page.locator("input[name='_csrf']")).toHaveValue("example-test-token");
    await expect(page.locator(`input[name='${field}']`)).toBeVisible();
    await expect(page.locator("link[rel='stylesheet']")).toHaveCount(2);
    await context.close();
  }
});

test("@ui-overrides profile content is escaped and an unselected page uses the default renderer", async ({ page, request }) => {
  const payload = '<script>document.body.dataset.pwned="yes"</script> & "quoted"';
  await page.goto(`${baseURL}/profile?name=${encodeURIComponent(payload)}`);
  await expect(page.getByRole("heading", { name: `Profile for ${payload}` })).toBeVisible();
  await expect(page.locator("body script")).toHaveCount(0);
  expect(await page.locator("body").getAttribute("data-pwned")).toBeNull();
  const stylesheet = await page.locator("link[rel='stylesheet']").first().getAttribute("href");
  expect(stylesheet).toContain("/assets/themes/northstar/");
  const response = await request.get(`${baseURL}${stylesheet}`);
  expect(response.status()).toBe(200);
  expect(response.headers()["cache-control"]).toContain("immutable");
  expect(response.headers()["x-content-type-options"]).toBe("nosniff");
  expect(await response.text()).toContain("--amos-northstar-accent");

  await page.goto(`${baseURL}/dashboard`);
  await expect(page).toHaveTitle("Dashboard");
  await expect(page.getByRole("heading", { name: "Default dashboard" })).toBeVisible();
  await expect(page.locator("link[href*='/assets/themes/northstar/']")).toHaveCount(0);
});
