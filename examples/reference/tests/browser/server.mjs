import { spawn } from "node:child_process";

const binary = process.env.AMOS_REFERENCE_BROWSER_BINARY;
if (!binary) {
  throw new Error("AMOS_REFERENCE_BROWSER_BINARY must point to the compiled test-only SQL browser host");
}
if (!process.env.AMOS_TEST_DATABASE_URL) {
  throw new Error("AMOS_TEST_DATABASE_URL is required for the real SQL browser host");
}

const child = spawn(binary, ["-test.run=^TestServeReferenceBrowser$"], {
  env: process.env,
  stdio: "inherit",
});
let stopping = false;
function stop(signal) {
  if (stopping) return;
  stopping = true;
  child.kill(signal);
}
process.on("SIGINT", () => stop("SIGINT"));
process.on("SIGTERM", () => stop("SIGTERM"));
child.on("error", (error) => {
  process.stderr.write(`reference browser host failed to start: ${error.message}\n`);
  process.exitCode = 1;
});
child.on("exit", (code, signal) => {
  process.exitCode = signal ? 1 : (code ?? 1);
});
