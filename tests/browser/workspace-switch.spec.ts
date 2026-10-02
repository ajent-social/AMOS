import { expect, test } from "@playwright/test";

const required = [
  "AMOS_WORKSPACE_TEST_BASE_URL",
  "AMOS_WORKSPACE_SESSION_COOKIE_NAME",
  "AMOS_WORKSPACE_SESSION_COOKIE_VALUE",
  "AMOS_WORKSPACE_A_ID",
  "AMOS_WORKSPACE_A_NAME",
  "AMOS_WORKSPACE_B_ID",
  "AMOS_WORKSPACE_B_NAME",
  "AMOS_WORKSPACE_REVOKE_TOKEN",
  "AMOS_WORKSPACE_UNAUTHORIZED_ID",
];
const missing = required.filter((name) => !process.env[name]);
if (missing.length > 0) {
  throw new Error(
    `workspace browser prerequisites are absent: ${missing.join(", ")}; run the real apphost PostgreSQL fixture`,
  );
}

const baseURL = process.env.AMOS_WORKSPACE_TEST_BASE_URL!;
const cookieName = process.env.AMOS_WORKSPACE_SESSION_COOKIE_NAME!;
const cookieValue = process.env.AMOS_WORKSPACE_SESSION_COOKIE_VALUE!;
const workspaceAID = process.env.AMOS_WORKSPACE_A_ID!;
const workspaceA = process.env.AMOS_WORKSPACE_A_NAME!;
const workspaceBID = process.env.AMOS_WORKSPACE_B_ID!;
const workspaceB = process.env.AMOS_WORKSPACE_B_NAME!;
const revokeToken = process.env.AMOS_WORKSPACE_REVOKE_TOKEN!;
const unauthorizedID = process.env.AMOS_WORKSPACE_UNAUTHORIZED_ID!;

test("switching, Back, and revocation always use current workspace authority", async ({
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
      secure: true,
      sameSite: "Lax",
    },
  ]);
  const page = await context.newPage();
  const resourceA = await page.goto(
    new URL("/private/data", baseURL).toString(),
  );
  expect(resourceA?.status()).toBe(200);
  expect(resourceA?.headers()["cache-control"]).toContain("no-store");
  await expect(
    page.getByText(`Current workspace: ${workspaceA}`),
  ).toBeVisible();
  await expect(
    page.getByText(`Synthetic record for ${workspaceAID}`),
  ).toBeVisible();

  const choicesA = await page.goto(new URL("/workspaces", baseURL).toString());
  const choicesAText = await page.locator("body").innerText();
  expect(choicesA?.status(), choicesAText).toBe(200);
  expect(choicesAText).toContain(`Current workspace: ${workspaceA}`);
  const workspacePicker = page.getByLabel("Switch workspace");
  await expect(
    page.getByRole("option", { name: workspaceB, exact: true }),
  ).toHaveCount(1);
  await workspacePicker.selectOption(workspaceBID);
  await page.getByRole("button", { name: "Switch workspace" }).click();
  await expect(
    page.getByText(`Current workspace: ${workspaceB}`),
  ).toBeVisible();

  const resourceB = await page.goto(
    new URL("/private/data", baseURL).toString(),
  );
  expect(resourceB?.status()).toBe(200);
  expect(resourceB?.headers()["cache-control"]).toContain("no-store");
  await expect(
    page.getByText(`Current workspace: ${workspaceB}`),
  ).toBeVisible();
  await expect(
    page.getByText(`Synthetic record for ${workspaceBID}`),
  ).toBeVisible();

  let backToHistoricalResource = null;
  for (let step = 0; step < 4; step += 1) {
    backToHistoricalResource = await page.goBack();
    if (new URL(page.url()).pathname === "/private/data") break;
  }
  expect(new URL(page.url()).pathname).toBe("/private/data");
  expect(backToHistoricalResource?.status()).toBe(200);
  expect(backToHistoricalResource?.headers()["cache-control"]).toContain(
    "no-store",
  );
  await expect(
    page.getByText(`Current workspace: ${workspaceB}`),
  ).toBeVisible();
  const restoredResourceText = await page.locator("body").innerText();
  expect(restoredResourceText).toContain(
    `Synthetic record for ${workspaceBID}`,
  );
  await expect(
    page.getByText(`Synthetic record for ${workspaceAID}`),
  ).toHaveCount(0);

  const revoked = await context.request.post(
    new URL("/__test/revoke-b", baseURL).toString(),
    { headers: { "X-Test-Authorization": revokeToken } },
  );
  expect(revoked.status()).toBe(204);
  const staleResource = await page.goto(
    new URL("/private/data", baseURL).toString(),
  );
  expect(staleResource?.status()).toBe(403);
  await expect(
    page.getByText(`Synthetic record for ${workspaceBID}`),
  ).toHaveCount(0);

  const remediation = await page.goto(
    new URL("/workspaces", baseURL).toString(),
  );
  expect(remediation?.status()).toBe(200);
  expect(remediation?.headers()["cache-control"]).toContain("no-store");
  await expect(page.getByRole("status")).toContainText(
    "Your selected workspace is no longer available",
  );
  await expect(page.getByText(`Current workspace: ${workspaceA}`)).toHaveCount(
    0,
  );
  await expect(page.getByRole("option", { name: workspaceB })).toHaveCount(0);

  await context.addCookies([
    {
      name: "amos_workspace_hint",
      value: unauthorizedID,
      domain: origin.hostname,
      path: "/",
      httpOnly: true,
      secure: true,
      sameSite: "Lax",
    },
  ]);
  const unauthorized = await page.goto(
    new URL("/private/data", baseURL).toString(),
  );
  expect(unauthorized?.status()).toBe(403);
  await expect(
    page.getByText(`Synthetic record for ${workspaceAID}`),
  ).toHaveCount(0);
  await expect(
    page.getByText(`Synthetic record for ${workspaceBID}`),
  ).toHaveCount(0);
  await context.close();
});
