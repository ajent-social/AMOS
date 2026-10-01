import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { readFile, readdir } from "node:fs/promises";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

const base = process.env.AMOS_AUTH_BASE_URL;
const mailbox = process.env.AMOS_AUTH_MAIL_DIRECTORY;
if (!base || !mailbox) {
  throw new Error(
    "T6.3 requires AMOS_AUTH_BASE_URL and AMOS_AUTH_MAIL_DIRECTORY for the real local application fixture",
  );
}
const appURL = new URL(base);
if (appURL.protocol !== "http:" && appURL.protocol !== "https:") {
  throw new Error("AMOS_AUTH_BASE_URL must use HTTP or HTTPS");
}

const signupPassword = "Correct-Horse-9!example";
const replacementPassword = "Different-Horse-8!example";
type LocalMailMessage = { recipient?: string; subject?: string; text?: string };

async function actionURL(recipient: string, kind: "verify" | "reset") {
  const subjectPart = kind === "verify" ? "verify" : "reset";
  const expectedPath = kind === "verify" ? "/verify-email" : "/reset-password";
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    let names: string[];
    try {
      names = (await readdir(mailbox)).filter((name) => name.endsWith(".json"));
    } catch {
      throw new Error("Private local mail fixture is unavailable");
    }
    for (const name of names) {
      let message: LocalMailMessage;
      try {
        const raw = await readFile(pathToFileURL(join(mailbox, name)), "utf8");
        message = JSON.parse(raw) as LocalMailMessage;
      } catch {
        throw new Error(
          "Private local mail fixture contains an unreadable message",
        );
      }
      if (
        message.recipient?.toLowerCase() !== recipient.toLowerCase() ||
        !message.subject?.toLowerCase().includes(subjectPart) ||
        !message.text
      ) {
        continue;
      }
      const firstLine = message.text.split(/\r?\n/, 1)[0];
      let link: URL;
      try {
        link = new URL(firstLine);
      } catch {
        throw new Error("Private local action message is malformed");
      }
      if (link.origin === appURL.origin && link.pathname === expectedPath)
        return link;
    }
    await delay(100);
  }
  throw new Error(
    `Timed out waiting for a local ${kind} message for the generated test address`,
  );
}

async function redactChallengeFailure<T>(
  label: string | (() => string),
  work: () => Promise<T>,
): Promise<T> {
  try {
    return await work();
  } catch {
    throw new Error(
      `${typeof label === "function" ? label() : label} browser step failed; private challenge data redacted`,
    );
  }
}

test("@auth-password registration, verification, sign-in and reset remain one-time", async ({
  page,
}) => {
  const health = await page.request.get(new URL("/readyz", appURL).toString());
  expect(
    health.ok(),
    "the generated application must be ready before browser checks",
  ).toBeTruthy();

  const email = `t63-${randomUUID()}@example.test`;
  await page.goto(new URL("/signup", appURL).toString());
  await expect(page.getByLabel("Email")).toHaveAttribute(
    "autocomplete",
    "email",
  );
  await expect(page.getByLabel("Password")).toHaveAttribute(
    "autocomplete",
    "new-password",
  );
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(signupPassword);
  const signupResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/signup",
  );
  await page.getByRole("button", { name: "Create account" }).click();
  const signupStatus = (await signupResponse).status();
  expect(
    signupStatus,
    "registration must reach its generic acknowledgement",
  ).toBe(202);
  await expect(
    page.getByRole("heading", { name: "Check your email" }),
  ).toBeVisible();

  const verification = await actionURL(email, "verify");
  await page.goto(new URL("/signin", appURL).toString());
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(signupPassword);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(
    page.getByText("Email or password was not accepted."),
  ).toBeVisible();

  await redactChallengeFailure("Verification", async () => {
    await page.goto(verification.toString());
    await expect(
      page.getByRole("button", { name: "Confirm email" }),
    ).toBeVisible();
    const scannerPreview = await page.request.head(verification.toString());
    expect(scannerPreview.ok()).toBeTruthy();
    await page.goto(new URL("/signin", appURL).toString());
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password").fill(signupPassword);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(
      page.getByText("Email or password was not accepted."),
    ).toBeVisible();

    await page.goto(verification.toString());
    // Never emit proof-bearing request headers in diagnostics.
    const verificationPost = page.waitForRequest((request) => request.method() === "POST" && new URL(request.url()).pathname === "/verify-email");
    // Neither scanner-style GET nor HEAD verifies; only this explicit POST may.
    await page.getByRole("button", { name: "Confirm email" }).click();
    const headers = (await verificationPost).headers();
    expect(headers.origin === appURL.origin, "native confirmation retains exact Origin").toBeTruthy();
    expect(headers.referer === appURL.origin + "/", "referrer excludes verification path and proof").toBeTruthy();
    await expect(
      page.getByRole("heading", { name: "Email verified" }),
    ).toBeVisible();
  });

  await page.goto(new URL("/signin", appURL).toString());
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill("incorrect-password");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(
    page.getByText("Email or password was not accepted."),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/auth$/);

  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(signupPassword);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/todos(?:\?.*)?$/);
  await expect(page.getByRole("heading", { name: "Todos", exact: true })).toBeVisible();

  await page.goto(new URL("/forgot-password", appURL).toString());
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Send reset instructions" }).click();
  await expect(
    page.getByRole("heading", { name: "Request received" }),
  ).toBeVisible();

  const reset = await actionURL(email, "reset");
  let resetForm = "";
  await redactChallengeFailure("Reset completion", async () => {
    await page.goto(reset.toString());
    await expect(page.getByLabel("New password")).toHaveAttribute(
      "autocomplete",
      "new-password",
    );
    resetForm = await page.locator("form").evaluate((form) => form.outerHTML);
    await page.getByLabel("New password").fill(replacementPassword);
    await page.getByRole("button", { name: "Change password" }).click();
    await expect(page).toHaveURL(/\/signin\?message=password-reset$/);
    await expect(page.getByRole("heading", { name: "Sign in", exact: true })).toBeVisible();
  });
  let replayStage = "Back navigation";
  await redactChallengeFailure(() => replayStage, async () => {
    // Completion redirects to GET. Back may skip the redirect source entry.
    await page.goBack();
    replayStage = "Revisit original reset proof after Back";
    await page.goto(reset.toString());
    await expect(page.getByRole("heading", { name: "Reset link unavailable", exact: true })).toBeVisible();
    await expect(page.getByLabel("New password")).toHaveCount(0);
    // no-store may refetch the spent link. Reconstruct the previously rendered
    // form to test an actual native stale-form POST without exposing its proof.
    replayStage = "Restore previous reset form";
    await page.evaluate((form) => { document.body.innerHTML = form; }, resetForm);
    await expect(page.getByLabel("New password")).toBeVisible();
    await page.getByLabel("New password").fill("Third-Horse-7!example");
    replayStage = "Stale form native submission";
    await page.getByRole("button", { name: "Change password" }).click();
    replayStage = "Stale proof rejection result";
    await expect(
      page.getByText(/invalid, expired or already used/i),
    ).toBeVisible();
    await expect(page.locator("input[name='password']")).toHaveCount(0);
  });

  await page.goto(new URL("/signin", appURL).toString());
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(signupPassword);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(
    page.getByText("Email or password was not accepted."),
  ).toBeVisible();
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(replacementPassword);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/todos(?:\?.*)?$/);
});
