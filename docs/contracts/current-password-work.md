# Bounded native password computation for W1

Status: proposed B1 dependency closure, not adopted or implemented. This refines
R6 in the [writer proposal](current-session-writers.md) without changing the
landed standalone sign-in v1.25 boundary. Independent review precedes source.

## Exact consumer and package boundary

Add `identity/internal/passwordwork`, shared by the native login, recovery and
primary-proof adapters. It imports the existing `identity/password` implementation
and standard-library context/synchronization only. It imports no SQL, storage,
writer root, session, credential-proof, network or native-handler package.
Its result is computation data, never authenticated identity or issuance proof.
No public host option supplies a worker, callback or credential producer.

```go
type Verification struct {
    Verified bool
    Replacement string
}
func Verify(ctx context.Context, h *password.Hasher,
    budgetKey, secret, encoded string, rehash bool) (Verification, error)
func Hash(ctx context.Context, h *password.Hasher,
    budgetKey, secret string) (string, error)
```

Both functions reject nil context/hasher, absent deadline, expired context and
empty budget key before launch. The effective deadline is the original caller
bound; the adapter never creates a fresh authorization lifetime. Native W1
Verify runs exactly once after C acquisition, using the current held hash/contact
and original root context. Registration/recovery Hash uses its action's prescribed
original context and admission, without replacing subsequent current-state checks.
The existing native password policy and blocklist remain mandatory.

## Cancellation without an authority-bearing background task

The maintained Argon2 implementation is synchronous and cannot be interrupted
mid-computation. Wrapping it with a deadline does not stop CPU execution. Use a
single package-wide nonblocking admission semaphore of capacity two, covering
**all** in-flight Verify and Hash calls across instances. No queue, automatic retry
or unbounded goroutine creation is permitted. A slot remains held until the
actual computation exits, even when its caller has already returned canceled.
Exhaustion returns the existing password busy error and creates no worker.
Existing hasher process/instance resource limits still apply inside this bound.

An admitted computation runs in one goroutine with only its own copied string
inputs, exact hasher pointer, bounded context and a private one-element result
channel. The caller selects result versus context cancellation. It checks
`ctx.Err()` again after receiving a result, so a ready result cannot override an
already observable cancellation. A canceled caller returns zero result and the
context error immediately when scheduled; it neither waits for Argon2 nor joins
the worker while holding database locks. The worker sends at most once to the
buffered channel and releases its slot with defer; it never waits for a receiver.
The channel is not closed by the caller. It receives no separate Attempt, transaction,
request, SQL callback, evidence factory, cookie or output writer and performs no
external effect. The original context is retained until computation exits: this
is required by the native protection budget
(`identity/protection.AdmittedBudget`), whose private one-use resource admission
would be lost in a background context. This may transitively retain opaque
context values; it is not a claim of zero reference retention. The worker and
native Budget cannot extract session/writer private keys or call their APIs, and
root exit invalidates authority handles independently of context reachability.

This is the sole explicit exception to the writer proposal's no-goroutine rule:
**pure bounded password computation may outlive its canceled waiter; authority
work may not.** The root callback itself stays synchronous and releases/rolls back
its transaction on the returned error. This is a context-responsive application
bound, not a hard real-time guarantee against process suspension or an OS stall.
No successful W1 completion is permitted after the original deadline.

Configured hasher Budget and Blocklist implementations must be the finite native,
local, context-respecting implementations identified in the construction manifest;
they may not retain a separate request, access authority SQL or call a provider.
The native AdmittedBudget must consume the original private admission exactly
once; cancellation does not refund it. Test fabricated/missing/reused admissions
and password-change verify-then-hash ordering through the real protection guard. A stalled
trusted computation can occupy at most the two slots and makes future admission
busy; it cannot accumulate more workers or authorize a fallback. Independent
qualification must measure both cancellation responsiveness and useful normal
completion under the declared resource envelope. Secrets remain private memory
only; no promise of wiping all Go runtime/string copies is made.

## Reuse and rehash staging

The worker calls existing `Hasher.Verify` exactly once. When rehash is requested,
its **internally defined** PersistRehash callback only captures the bounded
replacement string; it never performs persistence or receives a transaction.
The callback validates that its old encoding is the supplied encoding and is
called at most once. Its success only means local capture. The adapter never
exports or treats `Result.RehashPersisted` as database evidence.

The returned replacement is empty for no upgrade, unsuccessful derivation, or
rehash-disabled calls. A nonempty replacement is permitted only with Verified.
An unexpected callback shape fails unavailable with zero result. Native source
retains the exact verified hash and may stage its permitted compare-and-swap in
the live attempt, then validate the actual staged hash at F. Authentication does
not require optional rehash success. MFA's counter-only branch passes rehash=false
and cannot dirty a credential. Hash similarly returns only the existing hasher's
computed encoding; it performs no identity mutation.

Zero/error/canceled results cannot be converted into writer proof. Only native
method code, after successful bounded computation and current held-row checks,
may call the closed writerproof factories. Late computation results are unreachable
from that returned call and cannot publish or enter a later attempt.

## Required checks and ownership

Source scope is only the new internal package and its tests under the integrator's
shared-seam custody; native caller edits require their own exact path delegation.
This proposal does not grant broader login/magic/MFA ownership or adopt the whole
W1 graph. All source stays in the B1 candidate; no separate prerequisite PR.

Required checks: real valid/invalid/current/legacy/dummy verifier behavior and
optional captured rehash; same existing password policy and budget decisions;
missing/expired deadline rejects before computation; cancellation after a
channel-observed computation start returns zero before release; late success
never reaches the caller; canceled tasks retain both slots until actual worker
exit; the third call is busy without starting; Hash shares the same bound;
error paths release slots; simultaneous result/cancellation never returns success
once cancellation is observable. Tests use a private test-only barrier around
the computation, not sleeps or a production-configurable worker callback.

Run scoped normal/race/vet/pinned lint, with actual Argon2 coverage distinct from
barrier-controlled lifecycle tests. Genuine negatives must drop the final context
check, release admission on waiter exit, or replace the buffered channel with a
blocked send and fail their intended assertions, followed by exact restoration.
The native W28 database-lock-release/one-Verify/deadline schedule remains required
at integration; pure adapter tests cannot qualify it. No provider, deployment,
whole-identity or product acceptance follows from this package.
