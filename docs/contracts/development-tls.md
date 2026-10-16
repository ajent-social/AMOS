# Explicit development HTTPS testing profile

Amendment v1.32 adds a separate owner-operated development testing path. It does
not widen existing `NewLocal`, the initializer, runtime manifest, config/schema,
`amos dev`, production-target or deployment/provider contracts. This supplemental
owner-requested LAN outcome is not a new canonical product task or E16 acceptance.
Independent design review and landing precede implementation.

## Public composition and transport

Add `apphost.NewDevelopmentTLS(ctx, DevelopmentTLSConfig)` returning `*Host`.
The config contains `Local LocalConfig`, `CertificatePEM []byte` and
`PrivateKeyPEM []byte`. `Local.Origin` must be canonical HTTPS with an explicit
numeric port and either a literal RFC1918 IPv4 address or exactly127.0.0.1 for
local TLS verification. No DNS, public/unspecified/multicast address, IPv6, zone,
userinfo, query, fragment, path, implicit port or alternate textual spelling.
The profile is deliberately IPv4-only. Reject optional Billing in this profile;
it is not live billing composition. Retain optional theme/return/metrics validation.

Add `delivery/email.ParseDevelopmentTLSOrigin` for this exact shared grammar and
`localcapture.NewDevelopmentTLS` for the same explicit profile. Original origin
and capture constructors retain their behavior. Capture stays private local files,
never a network mailbox endpoint or SMTP/live-delivery qualification.

Parse fresh copied PEM bytes to immutable server identity before opening any
DB or mailbox. Require matching key, one leaf identity, current validity, explicit
server-auth usage and the exact origin IP SAN (no DNS-only match). Reject missing,
malformed, expired/not-yet-valid or wrong-origin/key identity generically. No
caller tls.Config, signer, callback, trust override or fallback is accepted. TLS
minimum1.3. Use secure session mode (`CookieSecure:true`, loopback exception false)
with the existing __Host cookie, exact Origin/Referer and session-bound CSRF.
Other origin-aware services receive HTTPS without the HTTP-loopback exception.

`Host.Serve` for this mode accepts only a plain `*net.TCPListener` bound to the
exact origin IP and port; it owns TLS wrapping. Reject mismatched/wildcard binds
and caller-supplied TLS/listener wrappers before serving. Enforce exact HTTP Host
and TLS before dispatching any application/health/auth route. Forwarded headers
never establish origin, authority or client identity. No HTTP redirect listener,
reverse proxy rewrite, insecure cookie, plaintext serving fallback or automatic
trust-store change. Enforce current certificate validity at handshake/serving;
restarting with new material is the supported certificate rotation path.

One active serve lifecycle owns its mail worker, cancellation and bounded graceful
HTTP shutdown, force-closing after the deadline. Reuse existing shared runtime
serving where practical; a private HTTPS server using the composed App handler
must preserve its logging, route reservations and all auth middleware. Concurrent
serve attempts fail without stealing the first listener. Closing dependencies
retains the existing explicit owner lifecycle.

## Concrete development executable

Add the separate `cmd/amos-devtls` executable for the reference personal todo
application. This supported narrow path consumes an existing migrated database;
it never initializes, migrates, resets or discovers another application's data.
It is not generated `amos dev` LAN support. The initial executable is supported
on Linux only; the public typed host remains portable. Retain a verified runtime
directory descriptor for the entire host lifetime and resolve the capture child
through that pinned descriptor (`/proc/self/fd`), so renaming/replacing its parent
cannot redirect capture. Unsupported executable platforms fail before dependencies. The private directory is selected
explicitly by `--runtime-dir`; no implicit cwd or environment credential fallback.

Use a pinned owner-only real directory0700, no symlink, current effective owner.
Within it require regular owner-only0600 files `runtime.json`, `certificate.pem`,
`private-key.pem`, and an existing private0700 `mail` directory. Open through
os.Root, validate Lstat/fstat identity and ownership/modes, reject symlinks and
escapes. Runtime JSON is closed, exact-key, duplicate-decoded-key rejecting,
max8192 bytes, with exact version `amos-development-tls-v1` and fields:
`version`, `installation_id`, `application_id`, `environment_id`, `origin`,
`database_url`, `material_key`, `rate_key`. IDs are canonical UUIDv7, keys are
32-byte lowercase hex. Certificate/key are bounded64KiB each. No secret/path/input
value is printed on error; generic diagnostics and nonzero exit only. No auto
creation, permissions repair or state overwrite. Config material is private
operational state and must never be committed. All configuration/certificate
material is validated before dependency startup or listening.

Reuse the existing reference business service/UI and personal-workspace selection,
not a visual substitute or authority bypass. The binary binds only the configured
address, reports generic ready/startup status, handles SIGTERM/interrupt and closes
owned listener/host. Readiness must be probed with normal TLS verification and a
fresh client trust pool containing the public certificate; no redirects or
InsecureSkipVerify. Owner retains private capture for verification during testing.

## Required evidence and bounded trial

Actual PostgreSQL plus real TLS requests must exercise signup, captured email
verification, signin, workspace/todo mutation and signout. Assert secure host-only
cookie attributes, CSRF denial, wrong/duplicate Origin/foreign Referer denial,
Host mismatch and forwarded-header spoof denial. Negative identity/address/bind
cases must fail before dependencies where applicable. Mutating caller certificate
slices after construction cannot alter serving identity. Verify plaintext clients
cannot obtain application content. Existing HTTP-loopback lifecycle remains green.
A compiled mutation removing secure-cookie or exact-listener enforcement must make
the corresponding regression fail, then exact restoration must pass.

After independent source review/guarded merge and fresh landed checks, use only
existing owner-authorized host capacity. Verify actual assigned private IPv4 and
unused application/database ports. Create a fresh private runtime, IDs, independent
keys, short-lived IP-SAN certificate, separately named loopback-only PostgreSQL
container and fresh data; migrate only this new owned database using the reviewed
registry. Keep existing consumers, containers, listeners, backups and credentials
untouched. Record owned PID/service, startup deadline and stop/restart instructions.
Keep real addresses/paths outside tracked artifacts. No public DNS, new cloud,
production cutover, trust-store edits or spend.

Mac-to-host certificate-verifying readiness and account requests prove LAN TLS
and finite service behavior. Wrong trust/IP must fail. Self-signed certificate
files do not establish system/browser trust. Browser interstitial and actual
browser interaction are separate evidence; never claim warning-free browser,
production, organization, provider, billing or release acceptance from this trial.

## Ownership

The integrator owns this amendment and supplemental task evidence. Source delegation
may own apphost shared private composition/new TLS files and tests, delivery/email
origin/capture additions, plus the separate development executable/tests. No root
module, migrations, existing initializer/schema/devrunner/template change, plan
rewrite, default listener or other task takeover. Acceptance records source, actual
PG/TLS, LAN and browser observations separately.
