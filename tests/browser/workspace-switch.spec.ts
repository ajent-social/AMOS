import { expect, test } from "@playwright/test";

// This suite intentionally requires a real generated consumer with two live
// memberships and a protected resource route. It does not qualify a fixture.
const required = [
  "AMOS_WORKSPACE_TEST_BASE_URL",
  "AMOS_WORKSPACE_SESSION_COOKIE_NAME",
  "AMOS_WORKSPACE_SESSION_COOKIE_VALUE",
  "AMOS_WORKSPACE_A_ID",
  "AMOS_WORKSPACE_A_NAME",
  "AMOS_WORKSPACE_B_NAME",
  "AMOS_WORKSPACE_UNAUTHORIZED_URL",
];
const missing = required.filter((name) => !process.env[name]);
if (missing.length > 0) {
  throw new Error(
    `workspace-switch browser prerequisites are absent: ${missing.join(", ")}; configure a real app session, two active workspaces, and an unauthorized workspace URL`,
  );
}

const baseURL = process.env.AMOS_WORKSPACE_TEST_BASE_URL!;
const cookieName = process.env.AMOS_WORKSPACE_SESSION_COOKIE_NAME!;
const cookieValue = process.env.AMOS_WORKSPACE_SESSION_COOKIE_VALUE!;
const workspaceAID = process.env.AMOS_WORKSPACE_A_ID!;
const workspaceA = process.env.AMOS_WORKSPACE_A_NAME!;
const workspaceB = process.env.AMOS_WORKSPACE_B_NAME!;
const unauthorizedURL = process.env.AMOS_WORKSPACE_UNAUTHORIZED_URL!;

test("switches verified workspaces and keeps browser Back on current authority", async ({
  browser,
}) => {
  const origin = new URL(baseURL);
  const context = await browser.newContext();
  await context.addCookies([
    {
      name: cookieName,
      value: cookieValue,
      domain: origin.hostname,
      path: "/",
      httpOnly: true,
      secure: origin.protocol === "https:",
      sameSite: "Lax",
    },
    {
      name: "amos_workspace_hint",
      value: workspaceAID,
      domain: origin.hostname,
      path: "/",
      httpOnly: true,
      secure: origin.protocol === "https:",
      sameSite: "Lax",
    },
  ]);
  const page = await context.newPage();
  await page.goto(new URL("/workspaces", baseURL).toString());
  await expect(
    page.getByText(`Current workspace: ${workspaceA}`),
  ).toBeVisible();
  await expect(page.getByLabel("Switch workspace")).toHaveValue(/.+/);
  await expect(
    page
      .getByLabel("Switch workspace")
      .locator("option", { hasText: workspaceB }),
  ).toHaveCount(1);

  await page.getByLabel("Switch workspace").selectOption({ label: workspaceB });
  await page.getByRole("button", { name: "Switch workspace" }).click();
  await expect(
    page.getByText(`Current workspace: ${workspaceB}`),
  ).toBeVisible();

  await page.goBack();
  await expect(
    page.getByText(`Current workspace: ${workspaceB}`),
  ).toBeVisible();

  const denied = await page.goto(new URL(unauthorizedURL, baseURL).toString());
  expect([403, 404]).toContain(denied?.status());
  await expect(page.getByText(workspaceA, { exact: true })).toHaveCount(0);
  await expect(page.getByText(workspaceB, { exact: true })).toHaveCount(0);
  await context.close();
});
