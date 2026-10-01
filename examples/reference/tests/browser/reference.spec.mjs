import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(path.resolve(process.cwd(), "package.json"));
const { test, expect } = require("@playwright/test");

test.describe("@reference", () => {

async function prepare(page, account = "alice") {
  await page.request.post("/_test/reset");
  const infoResponse = await page.request.get("/_test/info");
  expect(infoResponse.ok()).toBeTruthy();
  const info = await infoResponse.json();
  const loginResponse = await page.request.post("/_test/login", { form: { account } });
  expect(loginResponse.ok()).toBeTruthy();
  const login = await loginResponse.json();
  return { ...info, ...login };
}

async function createTodo(page, workspace, title) {
  await page.goto(`/todos?workspace=${encodeURIComponent(workspace)}`);
  await page.getByLabel("New todo").fill(title);
  await page.getByRole("button", { name: "Add todo" }).click();
  await expect(page.getByRole("status")).toContainText("Todo added");
}

test.afterEach(async ({ page }) => {
  await page.request.post("/_test/reset");
});

test("public landing renders workspace entry without a session", async ({ page }) => {
  const response = await page.goto("/");
  expect(response?.status()).toBe(200);
  await expect(page.getByRole("heading", { name: "Your work, in one place" })).toBeVisible();
  await expect(page.getByLabel("Workspace ID")).toBeVisible();
});

test("fits mobile, laptop, and wide viewports without horizontal overflow", async ({ page }) => {
  for (const width of [390, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/?workspace=sample-workspace");
    const sizes = await page.evaluate(() => ({
      pageWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
    }));
    expect(sizes.pageWidth).toBeLessThanOrEqual(sizes.viewportWidth);
  }
});

test("loads the pinned HTMX enhancement from the same origin", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator('script[src^="https://"]')).toHaveCount(0);
  const asset = await page.request.get("/reference/htmx.min.js");
  expect(asset.ok()).toBeTruthy();
  expect(asset.headers()["cache-control"]).toContain("private");
  expect(await asset.text()).toContain("htmx");
});

test("creates, lists, and edits an allowed todo through the session-backed service", async ({ page }) => {
  const { workspace } = await prepare(page);
  await createTodo(page, workspace, "Prepare quarterly report");
  const item = page.getByRole("listitem").filter({ hasText: "Prepare quarterly report" });
  await expect(item).toBeVisible();
  await item.getByRole("link", { name: "Edit" }).click();
  await page.getByLabel("Title", { exact: true }).fill("Prepare annual report");
  await page.getByLabel("Completed").check();
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("status")).toContainText("Todo updated");
  await expect(page.getByRole("listitem").filter({ hasText: "Prepare annual report" })).toContainText("Complete");
});

test("shows server validation for invalid form data", async ({ page }) => {
  const { workspace } = await prepare(page);
  await page.route("**/reference/htmx.min.js", (route) => route.abort());
  await page.route("**/reference/csrf.js", (route) => route.abort());
  await page.goto(`/todos?workspace=${encodeURIComponent(workspace)}`);
  const form = page.locator('form[action="/todos"][method="post"]');
  await form.evaluate((element) => { element.noValidate = true; });
  await page.getByLabel("New todo").fill(" ");
  const [response] = await Promise.all([
    page.waitForNavigation(),
    page.getByRole("button", { name: "Add todo" }).click(),
  ]);
  expect(response?.status()).toBe(400);
  await expect(page.getByRole("alert")).toContainText("todo title");
});

test("rejects stale edits and leaves a recovery message", async ({ page }) => {
  const { workspace, csrfToken } = await prepare(page);
  await page.route("**/reference/htmx.min.js", (route) => route.abort());
  await page.route("**/reference/csrf.js", (route) => route.abort());
  await createTodo(page, workspace, "Original title");
  const item = page.getByRole("listitem").filter({ hasText: "Original title" });
  const editLink = item.getByRole("link", { name: "Edit" });
  const editPath = await editLink.getAttribute("href");
  await editLink.click();
  const revision = await page.locator('input[name="revision"]').inputValue();
  const todoID = new URL(editPath, "http://localhost").pathname.split("/")[2];
  const concurrent = await page.request.post(`/todos/${todoID}/edit`, {
    form: { _csrf: csrfToken, workspace, title: "Concurrent title", revision },
    headers: { Origin: new URL(page.url()).origin },
    maxRedirects: 0,
  });
  expect(concurrent.status()).toBe(303);
  await page.getByLabel("Title", { exact: true }).fill("Stale title");
  const [response] = await Promise.all([
    page.waitForNavigation(),
    page.getByRole("button", { name: "Save changes" }).click(),
  ]);
  expect(response?.status()).toBe(409);
  await expect(page.getByRole("alert")).toContainText("changed since you opened it");
});

test("does not expose another account's workspace todos", async ({ page }) => {
  const { workspace: aliceWorkspace, foreignWorkspace } = await prepare(page, "alice");
  const bobLogin = await page.request.post("/_test/login", { form: { account: "bob" } }).then((result) => result.json());
  expect(bobLogin.workspace).toBe(foreignWorkspace);
  await createTodo(page, bobLogin.workspace, "Bob private item");
  await page.request.post("/_test/login", { form: { account: "alice" } });
  const response = await page.goto(`/todos?workspace=${encodeURIComponent(bobLogin.workspace)}`);
  expect(response?.status()).toBe(403);
  await expect(page.getByRole("alert")).toContainText(/unavailable|forbidden/i);
  await expect(page.getByText("Bob private item")).toHaveCount(0);
  expect(aliceWorkspace).toBeTruthy();
});

test("renders hostile todo titles as text", async ({ page }) => {
  const { workspace } = await prepare(page);
  await page.addInitScript(() => { window.__referencePayloadRan = false; });
  const payload = '<img src=x onerror="window.__referencePayloadRan=true">';
  await createTodo(page, workspace, payload);
  await expect(page.getByText(payload, { exact: true })).toBeVisible();
  await expect(page.locator("img")).toHaveCount(0);
  expect(await page.evaluate(() => window.__referencePayloadRan)).toBe(false);
});

test("submits a valid mutation with JavaScript disabled using the hidden CSRF field", async ({ browser, baseURL }) => {
  const context = await browser.newContext({ baseURL, javaScriptEnabled: false });
  try {
    const page = await context.newPage();
    const { workspace } = await prepare(page);
    await page.goto(`/todos?workspace=${encodeURIComponent(workspace)}`);
    const token = await page.locator('form[action="/todos"][method="post"] input[name="_csrf"]').inputValue();
    expect(token).toBeTruthy();
    await page.getByLabel("New todo").fill("Works without JavaScript");
    await page.getByRole("button", { name: "Add todo" }).click();
    await expect(page.getByRole("status")).toContainText("Todo added");
    await expect(page.getByText("Works without JavaScript")).toBeVisible();
  } finally {
    await context.close();
  }
});
});
