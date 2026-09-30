import { defineConfig } from "@playwright/test";

const host = process.env.PLAYWRIGHT_FIXTURE_HOST ?? "127.0.0.1";
const port = Number(process.env.PLAYWRIGHT_FIXTURE_PORT ?? "4175");
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  throw new Error("PLAYWRIGHT_FIXTURE_PORT must be an integer from 1 through 65535");
}

const executablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;

export default defineConfig({
  testDir: ".",
  testMatch: "**/*.spec.ts",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  reporter: "list",
  projects: [
    {
      name: "chromium",
      use: { browserName: "chromium" },
    },
  ],
  use: {
    baseURL: `http://${host}:${port}`,
    headless: true,
    ...(executablePath ? { launchOptions: { executablePath } } : {}),
  },
  webServer: {
    command: "node fixtures/server.mjs",
    url: `http://${host}:${port}/health`,
    env: { PORT: String(port), HOST: host },
    reuseExistingServer: false,
    timeout: 15_000,
  },
});
