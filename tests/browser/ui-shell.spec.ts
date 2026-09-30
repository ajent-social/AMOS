import { expect, test } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { setTimeout as delay } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const port = 4176;
const baseURL = `http://127.0.0.1:${port}`;
let server: ChildProcess;

async function waitForServer(): Promise<void> {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    if (server.exitCode !== null) throw new Error(`renderer test server exited with ${server.exitCode}`);
    try {
      const response = await fetch(`${baseURL}/health`);
      if (response.ok) return;
    } catch { /* The Go server is still starting. */ }
    await delay(100);
  }
  throw new Error("renderer test server did not become ready; Go prerequisite is required");
}

test.beforeAll(async () => {
  server = spawn("go", ["run", "./ui/render/testserver"], {
    cwd: repositoryRoot,
    env: { ...process.env, PORT: String(port) },
    stdio: "ignore",
  });
  await waitForServer();
});
test.afterAll(() => { server?.kill("SIGTERM"); });

test("real renderer supports keyboard use, escaped profile text and no-JS form submission", async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  const payload = '<script>document.body.dataset.pwned="yes"</script> & "quoted"';
  await page.goto(`${baseURL}/ui?name=${encodeURIComponent(payload)}&error=true`);
  await expect(page).toHaveTitle("Profile");
  await expect(page.getByRole("heading", { name: "Your profile" })).toBeVisible();
  await expect(page.locator(".profile-name")).toHaveText(payload);
  await expect(page.locator("body script")).toHaveCount(0);
  expect(await page.locator("body").getAttribute("data-pwned")).toBeNull();
  await expect(page.getByRole("region", { name: "Please correct these errors" })).toBeVisible();
  await expect(page.getByLabel("Display name")).toHaveAttribute("aria-describedby", "name-error");
  await expect(page.getByRole("link", { name: "Enter a display name" })).toHaveAttribute("href", "#name");

  await page.keyboard.press("Tab");
  await expect(page.getByRole("link", { name: "Skip to main content" })).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("link", { name: "AMOS" })).toBeFocused();
  await expect(page.locator("input[name='_csrf']")).toHaveValue("synthetic-operation-token");
  await page.getByLabel("Display name").fill("Grace Hopper");
  await page.getByRole("button", { name: "Save profile" }).click();
  await expect(page.getByRole("status")).toHaveText("Profile saved");
  await expect(page.locator(".profile-name")).toHaveText("Grace Hopper");
  await context.close();
});

test("HTMX main target gets a bounded fragment and missing metadata gets full HTML", async ({ request }) => {
  const full = await request.get(`${baseURL}/ui`, { headers: { "HX-Request": "true" } });
  expect(full.headers()["cache-control"]).toBe("no-store");
  expect(full.headers().vary).toContain("HX-Target");
  expect((await full.text()).startsWith("<!doctype html>")).toBeTruthy();
  const fragment = await request.get(`${baseURL}/ui`, { headers: { "HX-Request": "true", "HX-Target": "main-content" } });
  const html = await fragment.text();
  expect(html.startsWith("<main id=\"main-content\"")).toBeTruthy();
  expect(html.includes("<!doctype html>")).toBeFalsy();
});
