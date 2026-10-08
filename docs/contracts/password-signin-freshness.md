# Password sign-in credential revalidation (v1.25 proposal)

Status: proposed bounded prerequisite; independent exact-head design review and
landing must precede a separate supplemental source claim. No source is admitted
by this draft. Baseline: `fa6c5a35e61ed4f866652acc66addec1d76fb368`.
This corrects one stale-password/contact window in
[Service.signIn](../../identity/login/login.go). It does not adopt the
[current-session reader](current-session-recheck.md), a whole-writer protocol,
new identity API or host authority.

## Problem and bounded decision

Today sign-in reads person, email and credential values in a transaction which
ends before password verification. It later calls `IssueForRequest`, which opens
a different transaction and does not revalidate the verified credential or
contact. Epoch checking alone cannot establish that the exact verified password
and email remain current: the stored verifier can change without an epoch change.

Retain the bounded password verification and optional compare-and-swap rehash
outside the issuance transaction. Bind that verification to exact credential,
email and person values, then revalidate those values under row locks in the
same caller-owned transaction as `IssueForRequestTx`. Publish cookie/CSRF/success
only after that transaction commits. Do not hash twice, parse a session cookie
in login, or add a configurable credential-verification callback.

No exported signature or schema changes. Only `Service.signIn` changes in the
existing source file; new focused tests and the narrowly affected existing
issuance-failure injection adapter are separately named in the source grant.
`register`, constructors, password implementation, session implementation and
existing migration bytes remain outside this repair.

## Exact algorithm

1. Preserve request parsing, address normalization, origin checks, password budget,
   generic unknown/wrong/unverified/inactive response and dummy verification.
   The initial scoped lookup additionally records exact email ID, comparison key,
   verified timestamp, credential ID and stored verifier hash. These are private
   request-local values; never log, serialize or attach them to a public context.
2. Call the existing `Passwords.Verify` exactly once. Preserve its current optional
   rehash behavior. The rehash CAS must include the exact recorded credential ID,
   person, method and old hash as well as its existing realm guard. Begin with
   `expectedHash = encoded`. Set it to the generated replacement hash only after
   the CAS transaction reports success, and use that value only when Verify
   reports `RehashPersisted`. Otherwise retain the verified original hash.
   A failed optional rehash does not invent a successful replacement receipt.
3. After successful known, active, verified password admission, derive an issuance
   context bounded to three seconds; an earlier caller deadline wins. Use exactly
   one `s.db.WithTx` with explicit READ COMMITTED and `ReadOnly: false` for current
   row validation plus session staging. Before row acquisition, require actual
   `transaction_isolation = read committed` and `transaction_read_only = off`
   through fixed `pg_catalog.current_setting` queries; mismatch is unavailable.
   No nested transaction, retry or savepoint
   recovery occurs inside this attempt.
4. In separate statements lock the recorded scoped person `FOR SHARE`, then the
   exact email `FOR SHARE`, then the exact credential `FOR SHARE`. Every lookup
   binds its expected IDs and person/realm relationship. The email must still
   have the exact recorded comparison key and verified timestamp instant
   (compare instants, not Go time.Time structural representation); the credential
   must still be `email_password`, unrevoked and have `expectedHash`. The person
   must still be active with the originally observed security epoch. Compare
   values read after each lock wait. Any missing/changed binding is stale proof.
   Malformed persisted values and SQL/scan failures are dependency failures.
5. After all three locks, obtain one database `pg_catalog.clock_timestamp()`.
   Construct the existing internal `VerifiedCredential` with that instant,
   original bound realm/person/epoch, method `email_password`, aal1 and the
   existing twelve-hour absolute bound. Use the existing identity-internal
   constructor; no public principal or proof constructor is added.
6. Call the existing `Sessions.IssueForRequestTx` with this same context and
   transaction. Session code alone parses the prior configured cookie and stages
   its existing rotation. Moving from `IssueForRequest` (first matching cookie)
   to this helper explicitly adopts rejection of duplicate configured session
   cookies. That helper error follows the unchanged non-freshness mapping: generic
   HTTP503 `dependency.unavailable`, no cookie/CSRF/success and transaction rollback.
   Preserve malformed, foreign-realm and single-cookie behavior; no session
   implementation change is required. Retain the person/email/credential locks until the
   owning transaction completes. No caller may replace the supplied transaction
   or session service; existing trusted same-database/configuration composition
   remains required.
7. Return the staged result only when `WithTx` reports success. A local private
   stale-proof sentinel maps to the existing generic HTTP401
   `auth.unauthenticated` body with no cookie or CSRF value. Query, mode, timeout,
   cancellation, session staging and completion failures map to the existing
   HTTP503 `dependency.unavailable` response. Existing non-freshness error
   mapping is preserved. Do not publish headers/cookies/body from the callback.

The three-second bound applies to the new issuance attempt, not a claimed bound
on the entire request, initial lookup or password work. The accepted verification
receipt never survives this request. A success followed by a later ordinary
password/epoch revocation is serialized after this attempt and must be rejected
by subsequent session authentication under existing rules.

## Deliberate limits and compatibility

The optional rehash may commit before issuance is denied or fails. This preserves
existing behavior: rehash replaces the representation of the same verified
password, not its authority. Do not claim that rehash is atomic with issuance or
rolls back with it. The final expected-hash comparison still rejects a competing
replacement, including one which leaves security_epoch unchanged.

This bounded lock chain does not establish compatibility with every existing
writer. In particular, contact-before-person email confirmation, joined locking
in recovery and cross-person old-cookie rotation remain complete-graph gates.
A database-detected deadlock aborts the whole issuance attempt and maps to503;
there is no hidden retry or partial success. Such a failure is not a passing
liveness schedule. A separately adopted writer protocol must resolve those
orders before composed host/current-authority qualification.

No claim is made of complete producer finalization after arbitrary deferred
work, full session idle/assurance completion freshness, resource authority or
physical client-receipt-time validity. This slice only establishes that the
credential/contact/person binding which justified password verification is
revalidated after acquisition waits and remains locked through staged issuance
and successful completion. It neither reduces the selected authentication suite
nor enables a host with unsupported routes removed.

## Required verification

Use the exported login HTTP handler, real password verification, actual session
service and real TLS PostgreSQL under a fresh exact reviewed runtime-role fixture.
Observe two-connection schedules using bounded channels and database wait evidence,
not sleeps. A forwarding TxRunner may pause before the issuance callback and may
inject completion failure; record this test mechanism and its limits explicitly.

| Schedule | Expected observation |
| --- | --- |
| After successful verification, before issuance locks: replace hash without epoch change, revoke credential, replace credential ID, unverify/change/reassign contact, disable person or increment epoch | Generic401; no new session/cookie/CSRF. Original detached-issuance code fails the same-epoch changed-hash case. |
| Same tuple unchanged after an observed person, email or credential lock wait | Real successful issuance; exact scope and normal middleware authentication succeed. Each lock wait is separately observed. |
| Competing credential/contact/state writer after the new attempt holds the relevant row | Writer waits; issuance completes first or cancellation rolls it back. Do not label a deadlock or timeout successful serialization. |
| Current hash requires no rehash; legacy hash rehash CAS succeeds; CAS loses to a same-epoch replacement; optional rehash fails without changing original hash | Respect the exact expected-hash rule; no second password verification. A losing replacement cannot authenticate the old password. Do not assert rollback of already committed optional rehash. |
| Same-person and cross-person prior cookie, foreign-realm/malformed/single prior cookie | Existing rotation and error semantics preserved, foreign rows unchanged; failures disclose no provisional result. This is bounded behavior, not whole-writer compatibility. |
| Duplicate configured prior cookies | Newly adopted Tx-helper rejection maps to generic503 with no provisional output or committed rotation, instead of choosing the first cookie. No session source change. |
| Cancellation while waiting at each new row lock; dependency error; closed transaction; injected failed completion |503 with no success body, Set-Cookie or CSRF; every staged new session/old-session revocation rolls back where the transaction failed. |
| Unknown address, malformed address, wrong password, unverified and inactive account; ordinary signup | Existing generic failures/dummy work and registration behavior remain unchanged. |

Independent review repeats scoped normal/race/vet/pinned lint and the actual
service schedules. Required negatives are (a) original detached issuance and
(b) a mutant which compares only person epoch while omitting exact credential
hash/contact checks. Both must fail the intended assertion, then exact source
bytes are restored and the service suite rerun. Fresh landed checks and truthful
public evidence follow guarded merge. Missing prerequisites are failures, not
skips. Existing59 accepted tasks retain their original boundaries; a new
supplemental lifecycle records this repair independently.


### Exact existing-test adaptation

`TestLoginRuntimeRequiredService` currently injects `/auth` completion failures
through the session runner (`makeService(db, runner)`), because detached issuance
owns that transaction. After this change, those three existing `/auth`
rollback/cancellation/commit subtests must attach the forwarding runner to login,
pass the initial nil-options read through unchanged, and inject only after the
new READ COMMITTED writable issuance callback has staged the expected session
row. Assert the inherited cancellation and bounded derived deadline instead of
request-context pointer equality for that issuance call. Preserve all reached,
exact-row-count, actual storage.ErrTransaction, no-output and final rollback
assertions. Signup injection stays unchanged. This is a narrowly delegated
adapter/wiring correction, not permission to weaken or replace the existing
service checks. `TestLoginTxRunnerOptionsAndFailClosed` fails the initial lookup
and therefore retains its existing nil-options/context assertions unchanged.
