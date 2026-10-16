# Native auth presentation: scoped verification

Design: PR #114, landed `a3c28e7cafb14fe6e46c05924cf93bc9fb027582`.
Source candidate: this receipt accompanies the supplemental T6.14 repair.
Independent source review approved `afa8ff234a67517a65de82e4208a72c19487dd66`
against the design baseline. Reviewer: auth_theme_design_review, no blocking
findings. Reviewer inspected all 13 files and formatting/diff checks; execution
evidence remains the author’s. Final evidence-only head `a2b4991c2ae791e7ec2b821feba31ddee37ffe48` was
independently approved. PR #115 merged by guarded rebase to
`93b04a67f442d676e96763de044e7299e4c3004f`; landed tree equals reviewed tree.
Fresh landed scoped race checks passed (59 named tests/subtests, zero skips),
affected vet passed and both tracked no-JavaScript browser tests passed again. Historical
product acceptance is unchanged.

## Actual observations

- Go 1.27.1, Linux ARM64, isolated PostgreSQL 16.15 development service.
- Scoped race command passed both packages: 59 named tests/subtests, zero skips.
  Both legacy and themed NewLocal signup, captured verification, sign-in,
  personal todo, TOTP/CSRF and signout flows passed against real PostgreSQL.
- Themed host retains a copied brand after caller mutation; malformed theme is
  rejected before dependency opening. Native auth unit cases preserve status,
  response controls, form actions/fields/challenges and byte-identical legacy
  signin output. Tests cover controls/UTF-8, escaping and canonical local assets.
- `go vet` passed for the affected packages. After test-input refactoring to
  explicit form encoding, affected unit/invalid-host checks were rerun.
- Isolated mutation disabling only asset validation compiled and failed
  `TestThemeRejectsNoncanonicalAssets` on the expected accepted-path assertion.
  Original source bytes were restored and the positive check passed. An earlier
  mutation failed compilation and was rejected as negative behavioral evidence.
- Real Chromium 155 presentation checks passed at 1280px and 390px with
  JavaScript disabled: actual native page markup, seven lifecycle/invalid-link
  screens per viewport, CSS/logo loading, escaped script-shaped brand, no
  horizontal overflow, keyboard focus and themed failed sign-in without echoed
  password. Two tracked Playwright tests passed. Screenshots were inspected.
  Browser fixture actions are synthetic statuses, not real identity backends;
  actual PostgreSQL host behavior is the separate evidence above.
- Broader `go test -race ./ui/authpassword ./apphost` was attempted and failed
  visibly at `TestRuntimeBindingRequiredService` (required TLS profile absent)
  and `TestWorkspaceSwitchBrowserUsesCurrentMembership` (required installed
  browser configuration absent on that host). It is not reported passing.
  Those missing profiles and organization-browser acceptance remain outside
  this bounded presentation repair; no tests were changed to skip them.
- Changed-artifact public scan, formatting, diff check, current-source plan
  checker and in-memory projection check passed. Plan checks do not authenticate
  runtime admission or release receipts. Full release qualification is open.

## Reproduction

Use an isolated real PostgreSQL database with `AMOS_TEST_DATABASE_URL` set by
the operator; no provider credentials are needed. Use an owner-local Go cache.

```sh
go test -race ./ui/authpassword ./apphost \
  -run '^(TestT6_3_|TestTheme|TestNilTheme|TestLocal)' -count=1 -json
go vet ./ui/authpassword ./apphost ./ui/authpassword/testdata/browser
python3 scripts/check-plan.py
python3 scripts/render-plan.py --check
```

Presentation-only browser fixture (fixed loopback origin, five-minute lifetime):

```sh
go run ./ui/authpassword/testdata/browser
```

In another terminal, with installed Chromium and owner-local browser output
configured using the standard project environment variables:

```sh
AMOS_AUTH_THEME_BASE_URL=http://127.0.0.1:18674 \
  npx --prefix tests/browser playwright test \
  --config tests/browser/auth-theme.playwright.config.ts
```

No external email delivery, TLS production composition, consumer-specific visual
approval, organization/financial authority, Desktop bridge, deployment or model
spend is qualified by this evidence.
