#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BROWSER_DIR="$ROOT/tests/browser"
suite="${1:-}"
if [[ -z "$suite" ]] || (($# != 1)); then
  printf 'usage: %s <fixtures|ui-shell|reference|initializer|generated-app|upgrade|personal-billing>\n' "$0" >&2
  exit 2
fi

case "$suite" in
  fixtures)
    grep_pattern='@fixtures'
    ;;
  ui-shell)
    grep_pattern='@ui-shell'
    ;;
  reference)
    export AMOS_REFERENCE_BROWSER_SERVE=1
    export AMOS_REFERENCE_BROWSER_ROOT="$ROOT"
    if [[ -z "${AMOS_TEST_DATABASE_URL-}" ]]; then
      printf 'AMOS_TEST_DATABASE_URL is required for reference browser tests.\n' >&2
      exit 2
    fi
    if [[ -z "${AMOS_REFERENCE_BROWSER_BINARY:-}" ]]; then
      printf 'AMOS_REFERENCE_BROWSER_BINARY must point to the compiled test-only reference host.\n' >&2
      exit 2
    fi
    browser_config="$ROOT/examples/reference/tests/browser/playwright.config.mjs"
    grep_pattern='@reference'
    ;;
  initializer|generated-app|upgrade|personal-billing)
    printf 'Browser suite "%s" is reserved by task contracts but unavailable until its product UI and tests are implemented.\n' "$suite" >&2
    exit 2
    ;;
  *)
    printf 'unknown browser suite: %s\nusage: %s <fixtures|ui-shell|reference|initializer|generated-app|upgrade|personal-billing>\n' "$suite" "$0" >&2
    exit 2
    ;;
esac

browser_config="${browser_config:-$BROWSER_DIR/playwright.config.ts}"
runner="$BROWSER_DIR/node_modules/.bin/playwright"
if [[ ! -x "$runner" ]]; then
  printf 'Playwright is not installed for tests/browser; run npm ci --prefix tests/browser.\n' >&2
  exit 2
fi
if ! command -v node >/dev/null 2>&1; then
  printf 'Node.js is required for the browser fixture server.\n' >&2
  exit 2
fi

list_file="$(mktemp "${TMPDIR:-/tmp}/amos-browser-list.XXXXXX")"
trap 'rm -f "$list_file"' EXIT
cd "$BROWSER_DIR"
if ! "$runner" test --config="$browser_config" --project=chromium --grep "$grep_pattern" --list >"$list_file" 2>&1; then
  cat "$list_file"
  printf 'Unable to enumerate selected browser tests.\n' >&2
  exit 1
fi
cat "$list_file"
if ! grep -Eq '^Total: [1-9][0-9]* tests? in [1-9][0-9]* file' "$list_file"; then
  printf 'Browser suite "%s" selected zero tests.\n' "$suite" >&2
  exit 1
fi
"$runner" test --config="$browser_config" --project=chromium --grep "$grep_pattern"
