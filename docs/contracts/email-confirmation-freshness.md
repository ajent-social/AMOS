# Email confirmation after lock waits (v1.23 proposal)

Status: proposed prerequisite; independent exact-head review and guarded landing
precede source. This fixes the email confirmation transaction only. Full identity
writer compatibility, current-session provenance and host authority remain open.

## Existing behavior and narrow correction

At source `1149112f406500e2a80c194ee7290cbd45ab2c1c`,
`identity/email.Service.Confirm` has its own challenge-consuming UPDATE using
transaction-start time. The shared store correction in v1.22 does not reach it.
Confirmation subsequently updates the bound email and pending person, so those
row waits also precede completion of the confirmation's state transition.

Preserve the exported Confirm signature, configuration, token parsing, realm
binding and non-enumerating sentinels. Preserve preview-only GET behavior,
request-admission wrappers, protected delivery, and the absence of session
issuance or external effects. No schema or credential format changes.

The confirmation runner requests explicit READ COMMITTED. Inside its existing
single transaction:

1. Select and lock only the exact challenge row with `FOR UPDATE OF c`, matching
   ID, email-verification purpose and token digest, and joining its exact bound
   person/email within the configured installation/application. Require pending
   person, unverified email and unconsumed challenge. This lookup does not mutate
   or use transaction-start expiry as validity evidence. Return only its bound
   person/email selectors internally. They confer no general authority.
2. Update that exact bound email with the existing realm/person/unverified
   predicates; require one affected row. Update the exact bound pending person
   with the existing realm/state predicates; require one affected row. These
   writes are provisional and retain their row locks. Preserve existing timestamp
   semantics for these two records. No response, callback or external effect may
   observe a successful confirmation before transaction completion.
3. After those statements have completed, use the adopted
   `store.Store.ConsumeChallenge` in this same transaction. Its separate post-lock
   database clock is therefore after challenge, email and person statement waits.
   Require returned person/email to equal the locked selectors. Do not duplicate
   its expiry SQL or provide a caller clock. Its strict expiry check and consumed_at
   use the same instant; equality is expired.
4. Any failure rolls back the whole transaction, including provisional contact
   verification/person activation. Challenge unavailable, wrong binding or
   conditional zero-row results retain ErrChallengeUnavailable. SQL/scan/runner,
   isolation or other persistence failures retain ErrUnavailable. Do not expose
   raw diagnostics. Return nil only after the runner reports successful commit.

The initial selection may lock an expired challenge, but cannot commit any
verification on its behalf: the final shared consumer rejects it and all prior
writes roll back. No successful partial activation is an allowed fallback.
A commit error remains unavailable, including an unknown commit outcome; do not
retry automatically or claim it necessarily rolled back. Later client replay
remains non-enumerating.

This deliberately retains the challenge-before-email-before-person lock chain.
It does not claim compatibility with issue paths that lock parents first, with
arbitrary callbacks, or with deferred workspace triggers at commit. Cancellation
or database deadlock errors must fail closed. The validity linearization point
is the final shared consumption, after the explicit confirmation row writes;
this is not a promise that a wall-clock deadline cannot pass during commit or
an unqualified deferred trigger. Whole-writer and host adoption remain blocked.

## Source ownership and dependent acceptance

After independent design adoption, the separately claimed email lane may change
only Confirm's body/comment in `identity/email/email.go` and add
`identity/email/confirmation_freshness_integration_test.go`. Existing runtime
helpers may be reused without editing their files. Do not change shared store,
session/producer siblings, modules, migrations, plan, preview or HTTP handlers.
This is a bounded T3.6 follow-on; its historical component acceptance and the
accepted-product count are not new evidence for this correction.

## Required actual verification

Use a fresh operator-granted, reviewed TLS PostgreSQL fixture with precreated
schema and a runtime-only DML role. Tests must fail visibly without the required
fixture; no DDL, privileged connection or skipped service evidence. Use the actual
exported Confirm through a real transaction runner. Keep synthetic rows exact-owned,
all SQL/wait observations/cleanup bounded, and credentials out of artifacts.

- Independently hold each of challenge, bound email and bound person in separate
  schedules. Observe the exact confirmation backend blocked by that holder, with
  transaction and waiting-statement starts before expiry. Observe database time
  beyond expiry before releasing the holder. Confirm must return unavailable
  challenge and all three records must retain their prior state after rollback.
  A goroutine start, a sleep or unrelated backend activity is not wait evidence.
- For each same row-wait schedule with a still-live challenge, require successful
  commit: only its bound contact is verified, its pending person is activated,
  and consumed_at is no earlier than a database observation made immediately
  before releasing the holder. Require later replay to be unavailable.
- Prove wrong purpose/digest/realm, unknown/expired/consumed challenges and wrong
  bound contact/person state fail without committed changes. Another unverified
  contact belonging to the person must remain untouched.
- Force a real caller-runner rollback after the Confirm callback succeeds; all
  three records remain unchanged and a subsequent live confirmation succeeds.
  Prove concurrent confirmations yield exactly one committed success, and prove
  cancellation/statement failure and commit failure never report success.
  Injected completion errors are bounded test evidence, not naturally occurring
  unknown-commit qualification.
- Run independent normal/race checks, vet and pinned lint. An isolated original
  inline-consume mutation must fail the expired-wait assertion. A second mutation
  moving shared consumption before the email/person writes must fail a downstream
  wait assertion, even though shared consumption itself uses a post-lock clock.
  Restore exact source and rerun; an independent reviewer repeats both negatives.
  Fresh landed service checks and exact source identity are separate gates.

Existing HTTP/abuse admission, full issuance and delivery, session provenance,
writer graphs, runtime roles, live email provider and replacement rehearsal are
not qualified by this bounded producer check.
