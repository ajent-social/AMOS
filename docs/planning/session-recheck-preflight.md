# Transaction-time session provenance preflight

Status: independently reviewed with open findings; not frozen or approved for implementation. This is the next
concrete authority prerequisite; constructor adaptation does not implement it.
Source inspected at the PR30 design revision, with identity implementation unchanged.

The existing middleware resolves a durable session but attaches only a principal.
A future transaction authorizer therefore lacks a trusted session reference for
revocation checks. Proposed additive surface, owned by the coordinator:

```go
func (s *Service) RecheckCurrentTx(ctx context.Context, tx *sql.Tx) (identity.Principal, error)
```

The session package would keep a private context key and immutable reference to
the successful middleware session, its principal snapshot and exact Service
instance. No exported injection/extraction API, raw token retention, caller session
ID, principal constructor or public credential argument is proposed. Mint only
after the authentication transaction commits and origin/CSRF checks succeed.
Duplicate-cookie rejection is an explicit compatibility amendment requiring review.

The proposed recheck uses the supplied transaction only, verifies READ COMMITTED,
locks person before session, then reads database clock after waits. It checks realm,
active state, security epoch, revocation, idle/absolute expiry and assurance without
refreshing expiry, opening a connection, committing, retrying or issuing cookies.
Missing/revoked proof maps to existing unauthenticated semantics; inability to
establish current state is unavailable. A refreshed principal is still not a policy
allow or updated workspace selection. Recheck again after later resource/replay
waits before disclosure; full executor integration remains separate.

The existing internal authproof bridge is legal inside identity/session; operation
and policy packages may not import it. No migration is currently proposed. Exact
schema, assurance downgrade/expiry rules and method semantics need an ADR, minor
contract amendment, independent review and explicit source ownership before coding.

Writer compatibility blocks dispatch. Existing rotation can lock an old session
before the new person's row; cross-person old cookies make a simple claimed
person/session order insufficient. Federation, recovery, magic-link and MFA flows
also need whole-transaction lock-order review. A bounded session seam cannot certify
those writers merely by using ordered locks itself.

Required evidence includes real two-connection revocation and epoch schedules,
absolute/idle/assurance expiry during demonstrable lock waits, rollback controls,
proof-mint failures, same-instance binding, unsupported isolation and actual writer
paths. Use database-clock observation and bounded barriers, not sleeps or mocks as
service proof. Keep source ownership disjoint from the active constructor author;
hand over session.go only after its reviewed integration. No executor, provider,
production role or deployment qualification follows from this preflight.

## Independent source review findings

The bounded review of the proposal and landed session source identified these
amendment gates. These are source-derived findings, not reproduced runtime
exploits or completed fixes.

- A post-renewal recheck cannot recover the old idle-expiry boundary after
  `FindActiveSession` has renewed it using transaction-start time. Renewal needs
  a post-lock database-time check and an actual wait-across-expiry regression
  before it can mint trustworthy current-session provenance.
- The frozen transaction authorizer returns only a policy decision. An explicit
  current-principal return path is needed so an allowed handler receives any
  assurance downgrade from the recheck, rather than the original snapshot.
- Session rotation can lock a session before acquiring a person foreign-key
  lock, conflicting with the proposed person-then-session order. Composed
  writers must discover and order all affected persons before later locks;
  sorting inside issuance alone does not cover callers that already hold locks.
- Magic-link and recovery challenge consumption need post-lock freshness
  qualification. A later session recheck cannot reconstruct an already expired
  credential that was consumed using transaction-start time.

The next amendment must address renewal, refreshed-principal handoff, complete
writer transaction graphs and producer freshness before adoption. Actual
bounded two-connection tests must prove commit/rollback behavior, expiry while
waiting, same-instance provenance, assurance downgrade and current-principal
handoff. Callback invalidation before codec work remains separate. No writer
repair, authority seam implementation or executor acceptance is claimed here.
