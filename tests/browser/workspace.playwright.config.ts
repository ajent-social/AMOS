import { defineConfig } from "@playwright/test";

const executablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;
const outputDir = process.env.AMOS_WORKSPACE_PLAYWRIGHT_OUTPUT_DIR;
if (!executablePath) {
  throw new Error(
    "PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH must name the installed Chrome executable",
  );
}
if (!outputDir) {
  throw new Error(
    "AMOS_WORKSPACE_PLAYWRIGHT_OUTPUT_DIR must point to disposable external artifacts",
  );
}

export default defineConfig({
  testDir: ".",
  testMatch: "workspace-switch.spec.ts",
  outputDir,
  reporter: "list",
  workers: 1,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  use: {
    browserName: "chromium",
    launchOptions: {
      executablePath,
      headless: true,
      args: [
        `--unsafely-treat-insecure-origin-as-secure=${process.env.AMOS_WORKSPACE_TEST_BASE_URL}`,
      ],
    },
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
});
