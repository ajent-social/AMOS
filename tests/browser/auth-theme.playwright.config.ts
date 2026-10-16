import { defineConfig } from "@playwright/test";
const outputDir = process.env.AMOS_BROWSER_ARTIFACT_DIRECTORY;
if (!outputDir)
  throw new Error(
    "AMOS_BROWSER_ARTIFACT_DIRECTORY is required for owned browser output",
  );
export default defineConfig({
  testDir: ".",
  testMatch: "auth-theme.spec.ts",
  workers: 1,
  retries: 0,
  timeout: 30_000,
  outputDir,
  reporter: "list",
  use: {
    headless: true,
    browserName: "chromium",
    screenshot: "off",
    trace: "off",
    video: "off",
    launchOptions: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH
      ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH }
      : {},
  },
});
