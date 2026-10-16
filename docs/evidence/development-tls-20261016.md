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

## Landed integration

Independent source/final receipt review approved the complete candidate. PR #132
landed `d3172533c2acc252adae20ac92a55730e3672f4d`; its whole tree equals the
reviewed candidate. Fresh landed named checks again passed32host+49origin/capture/
executable=81, zero skips; bounded plan/projection checks passed. No hosted CI or
full apphost claim follows. Initial standalone executable is Linux-only, retaining
its verified directory descriptor; no native Mac Go build qualification.

## Isolated runtime and actual LAN requests

The owner-authorized existing host ran a freshly built landed executable, fresh
private runtime/IDs/keys, short-lived seven-day IP-SAN ECDSA certificate and fresh
separately named PostgreSQL container/volume. Database exposure is loopback-only;
the application binds the assigned RFC1918 address exactly on a previously unused
port. Fresh resources have unpredictable ownership tokens; volume creation identity
and token were verified before migration and rechecked after startup. Only the
new database received the reviewed existing generator registry. Existing consumer
preview, listeners, private configuration and persistent data remained untouched.
The database is capped at512MiB/one CPU. No new cloud, public DNS or trust-store edit.

The independently reviewed private setup helper initially failed on a Python file
API before writing configuration or starting any database, volume or process.
Only two empty owned private directories were created; both were verified empty
and removed. Corrected exclusive/private built-in file creation was smoke-tested
and independently cleared. The retry completed, with direct certificate-verified
readiness and a detached owned service. Current exact PID/start-time/executable
identity and built-binary digest were rechecked after the launch session ended;
container/volume ownership remained intact. A real wrong database password was
rejected; database TLS and restricted runtime-role qualification remain separate.

From a separate Mac, real direct TLS1.3 requests with only the copied public
certificate completed signup, captured verification, secure signin, personal todo
creation/persistence, all three authenticated UI assets and signout/revocation.
Five native auth HEAD responses returned200, empty bodies and no cookies. Precise
certificate-verification failures proved ordinary trust and wrong-IP rejection;
wrong Host with forwarded spoof, plaintext, missing CSRF and foreign Origin were
denied. No insecure verification, environment proxy or browser trust change.
The public certificate was copied; private TLS keys and database credentials
stayed with the runtime. Verified test-account details and exact-owned stop/restart
instructions are in private owner-only handoff files, not public artifacts.

## Remaining limits

The UI tool reported no browser available. Actual visual/native browser acceptance
and self-signed browser trust remain unverified; certificate-verifying HTTP is not
browser acceptance. Self-signed material needs an owner's separate browser decision;
no agent bypassed a browser security warning. The service has no automatic crash
restart and the certificate is short-lived; this is an explicit testing profile.
Mail remains private capture, not live email. Production constructor, organization
policy/authority, live billing/provider behavior, cloud deployment and E16 admission
remain open. The broader missing runtime/browser test prerequisites above remain.
Requested batch ceiling10; one coherent supplementary runnable testing outcome;
inherited model/settings preserved, exact cost/token telemetry unavailable.
