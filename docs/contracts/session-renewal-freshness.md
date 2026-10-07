# Post-lock session renewal freshness (v1.21 proposal)

Status: proposed prerequisite only. Independent complete design review and
landing precede source assignment. No current-session provenance, transaction
authorizer, refreshed-principal handoff or executor is implemented by this slice.

## Source-grounded problem

`identity/store.FindActiveSession` currently renews idle expiry with
`transaction_timestamp()` in an UPDATE that can wait for the session row.
A transaction started before expiry may therefore renew a session after its old
idle or absolute boundary has passed. `ActiveSessionAssurance` likewise uses
transaction-start time when deciding whether to retain elevated assurance.
These are source-derived findings; actual regression schedules remain required.
A later authority recheck cannot reconstruct an idle boundary already renewed.

## Additive semantic correction

Preserve exported signatures and result types, existing invalid-input and
unavailable/persistence sentinels, scope predicates and middleware wire mapping.
No migration, credential, token, cookie, principal or policy shape changes.

`FindActiveSession` must:

1. Validate inputs as today and reject non-READ-COMMITTED transaction isolation
   with `ErrPersistence` before renewal. This is an explicit compatibility
   restriction for callers using another isolation level. Never alter isolation
   or open/retry/complete a transaction owned by the caller.
2. Acquire the matching session row lock with the full digest plus installation,
   application and environment scope, without first renewing expiry. Missing
   scope/token returns empty result and `ErrSessionUnavailable`. Do not lock a
   session from another realm or return any of its identity values.
3. After the row lock statement completes, sample `clock_timestamp()` from the
   same database. Validate the currently locked row against its pre-renewal idle
   and absolute boundaries, revocation, current active person, realm and matching
   security epoch. The account read uses a fresh READ COMMITTED statement
   snapshot; this slice does not add a person row lock or claim writer ordering.
4. Update last-seen and idle expiry only if all predicates pass. Use the same
   sampled database instant for all expiry predicates and renewal; idle expiry
   is the lesser of absolute expiry and that instant plus thirty minutes.
   Return the existing AuthenticatedSession shape only on success. Expired,
   revoked, foreign, inactive or wrong-epoch sessions yield an empty result and
   `ErrSessionUnavailable`; query/lock/scan failures remain `ErrPersistence`.

Do not evaluate the clock before waiting on the row, reuse transaction-start
clock, extend absolute expiry, update a denied session, or substitute application
wall time. Use separate statements or another independently proven sequencing
mechanism; the presence of `clock_timestamp()` inside a waiting UPDATE is not
sufficient evidence that the clock was sampled after the wait.

`ActiveSessionAssurance` retains its prerequisite: call only after successful
`FindActiveSession` in the same caller-owned transaction, which holds that
session row lock. It reads a fresh database clock after that lookup and checks
current revocation plus idle/absolute expiry again. An expired session returns
empty level/time and `ErrSessionUnavailable`; database errors return
`ErrPersistence`. For a live session, assurance whose expiry is strictly later
than the sampled instant keeps its persisted level/expiry; otherwise downgrade
to `aal1` with the existing absolute-expiry fallback. Equality is expired. Do not
renew assurance or elevate expired proof. Preserve optional assurance-schema
behavior: PersistAssurance=false must not query assurance columns; the renewal
method itself cannot depend on that optional schema. Both CASE results use one sampled
instant; no application clock or transaction-start timestamp controls the choice.

Middleware continues to expose a principal only after its owning transaction
successfully completes. Existing origin/CSRF checks and commit-failure response
behavior remain unchanged. Full authority must still return the refreshed
principal and recheck after subsequent resource/replay waits; no such authority
API is granted here.

## Ownership and remaining writer gates

After design landing, the integrator may explicitly delegate only
`identity/store/store.go` (FindActiveSession body/comment),
`identity/store/assurance.go` (ActiveSessionAssurance body/comment), and new
`identity/store/session_freshness_integration_test.go`. No unrelated store
methods, session middleware/issuance, producers, contracts, modules, migrations
or executable wiring are source-leaf owned. Preserve foreign task claims and
all existing authored work; this is a delegated current-session prerequisite.

Locking a session row and reading current account state is not a person/session
serialization protocol. Concurrent account writers, cross-person rotation,
password reset/change, magic-link, MFA, email confirmation and federation still
require complete transaction lock graphs and post-lock producer freshness.
No change here certifies those writers, mints opaque provenance or enables
operation dispatch. Full T2.8 and paid-access T5.9 remain gated.

## Required verification

Use a real TLS runtime-only identity/assurance profile with at least two
connections, precreated by the reviewed fixture operator. Tests create only
exact-owned synthetic data through runtime DML; they do not create schema or
read admin credentials. Missing profile fails visibly, zero skips. Bound every
transaction and wait, observe the exact waiter backend/lock rather than unrelated
activity, and coordinate with channels plus database clock observation. Wall
clock sleeps alone are not evidence of a lock wait or expiry crossing.

Required schedules cover:

- Old idle and absolute expiry crossing a demonstrated session-row wait while
  transaction-start AND lock-statement-start time remain before the boundary;
  a pre-lock CTE/statement clock must also be detected. Rejection leaves both
  idle expiry and last-seen unchanged after the blocker commits.
- Successful post-wait renewal of a still-live session, bounded to absolute
  expiry, plus rollback restoring the exact previous values.
- Assurance expiry crossing the same wait and downgrading to aal1 while the
  session remains live; live elevated assurance and exact-boundary semantics.
- Revocation or security-epoch change committed by the blocker before release;
  no successful session result. This tests those schedules, not complete writer
  serialization or an arbitrary concurrent update after the sample.
- Wrong realm, unknown digest, canceled wait, closed/completed transaction and
  unsupported isolation fail closed without renewing or returning partial DTOs.

Normal/race/vet/pinned lint, a meaningful old-clock mutation producing intended
stale-renewal or stale-assurance failure, exact restoration and passing checks,
and different exact-head source/fix review are mandatory. Guarded merge and
fresh landed required-service checks are separate receipts. Local fixture proof
does not qualify providers, production roles, full middleware authority or
production deployment.
