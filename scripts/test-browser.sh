#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BROWSER_DIR="$ROOT/tests/browser"
suite="${1:-}"
if [[ -z "$suite" ]] || (($# != 1)); then
  printf 'usage: %s <fixtures|reference|initializer|generated-app|upgrade|personal-billing>\n' "$0" >&2
  exit 2
fi

case "$suite" in
  fixtures)
    grep_pattern='@fixtures'
    ;;
  reference|initializer|generated-app|upgrade|personal-billing)
    printf 'Browser suite "%s" is reserved by task contracts but unavailable until its product UI and tests are implemented.\n' "$suite" >&2
    exit 2
    ;;
  *)
    printf 'unknown browser suite: %s\nusage: %s <fixtures|reference|initializer|generated-app|upgrade|personal-billing>\n' "$suite" "$0" >&2
    exit 2
    ;;
esac

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
if ! "$runner" test --config=playwright.config.ts --project=chromium --grep "$grep_pattern" --list >"$list_file" 2>&1; then
  cat "$list_file"
  printf 'Unable to enumerate selected browser tests.\n' >&2
  exit 1
fi
cat "$list_file"
if ! grep -Eq '^Total: [1-9][0-9]* tests? in [1-9][0-9]* file' "$list_file"; then
  printf 'Browser suite "%s" selected zero tests.\n' "$suite" >&2
  exit 1
fi
"$runner" test --config=playwright.config.ts --project=chromium --grep "$grep_pattern"
