# Post-lock challenge consumption freshness (v1.22 proposal)

Status: proposed prerequisite; independent exact-head review and guarded landing
precede source. This is one identity-store primitive, not complete credential
producer qualification, writer serialization or current resource authority.

## Source-grounded problem and boundary

At source `a5e6e98dacd757cdc93081837839096698b646c7`,
`identity/store.ConsumeChallenge` uses `transaction_timestamp()` to decide expiry
inside an UPDATE that can wait for a challenge lock. A transaction begun while
the challenge was live can therefore consume it after its expiry. This is a
source-derived finding; a real two-connection schedule must demonstrate the
stale behavior before any fix is accepted.

The method is consumed by password recovery and magic-link confirmation. Their
other person/email/policy/session waits and lock ordering remain their own gates.
Email confirmation has separate inline challenge SQL and does not call this
method. Changing the store primitive does not repair or qualify that inline path,
previews, complete credential producers, session provenance or an executor.

## Exact semantic correction

Preserve `ConsumedChallenge`, the public method signature and existing sentinels:

```go
func (*Store) ConsumeChallenge(context.Context, uuid.UUID, string, []byte) (ConsumedChallenge, error)
```

1. Validate nil receiver/transaction/context, canonical UUIDv7 value, finite
   purpose and exactly32-byte digest as today. Invalid input returns zero output
   and ErrInvalidInput before SQL. Do not add credential parsing or accept a
   caller clock, principal, person ID, session reference or policy assertion.
2. Require READ COMMITTED using the supplied transaction's actual isolation.
   Unsupported isolation or failure to establish it returns zero output and
   ErrPersistence before mutation. This is an explicit compatibility restriction;
   never change the caller's isolation or open, retry, commit or roll back a
   transaction inside this method.
3. Lock only the row matching ID, purpose and token digest, using a SELECT FOR
   UPDATE which does not mutate it or use expiry as proof. Do not return person
   or email values yet. Missing/mismatched row returns zero output and
   ErrChallengeUnavailable. Lock/query/scan failure returns ErrPersistence.
4. Only after the row-lock statement completes, sample `clock_timestamp()` in
   the same database/transaction. Use that one instant in the final UPDATE's
   strict `expires_at > instant` and `consumed_at IS NULL` predicates, preserving
   exact ID/purpose/digest binding. Set consumed_at to that same post-lock instant.
   Zero affected/returned rows are ErrChallengeUnavailable; equality is expired.
5. Return the stored bound person/email only from a successful final UPDATE.
   All error paths return the zero DTO. Database failures are ErrPersistence,
   never raw SQL diagnostics. Results remain provisional until caller commit.
   A rollback must restore consumption and allow a still-live token to be used.

Do not sample before the wait, rely on transaction-start time, or assume a clock
expression inside the waiting UPDATE executes after every possible wait. Separate
lock, clock and UPDATE statements make sequencing explicit. The lock is retained
by the caller transaction; no second mutation writer can change this row between
sampling and update. This does not lock a person's other authority state.

Changing consumed_at from transaction-start to post-lock database time is an
intentional event-time correction. No migration/schema change is needed. Other
created/updated timestamps retain their existing semantics; neither the method
nor its caller may report a committed result before successful transaction exit.

## Ownership and retained gates

The integration coordinator retains T2.8 and shared identity ownership. After
independent design adoption it may delegate only the ConsumeChallenge body and
comment in `identity/store/store.go` and a new
`identity/store/challenge_freshness_integration_test.go`. Existing test helpers
may be reused without editing their files. No other store method, session
middleware, producer, module, migration, contract, plan or executable is source
owned. Preserve foreign task claims and all existing source/receipts.

Full person/session/challenge writer graphs, cross-person rotation, bulk session
revocation, email inline consumption, magic-link proof timestamps, recovery and
MFA policy waits, federation and refreshed-principal handoff remain open. This
primitive does not return a principal, grant, session proof or workspace/resource
authorization. T2.8 and the continuity host/current-authority contract remain
in progress; full producer tests must follow their independently adopted contracts.

## Actual verification required before source acceptance

Use only a fresh operator-granted reviewed TLS runtime fixture with precreated
identity schema and finite runtime DML. Tests never provision schema or obtain
administrator credentials. Missing fixture fails visibly, with zero skips.
Every query, transaction, waiter observation and cleanup is context-bounded.
Synthetic row IDs are exact-owned; no foreign data may be mutated.

Required actual schedules:

- Hold the challenge row in one connection. Begin the consuming transaction and
  demonstrate that its exact backend is waiting on that row while transaction
  and lock-statement start time are before expiry. Observe database time beyond
  expiry, release the blocker, and require ErrChallengeUnavailable/zero DTO.
  Verify consumed_at remains NULL after caller rollback/commit as appropriate.
  Sleeps alone, unrelated backend activity or a started goroutine do not prove
  the waiter schedule.
- A still-live challenge succeeds after a demonstrated wait, returns only its
  exact bound person/email, and records consumed_at no earlier than a database
  observation taken by the blocker immediately before releasing its held row. Verify committed replay is unavailable.
- A successful consume rolled back by the caller leaves a still-live token
  consumable in a later transaction. Two real competing consumers produce one
  committed success and one non-enumerating unavailable result.
- Wrong purpose, wrong digest, unknown ID, already consumed, expired and the
  exact strict expiry boundary return zero output/unavailable. Invalid shape
  fails before SQL. Canceled waits, completed transactions and unsupported
  isolation fail closed without returning partial values or consuming a token.

Exercise the actual exported method with runtime transactions, not a copied SQL
function. Tests may use existing independently qualified runtime-fixture and
waiter helpers. A meaningful isolated mutation restoring transaction-start expiry
must fail the intended stale-consumption assertion, then exact source restoration
and fresh checks must pass. Detect a pre-lock clock sample as well. Normal/race,
vet, pinned lint, a different exact-head reviewer and fresh landed required-service
checks are separate receipts. No provider/production-role/host readiness follows.
