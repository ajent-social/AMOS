# Native sign-in return policy: scoped verification

Design PR #116 landed at `ca4ba4b2b4ea95ba1217b6e5885a7da9a752c6a5`;
its tree matched the independently reviewed design candidate. This receipt
accompanies the supplemental T6.3 source repair; independent source reviewer auth_theme_design_review approved
`ed7545c3489224dd8c6c88bf8321c57a88588af9` with no blocking findings. PR #117
landed `97679c97983993f5fdb500481702f6bdf62e1aa9`, full reviewed-tree equality.
Fresh landed scoped race passed 75 named tests/subtests with zero skips, affected
vet passed, and both no-JavaScript navigation browser tests passed again.

## Obtained evidence

- Go 1.27.1/Linux ARM64, isolated PostgreSQL 16.15. Final scoped race suite
  passed 75 named tests/subtests, zero skips. Both legacy and themed NewLocal
  lifecycles passed. The configured host signs in directly to a supplied-session
  protected consumer route, retains constructor-owned policy after caller
  mutation, and denies that destination after signout. No todos redirect alias
  is created for this test destination.
- Unit cases cover policy validation/bounds/deep-copy, exact allowed selection,
  unsafe/unknown/prefix/query/fragment/escaped destinations, failed/missing
  credentials, Origin rejection, duplicate form rejection, legacy golden and
  all existing presentation cases. Control-containing form values retain the
  existing parser's 400 behavior; an initial test expecting fallback was corrected
  without changing that parser. The final aggregate and affected tests passed.
- A compiled isolated mutation trusting arbitrary nonempty next values failed
  the unsafe-selection test on the expected assertion. Original source bytes
  were restored and the positive check passed.
- Two real Chromium 155 no-JavaScript tests passed at 1280px and 390px, covering
  safe hidden next values and submitted form navigation for default, allowed and
  unsafe destinations. Two existing theme browser tests also passed in nil-policy
  mode. Browser fixture backend statuses/destination sentinels are synthetic;
  actual session/authorization evidence is the separate PostgreSQL suite.
- Affected vet passed. Full UI and browser-fixture lint passed with zero issues.
  Apphost changed-code lint passed with zero new issues relative to the design
  baseline. Full affected-package lint was attempted: it reports two existing
  unchecked response closes in `apphost/local_billing_integration_test.go`,
  unchanged by this repair. A nonsemantic boolean-local cleanup in the shared
  canonical-path helper resolves its own new staticcheck suggestion; validation
  tests were rerun. No billing file or lint policy was changed to hide findings.
- Formatting, changed-artifact public scan, bounded current-source checker,
  in-memory projection and diff checks passed. No plan admission, provider,
  production, organization or Desktop qualification follows from these checks.
- The previously reported broader host-suite prerequisites (TLS runtime profile
  and organization-browser configuration) remain unqualified. This repair does
  not change those tests or claim a green complete-host suite.

## Reproduction

With an operator-configured isolated real database and owner-local Go caches:

```sh
go test -race ./ui/authpassword ./apphost \
  -run '^(TestT6_3_|TestTheme|TestNilTheme|TestReturnPolicy|TestLocal)' \
  -count=1 -json
go vet ./ui/authpassword ./apphost ./ui/authpassword/testdata/browser
golangci-lint run ./ui/authpassword ./ui/authpassword/testdata/browser
golangci-lint run --new-from-rev=ca4ba4b2b4ea95ba1217b6e5885a7da9a752c6a5 ./apphost
```

Navigation-only browser fixture, with a five-minute lifetime:

```sh
AMOS_AUTH_RETURN_FIXTURE=1 go run ./ui/authpassword/testdata/browser
```

With installed Chromium and owned output configured by the project's usual
browser environment variables:

```sh
AMOS_AUTH_THEME_BASE_URL=http://127.0.0.1:18674 \
  npx --prefix tests/browser playwright test \
  --config tests/browser/auth-return.playwright.config.ts
```

Run the existing auth-theme browser config with the fixture flag absent to check
nil-policy presentation. No real account credentials or external services are
used by these presentation/navigation fixtures.
