# Development HTTPS LAN testing qualification record

Owner explicitly requested a runnable LAN testing path and autonomous execution.
This supplemental outcome retains the original canonical product task inventory;
it does not claim production or E16 acceptance. The integrator owns its separate
claim. Existing application data, private configuration, listeners and consumer
previews must remain untouched.

## Design

PR #130 landed `76cce216b319e4abb6aa173b595fea2f7c4119b3`; its entire
landed tree equals the independently reviewed design. Bounded plan/projection,
diff and public-artifact checks passed. [ADR046](../adr/046-explicit-development-tls.md)
and [v1.32](../contracts/development-tls.md) freeze the additive typed host and
separate reference development executable. Existing initializer/generated,
HTTP-loopback and production profiles are not widened.

## Source qualification

Source base: `4eb91527501cd86ef9331ab1a7b00ecf15c76108`. Eleven source/test
files add the typed host, exact origin/private capture and separate executable.
No root module, migration, existing initializer/generated/profile or other task
claim changed. Initial unshipped config field spelling is `Application`; the
unchanged public scanner accepts the resulting Go selectors. Independent review
approved that spelling clarification without a version/migration change.

Linux ARM64, Go1.27.1, actual disposable PostgreSQL and real certificate-verifying
TLS1.3, exact integrated source `3b470452336da1a7b78545152e716607095587a7`:

```sh
go test -race ./apphost \
  -run '^(TestDevelopmentTLS.*|TestLocalSignupVerificationSigninPersonalTodoAndSignout|TestLocalAuthPageHeadIsReadOnly|TestMetrics.*)$' \
  -count=1 -json
go test -race ./delivery/email ./delivery/email/localcapture ./cmd/amos-devtls -count=1 -json
go vet ./apphost ./delivery/email ./delivery/email/localcapture ./cmd/amos-devtls
golangci-lint run ./apphost ./delivery/email ./delivery/email/localcapture ./cmd/amos-devtls
```

Integrated host32 plus origin/capture/executable49 named checks passed:81 total,
zero skips. Vet passed; lint reported zero issues. The actual compiled executable
completed signup, private captured verification, secure signin, personal workspace,
CSRF denial, todo persistence, all three authenticated assets, signout/revocation,
wrong-trust denial and bounded SIGTERM. Its unique database was dropped. The
trust-enabled fixture does not qualify database password authentication or DB TLS.
Existing legacy/themed HTTP-loopback lifecycle, HEAD and metrics checks passed.

Host checks exercise exact listener/Host, wrong trust/IP/TLS version, plaintext,
forwarded spoof and Origin/Referer/CSRF denial, copied PEM, concurrent serve ownership
and actual five-second forced shutdown. Reviewer found TLS resumption can skip
GetCertificate: real expired cached-session handshake was RED with resumed=true.
All-handshake validity gates plus per-request guard now reject both expired
resumption and requests on a previously resumed persistent connection. Tests
retain ordinary root/IP verification; only test-client time holds its ticket
eligible, with real server expiry. No insecure verification or trust-store edit.

Compiled exact-bind removal failed wildcard/wrong-loopback regressions; exact
restoration passed. Host author restored source hash before the reviewed field
spelling change: `fd5d815c23d78208c4b4972731d0a601a3085df261b2f137107d6b9f13e0e236`.
Final integrated TLS source hash:
`65f2083e008941d6485b5b73d0d52eca8c9ea7f648045dc909c3e1bba2ccdfe1`.
Compiled descriptor-custody mutation rerouted capture through the replaced parent
and failed its regression; exact restored source passed. CLI configuration source
hash: `0d1360cc5348e1bf4a07236cc6548aca55cd21d664c1c238e8e40f41eb56d94b`.

A broader author three-package race attempt recorded72 passes and two explicit
missing-prerequisite failures: AMOS_BINDING_BOUND_RUNTIME_TEST_CONFIG and
PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH. Neither prerequisite nor skip policy changed.
Do not call the full apphost suite, bound restricted TLS runtime or real browser
qualified. These separate existing gates remain unresolved; the named development
scope above passed with zero skips. No hosted CI claim.

## Integration and runtime gates

Final independent source review, guarded merge, reviewed-tree equality and fresh
landed checks remain required for this candidate. The initial standalone executable
is Linux-only, retaining its verified directory descriptor until host closure;
public typed host portability is an intended surface, not native Mac evidence.
Replacing the parent cannot redirect capture. Only after source gates may a fresh
isolated owned LAN trial start. A separately reviewed private provisioner verifies
fresh resource tokens/creation identity, exact PID/start-time cleanup and assigned
RFC1918 binding; it has not run yet. Existing consumer listeners/data stay intact.

Actual certificate-verifying LAN behavior and browser-visible acceptance will be
recorded separately. Self-signed material never establishes automatic system/browser
trust. No production constructor, organization authority, real email delivery,
live billing/provider or release admission is implied. Requested batch ceiling10;
one coherent supplementary runnable testing outcome; inherited model/settings
preserved, exact cost/token telemetry unavailable.
