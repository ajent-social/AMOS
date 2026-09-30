#!/usr/bin/env bash
# Required Go gates. Shared hosts must hold their external build lease first.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
mode="${1:-}"
if [[ $# != 1 ]]; then
  printf 'usage: %s <fmt|lint|unit|integration>\n' "$0" >&2
  exit 2
fi
case "$mode" in
  fmt)
    dirty=0
    while IFS= read -r -d '' source; do
      if [[ -n "$(gofmt -l "$source")" ]]; then
        printf 'gofmt required: %s\n' "$source" >&2
        dirty=1
      fi
    done < <(git ls-files -co --exclude-standard -z -- '*.go')
    exit "$dirty"
    ;;
  lint)
    if ! command -v golangci-lint >/dev/null 2>&1; then
      printf 'golangci-lint 2.13.2 is required; see docs/development.md.\n' >&2
      exit 2
    fi
    if [[ "$(golangci-lint version)" != *'version 2.13.2 '* ]]; then
      printf 'golangci-lint must be pinned to 2.13.2.\n' >&2
      exit 2
    fi
    exec golangci-lint run --config .golangci.yml ./...
    ;;
  unit|integration)
    if [[ -z "${AMOS_TEST_DATABASE_URL+x}" ]] || [[ -z "$AMOS_TEST_DATABASE_URL" ]]; then
      printf 'AMOS_TEST_DATABASE_URL is required; durable tests never silently skip.\n' >&2
      exit 2
    fi
    if [[ "$mode" == integration ]]; then
      exec go test -tags=integration ./... -count=1
    fi
    exec go test ./... -count=1
    ;;
  *)
    printf 'unknown Go gate: %s\n' "$mode" >&2
    exit 2
    ;;
esac
