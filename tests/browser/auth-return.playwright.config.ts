import { defineConfig } from "@playwright/test";
import presentation from "./auth-theme.playwright.config";
export default defineConfig({
  ...presentation,
  testMatch: "auth-return.spec.ts",
});
