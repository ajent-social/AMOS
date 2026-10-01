import path from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

const require = createRequire(path.resolve(process.cwd(), "package.json"));
const { defineConfig } = require("@playwright/test");
const here = path.dirname(fileURLToPath(import.meta.url));
const host = process.env.AMOS_REFERENCE_BROWSER_HOST ?? "127.0.0.1";
const port = Number(process.env.AMOS_REFERENCE_BROWSER_PORT ?? "4187");
const binary = process.env.AMOS_REFERENCE_BROWSER_BINARY;
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  throw new Error("AMOS_REFERENCE_BROWSER_PORT must be an integer from 1 through 65535");
}
if (!binary) {
  throw new Error("AMOS_REFERENCE_BROWSER_BINARY must point to the compiled test-only SQL browser host");
}

export default defineConfig({
  testDir: here,
  testMatch: "reference.spec.mjs",
  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  reporter: "list",
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
  use: {
    baseURL: `http://${host}:${port}`,
    headless: true,
    trace: "off",
    screenshot: "off",
    video: "off",
  },
  webServer: {
    command: `node ${JSON.stringify(path.join(here, "server.mjs"))}`,
    url: `http://${host}:${port}/_test/info`,
    env: {
      AMOS_REFERENCE_BROWSER_HOST: host,
      AMOS_REFERENCE_BROWSER_PORT: String(port),
      AMOS_REFERENCE_BROWSER_BINARY: binary,
      AMOS_REFERENCE_BROWSER_SERVE: "1",
    },
    reuseExistingServer: false,
    timeout: 30_000,
  },
});
