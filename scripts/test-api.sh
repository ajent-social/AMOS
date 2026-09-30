#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
mode="${1:-unit}"
if (($# > 0)); then
  shift
fi
if (($# > 0)); then
  printf 'usage: %s [unit|integration|providers]\n' "$0" >&2
  exit 2
fi

log_file="$(mktemp "${TMPDIR:-/tmp}/amos-api-test.XXXXXX")"
trap 'rm -f "$log_file"' EXIT

run_go_tests() {
  local status=0
  if go test -v -count=1 "$@" >"$log_file" 2>&1; then
    status=0
  else
    status=$?
  fi
  cat "$log_file"
  if ! grep -Eq '^=== RUN[[:space:]]' "$log_file"; then
    printf 'API test selection matched zero tests.\n' >&2
    return 1
  fi
  return "$status"
}

cd "$ROOT"
case "$mode" in
  unit)
    run_go_tests ./tests/harness
    ;;
  integration)
    if [[ -z "${AMOS_TEST_DATABASE_URL:-}" ]]; then
      printf 'AMOS_TEST_DATABASE_URL is required for the API integration suite.\n' >&2
      exit 2
    fi
    run_go_tests -tags=integration ./tests/harness
    ;;
  providers)
    if [[ "${AMOS_RUN_PROVIDER_TESTS:-}" != "1" ]]; then
      printf 'Provider tests are opt-in; set AMOS_RUN_PROVIDER_TESTS=1 to request them.\n' >&2
      exit 2
    fi
    printf 'Provider test suite is unavailable until real provider adapters and provider-qualified tests exist.\n' >&2
    exit 2
    ;;
  *)
    printf 'unknown API suite: %s\nusage: %s [unit|integration|providers]\n' "$mode" "$0" >&2
    exit 2
    ;;
esac
