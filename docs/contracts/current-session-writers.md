# Current-session writer and producer protocol (CP2 candidate)

Status: proposed finite design resolution, **not adopted, implemented, qualified,
source-ready or accepted**. Revision fixes R1–R6 of the CP2 review.
Document base: `3d00fcc21662e0ed3336f5388184e1cedbe52580`; main snapshot
inspected at 2026-10-08 19:20 UTC: `2bb3f5ad9249d633a27dd272b9f1484d82cf111b`. The separate v1.25
password design is adopted on that main. PR76 source
`fca24649b442d1563dc2ea410679eababed0b69d` was inspected separately as an independent-check candidate (open at that
inspection), not transplanted or used to assert source adoption/landing. This supplements the
[current-session reader candidate](current-session-recheck.md); it does not edit
that candidate, frozen contracts, task ownership or the acceptance registry.
The reader remains separate person `SHARE`, then exact session `SHARE`.
Same-instance provenance is not transaction/database attestation. Trusted
composition and refreshed workspace/resource handoff require their own resolution.

## Decision: move admission to the transaction root

Select a conservative, serial writer protocol, W1, for the complete native
identity/workspace graph. This is an explicit throughput tradeoff, not a claim
that the existing graph already obeys it. It preserves all currently supported
routes. A later concurrent writer protocol requires a separately reviewed
replacement and the same schedules; do not remove serialization as an optimization.

### R2: exact singleton schema and runtime privileges

The integrator allocates the migration sequence; this is its proposed exact DDL
and privilege contract, not permission to edit or execute a migration:

```sql
CREATE TABLE public.identity_writer_gate (
    id smallint PRIMARY KEY CHECK (id = 1),
    lock_slot boolean NOT NULL DEFAULT false
);
INSERT INTO public.identity_writer_gate (id) VALUES (1);
REVOKE ALL ON public.identity_writer_gate FROM PUBLIC;
-- amos_runtime denotes the separately provisioned runtime role.
REVOKE ALL ON public.identity_writer_gate FROM amos_runtime;
GRANT SELECT (id), UPDATE (lock_slot)
    ON public.identity_writer_gate TO amos_runtime;
```

The runtime role is neither owner nor a member/inheritor of a broader role; it
has no INSERT, DELETE, TRUNCATE, UPDATE(id), DDL, ownership or grant option.
`lock_slot` has no authority meaning and the root never writes it. Column UPDATE
is deliberately present because a locking SELECT requires UPDATE on at least
one column. All roots use exactly:

```sql
SELECT id FROM public.identity_writer_gate WHERE id = 1 FOR UPDATE;
```

Require exactly one row with id=1; missing/malformed gate is unavailable, never
an implicit unlocked path or runtime repair. Before G, verify actual
`pg_catalog.current_setting('transaction_isolation') = 'read committed'` and
`pg_catalog.current_setting('transaction_read_only') = 'off'`. Failure or mismatch
rolls back. No mode switch, retry or savepoint recovery is permitted.

This gate spans the trusted database, including all installations/applications
and environments: existing token/subject uniqueness and person-wide revocation
are not all environment-local. It is not a distributed service or new credential.

The root acquires this gate before **any** domain row lock, mutation, foreign-key
insertion, policy callback or deferred constraint work. Plain nonauthoritative
lookup, signup/reset new-password hashing and bounded provider exchange may
happen before the root. W1 password **verification** never happens before G: exactly one Verify
uses the current hash under held C. Pre-root work is not authority. No upgrade from an existing
reader transaction is allowed: release/rollback it, then start a fresh W1
transaction and reestablish authority. No gate acquisition inside `issueTx`,
`FindCurrentPassword`, `insertBinding`, a membership trigger or a policy callback
can make an already-running legacy caller conform.

The singleton serializes competing writers and stabilizes absent-row predicates.
It does not exclude readers. Separate per-person `FOR UPDATE` gates exclude
current-session readers before a writer can hold a session they need. Use the
stronger mode consistently, including credential rehash and membership insertion;
ordinary person `SHARE` cannot protect a policy's membership/binding absence.
A non-mutating current-session reader never takes or upgrades the singleton.

Transaction root requirements:

1. Explicit writable READ COMMITTED, bounded request context, gate first; verify
   actual transaction mode. No automatic retry or savepoint rollback continuing
   with a retained capability. Error invalidates the entire attempt.
2. Under the gate, discover the **complete** affected person set using fresh
   scoped lookups. Include actor, target, prior-cookie owner, link-flow person,
   membership targets and personal owners; include people whose authority a
   workspace change affects. Do not infer identity from email equality or a
   configured owner ID. Sort immutable person UUIDs, then lock each with a
   separate scoped `FOR UPDATE` statement. For newly registered persons, reserve
   server-generated IDs in the plan; insert their rows before their children.
   Rediscover and compare bindings after person acquisition. If a binding changes
   or an unplanned person is found, roll back; never append a lower-order lock.
3. Acquire children by the fixed order below; within each table use ascending
   immutable primary key, one explicit lock statement per existing row. Prepare
   new IDs up front. A joined `FOR UPDATE OF p,e,c` is not an ordering primitive.
   Lock complete sets, not arbitrary batches. Missing expected rows deny the
   method; malformed state/query failures are unavailable.
4. Perform bounded method verification/staging and policy work on those rows.
   All callbacks use this transaction and the declared row plan; none can open a
   second transaction, commit, acquire a new earlier lock or perform network I/O.
   Rate-budget admission stays in its existing completed transport transaction;
   budget proof is not credential proof. A callback which cannot meet this
   contract is an explicit composition blocker, never assumed compatible.
5. Drain pending required constraints, then run the final method-specific
   freshness fence described below; commit immediately through the root owner.
   Publish cookies, seeds, redirect success, principal or other protected output
   only after successful completion. No additional callback or SQL mutation may
   follow the final fence. Commit failure discards all provisional output.

| Phase | Complete rows and mode | Reason / constraint |
| --- | --- | --- |
| G | Singleton UPDATE | Serializes every writer, including jobs/admin paths touching these tables. |
| P | All affected persons UPDATE, sorted | Reader exclusion, state/epoch and contact/membership/binding absence gate. No subsequent new person lock. |
| C | Emails UPDATE, credentials UPDATE, federation connections SHARE (UPDATE for configuration change), external bindings UPDATE, each sorted in this table order | Lock positive proof and provider configuration; gate protects missing rows and uniqueness contenders. Password hash work cannot commit a nested rehash transaction. |
| H | Challenges UPDATE, factors UPDATE, federation flows UPDATE, in that table order | Preserve purpose, contact/browser/epoch bindings, replay and flow consumption. Discovery of a flow occurs before locks, not flow UPDATE before person. |
| S | All existing affected sessions UPDATE, sorted | Actor/link session, same-person or cross-person old cookie, and full revocation set. Insert the newly staged session only after this phase. |
| W | Workspaces UPDATE sorted, then memberships UPDATE sorted | Include every workspace later reached by person-state owner triggers, both OLD/NEW workspaces for membership moves, and policy dependencies. |
| D | Declared delivery material, outbox, audit/replay/business participants in their separately frozen order | Their callbacks may not return to G–W for new locks. An unqualified participant remains a blocker. |
| F | Explicit constraint drain; fresh read-only final fence; commit | Deferred triggers must only revisit planned/held rows. No output before commit. |

Because G excludes all other W1 writers, sets and absence predicates are stable
while P–F execute. Readers lock person before session before workspace/resource;
they cannot hold a later lock and then request a new earlier identity lock.
A writer blocked at P has not locked any session/workspace; once it passes P,
a reader for that person cannot enter. A reader for another person may delay W,
but the writer must not subsequently request that reader's person/session.
This is the ordering argument to test, not a substitute for execution evidence.
An operation needing multiple persons as a reader must acquire its complete
sorted person set before sessions. Resource-to-identity reentry and read-to-write
promotion are rejected protocols.

PostgreSQL row `SHARE` permits foreign-key `KEY SHARE` but conflicts with person
UPDATE/NO KEY UPDATE. W1 deliberately takes its person UPDATE **before** children;
retrofitting it after old-session revocation would recreate the inversion.
Locks survive until transaction end, subject to savepoint rollback, which W1
forbids after admission. See the PostgreSQL
[row-lock conflict table](https://www.postgresql.org/docs/16/explicit-locking.html#LOCKING-ROWS).
READ COMMITTED permits a new statement snapshot after waits; a statement's early
clock/snapshot is not a final producer fence. See
[READ COMMITTED](https://www.postgresql.org/docs/16/transaction-iso.html#XACT-READ-COMMITTED).
Unique insertion can wait for an uncommitted contender and then succeed after
its rollback, so a successful insert is not freshness evidence. See
[unique checks](https://www.postgresql.org/docs/16/index-unique-checks.html).

## R1: exact producer seam and lifetime

The [API appendix](current-session-writer-api.md) replaces the former semantic
producer pseudonames with exact package ownership, declarations, phase calls,
immutable values and native factories. `internal/authoritywriter` owns ordering
only; `identity/internal/writerproof` owns closed method evidence. Session imports
evidence, native producers import session, and neither lower package imports the
producers. Workspace roots use ordering capabilities without credential evidence.

Root/plan/attempt/request/evidence/staging are instance-bound, copied and
non-serializable. Zero, reused, wrong-service, wrong-transaction, unplanned-row
and closed-attempt values fail before participant SQL. Final permits allow only
one after-commit publication. Native adapters compare exact held records at F;
the root's ordering marker is neither authentication nor database attestation.
All actions below are preserved; arbitrary external producer registration is
not an extension surface. Legacy paths explicitly refuse in the W1 profile.

## R6: root timeout and nonrefreshable deadline table

This proposed initial W1 profile freezes a **five-second total root ceiling**,
including pool checkout, mode check, G/P/child waits, password verification,
callbacks, constraint drain, F and completion. On Run entry at monotonic T0,
derive context deadline min(original request deadline, T0+5s). An absent request
deadline gets that five-second bound; nil context, canceled context, a reported
zero deadline or a deadline <= T0 is unavailable before opening a transaction.
An earlier deadline is never extended. There is no queue outside this budget,
background-context fallback, retry, deadline reset on phase entry or detached
commit. Context cancellation during G wait aborts and releases any acquired
locks; commit failure or cancellation yields no result. Fixed transaction-local
statement/lock timeouts may be shorter but cannot replace the total context
bound. Hash/vault adapters must honor it; inability to bound actual work is a
source/admission gate, not permission to overrun it. Current password.Verify uses
non-interruptible Argon2 calls after its context-aware admission. The context
can abort the transaction but does not guarantee the CPU call returns in five
seconds. Therefore current Hasher is not asserted to meet the hard total bound:
source qualification must supply bounded/cancelable native execution with a
concrete adapter contract before W1 admission; changing this selected five-second
ceiling requires a separate amendment, not an implementation fallback. No detached
hash goroutine may keep using the attempt after cancellation.

Sample `B = pg_catalog.clock_timestamp()` immediately after G in a separate
statement. Freeze DB root deadline `B + remaining_monotonic_budget` (remaining
measured immediately after that query returns, never more than 5s); context still owns
the stricter elapsed-time limit. F must satisfy this DB deadline too. This is a
freshness bound, not a promise of a physical commit timestamp. Use microsecond
UTC persisted instants, compare instants rather than Go structural equality.
Strict final validity is F < every applicable deadline; equality denies/rolls
back. A root/context timeout itself is unavailable. No local Now hook supplies
W1 authority time; DB clocks with invalid/backward chronology fail unavailable.

| Native evidence/action | Frozen original bound; final rule |
| --- | --- |
| Password sign-in or current-password primary | Exactly one Verify on locked exact C credential/contact/person, then immediately sample DB V before any further wait. ProofUntil=V+15m; exact credential ID/hash/method/unrevoked state, verified contact ID/key/time and person epoch bind it. Final F<V+15m and root bound. Unknown/wrong password preserves dummy/generic behavior, never evidence. No detached pre-G proof accepted. |
| New password on signup/reset/change | Signup/reset Hash before G is preparation, not proof. Password change performs Verify **then Hash under held C**: current AdmittedBudget permits change-new only after change-current, once each. Bind output to this attempt/action; reset challenge/current actor still pass independently. W1 optional sign-in rehash uses same-tx CAS under C and is atomic with issuance, never its old nested transaction callback. Capture a CAS SQL/error/row-count failure even if Verify suppresses optional persistence errors: poison/rollback the root, no savepoint recovery or stale expected-hash fallback after an aborted tx. |
| New base session | Authentication V from method verification, absolute E=V+12h, initial idle I=min(E,V+30m); F<min(E,I). Elevated session base remains 12h, elevation has its own shorter bound. No resetting issued/authenticated time at F. |
| Renewal / existing actor | Original stored E and I must be live after acquisition and at F; freeze renewal V after S, stage I'=min(E,V+30m), validate F<I' too. Rotation/self-revocation preserves original actor I/E/authentication and required assurance bounds through F; intentional revocation alone is exempted. |
| Registration/email request and confirm | Signup lifetime fixed 30m. Email configurable lifetime validated once: integral seconds, 1m..24h, default 30m. New challenge E=B+TTL; original persisted E for resend/confirm, never refreshed by queueing/consumption. Delivery material expiry = E and job expiry <= E, both > F. |
| Reset request / complete | Integral-second TTL 1m..24h, default 30m. New E=B+TTL, persisted E on completion; consume/credential/epoch/revocation does not move E. Complete needs F<E even after consuming its own token. |
| Magic request / confirm | Integral-second TTL 1m..30m, default 30m. New E=B+TTL, original persisted E on confirm; same browser or explicit other-device consent bound. F<E with no extension by session rotation. |
| MFA BeginEnrollment | Actor's original base/required assurance bounds plus primary V+15m; pending P=V+30m, seed AAD binds exact P. F<all. No TOTP proof or new session exists yet. |
| MFA ConfirmEnrollment | Same primary/actor bounds, original pending P and pending epoch; after vault work sample DB T, match newest eligible step k with 30s period/skew1. Freeze window K=(k+2)*30s since Unix epoch. Require k>=0, step unused, T in [(k-1)*30s,K), F<K and F<P. New elevation A=T+15m, F<A. Clearing pending expiry during activation does not erase P in evidence. |
| MFA Challenge | Same rule on active factor without pending P; exact last-used step/replay comparison, current lockout and seed binding. F<K, primary V+15m, actor bounds and A=T+15m. No matching code is required for the counter branch; that branch has its separate deadline below. |
| MFA Status | Original actor bounds/policy at final DB instant. No primary password, TOTP or issuance. Pending=true only if original pending P>F; at equality report no live pending factor, rather than extending it. |
| Federation BeginLogin / BeginLink | Flow E=B+10m; exact URL inputs/connection/intent. BeginLink also F<original actor authenticated_at+15m, I,E and required assurance bound. No provider proof exists on begin. |
| Federation CallbackLogin / CallbackLink | Original persisted flow E, provider ProviderValidUntil and ReceiptUntil below. Link additionally retains bound session I/E, assurance and authenticated_at+15m from begin; callback does not renew them. F<all. |

For federation exchange, a native adapter must sample a DB validation-start
instant Q **before** the external exchange, outside W1, under the original
bounded request. The proposed receipt ceiling is Q+60s, nonrefreshable across G
or any later wait. Its immutable result supplies exact provider, connection,
issuer, subject, nonce binding, Q, validation-completed instant and the actual
provider proof's `ProviderValidUntil`. Final bound is min(Q+60s,
ProviderValidUntil, original flow expiry, applicable link bounds, root deadline).
ValidatedAt is sampled through the same private DB capability immediately after
validation, before entering G, with Q<=ValidatedAt<ReceiptUntil. Neither bound
is reset at callback-root admission. The DB/adapter clock comparison and provider
validity semantics must be qualified
per adapter; a local wall-clock guess or sampling Q after G is forbidden.

Current `federation.Identity` has only issuer/subject/nonce/email and cannot
supply this contract. Finite missing adapter gates remain for Google, GitHub,
Apple and enterprise OIDC: each must define actual validated artifact, issuer/
audience/nonce or equivalent binding, trustworthy expiry/revocation semantics,
clock conversion, receipt lifetime and positive/expired/equality schedules.
GitHub must specify its actual exchange validation contract rather than invent
an ID-token `exp`. Passkey proof/counter/origin/RP-ID validity remains its own
future native adapter gate. All stay in full product scope; no selected host
may silently drop these methods or advertise them as qualified by W1.

The adopted v1.25 standalone sign-in prerequisite is deliberately distinct: it
verifies before its three-second issuance transaction, revalidates held rows and
may commit optional rehash separately. Its source candidate is not W1 evidence.
This proposal neither changes that design nor claims its source landed; adopting
W1 later requires explicit version/compatibility and all-participant migration.

## R3: separate native action fences

Every writer row below starts G, discovers and seals its full plan, acquires
P/C/H/S/W in order (empty sets allowed), performs declared D and drains constraints
before F. `actor` means same-service request admission plus exact original
session/person/epoch/method/auth-time/base/required-assurance binding; a public
principal is insufficient. `policy` means the exact native current policy,
including absence predicates, rechecked at F. `issue` means exact staged new
session plus exact planned old-cookie revocation, never authority before commit.
All applicable strict deadlines are the R6 table's original bounds.

| Action and actual entry | Discovery/acquisition and pretransition predicate | Final posttransition predicate and only after-commit output |
| --- | --- | --- |
| Registration: login.register → CreatePendingRegistration → BootstrapPending → QueueExistingChallengeTx | Reserve person/email/credential/challenge/workspace IDs; P insert new pending person, C contact/hash, H challenge, W pending personal workspace, D delivery. Address absence under G; exact normalized address and hash. | Exact pending person/contact/unrevoked credential; new unconsumed verification challenge bound to contact/digest/purpose with original E live; exact pending workspace/owner and material/job delivery. Generic registration acknowledgement, no session. Duplicate address preserves non-enumeration and no partial rows. |
| PasswordSignIn: login.signIn | Resolve exact active person/contact/credential and old-cookie owner under G; sorted union P then C, H empty, S old/new session, policy W. One Verify under C; any rehash same-tx. | Exact verified tuple or this attempt's rehash, state/epoch/policy and proof deadline, issue fields/rotation all match. Cookie/CSRF/authenticated result only after commit. |
| Email IssueVerification / QueueExistingChallengeTx | Pending contact/person P/C; H existing/new challenge plus rate-limit set; W owner-trigger set; D material/job. QueueExisting is a participant in registration, not a second root. | Unconsumed exact verification challenge/contact/digest and original E; pending person/contact unless existing idempotent route explicitly requires otherwise; exact delivery. Acknowledgement only; no verified flag/consumption/session demanded. Rate-limited/no-op remains generic. |
| Email Confirm | Resolve challenge/person/contact; P then C then H; pending owner workspace in W. Pre: unconsumed live exact challenge, matching contact and eligible pending/active account. | Email verified, intended person activation, exact challenge consumed by this attempt once, original E still live, workspace owner constraints satisfied. Confirmation success, no implicit session. Existing idempotent/no-op result does not mint proof. |
| Reset Request: recovery.request | Active verified contact P/C, complete prior unconsumed reset challenges H, D material/job. No credential proof is claimed; preserve rate cap and unknown-address no-op. | New exact reset challenge **unconsumed**, E live; prior selected reset challenges invalidated, exact contact/delivery. Generic accepted response only. No changed hash/epoch/session revocation required. |
| Reset Complete: recovery.complete | Prehash is preparation; under G rediscover exact contact/credential/challenge and **all unrevoked sessions across environments**, sorted P/C/H/S; policy W. Pre: live unconsumed token, active verified contact, permitted reset. | Exact new hash, epoch advanced once, exact challenge consumed by this attempt, every discovered unrevoked session revoked (including expired rows), original E live and policy still permits transition. Reset completion only, no new session. |
| Password Change: recovery.change | Actor plus exact current credential/verified contact under P/C; one current-password Verify then replacement Hash under C; complete session set S, policy W. | Exact old-to-new hash, epoch+1, full revocation; actor original deadlines and primary deadline live despite this attempt's intentional actor revocation. Change success only. No reset challenge is required. |
| Magic Request: magiclink.request | Active verified person/contact P/C, new H challenge/browser digest, policy W if configured, D. | New unconsumed purpose/contact/token/browser-bound challenge, E live, exact material/job. Generic accepted and bounded browser-flow cookie; no consumption, rotation or credential proof. Unknown address retains generic no-op/browser privacy behavior without asserting challenge existence. |
| Magic Confirm: magiclink.confirm | P union target/old-cookie owner; C exact verified contact; H live unconsumed token, same-browser binding or explicit other-device confirmation; S rotation; policy W. | Exact consumed challenge, unchanged contact/person/epoch/browser decision, original E live, issue/rotation. Cookie/CSRF only after commit; no other-device silent consent. |
| MFA BeginEnrollment | Actor P/S, primary credential/contact C, complete existing pending/active factor set H (including rows to expire), policy W; one Verify under C; local vault Seal with exact pending AAD. | New pending factor with exact ciphertext/scope/epoch/P, old expired pending transitions exact, no active factor, actor/primary/policy/P live. Seed/OTP URI/factor ID only after commit. No accepted step, activation, aal2 or session exists. |
| MFA ConfirmEnrollment | Same actor/primary plan plus exact pending factor H and old-cookie union S. Pre: pending epoch/P live, unlocked, accepted TOTP step after vault work, no active factor. | Active factor with exact re-sealed ciphertext, cleared pending fields, this step consumed, counters reset, exact aal2 issue/rotation; retained P/primary/actor/K/A live. Otherwise only R4 may commit counters. |
| MFA Challenge | Actor/primary plus exact active factor H, prior-session S, policy W. Pre: active unlocked factor, accepted unused TOTP step. | Same active factor, exact last_used_step update/counters reset and aal2 issue/rotation; actor/primary/K/A live. Replay or bad code uses R4, never this success fence. |
| MFA Status | Dedicated non-mutating reader transaction: actor person SHARE then session SHARE, factor plain read, policy workspace/resource closure; never renew/upgrade inside it. | Recheck actor/policy after all waits; enabled reflects active factor; pending only while P>F. Buffered FactorStatus after successful completion. No G, primary password, accepted step, factor update or issuance. Requires the reader/completion contract; cannot masquerade as a W1 success producer. |
| Federation BeginLogin | Before root, native AuthorizationURL is **local/no-network**, bounded and bound to exact configured connection/provider/issuer, generated flow/state/browser/nonce/PKCE S256, callback and safe return target. G then C enabled scoped connection; H insert reserved flow (P/S empty). | Same enabled connection/config and exact new unconsumed login flow, E live, null link actor fields; URL exactly matches staged inputs. Publish URL/browser cookie only after commit. No exchange proof, consumed flow or session required. |
| Federation BeginLink | Same local URL preparation; exact original actor/session digest captured by session adapter. G discovers bound person, P then C connection, H reserved flow, S actor; W policy. Pre: actor/base/assurance and auth+15m live. | After flow INSERT/unique/FK waits and drain, recheck original actor/person/epoch/base/assurance/auth+15m, connection and exact **unconsumed** link flow, E live and same return target. URL/browser cookie only; no binding or new session yet. |
| Federation CallbackLogin (Callback login branch) | Exchange outside G with R6 receipt. Under G resolve exact flow/connection/binding/person plus old-cookie union; P,C connection/binding,H flow,S rotation. Pre: unchanged enabled scoped connection, provider/issuer/subject binding (never email equality), unconsumed flow/state/browser/nonce/realm/intent and live provider/flow proof. | Exact flow consumed once by this attempt, unchanged binding/connection/person/epoch; original flow/provider/receipt bounds live; issue/rotation. Cookie and safe redirect after commit. |
| Federation CallbackLink (Callback link branch) | Exchange outside G; discover exact bound actor and session, P separately before C connection/binding and H flow then S actor. Pre: same actor/flow/session digest and all original link bounds. Stage unique binding at C-prerequisite after complete acquisition, no joined p,s lock. | Exact inserted binding/provider subject, exact flow consumed once, unchanged connection/actor/session/base/assurance/auth+15m plus provider/receipt/flow bounds after binding uniqueness wait. Safe redirect only, no issuance cookie. Retain every existing final-flow UPDATE predicate as a read-only final check of the intended transition. |
| Renewal: FindActiveSession + ActiveSessionAssurance / middleware | G resolves digest/owner, P then S; pretransition current state/epoch and original idle/absolute valid. | Exact renewal only, original E/I and staged I' live, conservative assurance downgrade; middleware admission proof only after commit and origin/CSRF admission. Reader RecheckCurrentTx remains non-renewing. |
| Signout / RevokeSession / RevokeSessionScoped | G discovery of configured-realm digest/person then P/S; duplicate-cookie/origin/CSRF rules retained. Missing/malformed scoped session can be idempotent no-op. | Exact targeted row revoked, no foreign row changed; report positive revocation/clear-cookie only after completion. Signout needs no new credential or elevated proof. |
| SetSessionAssurance | Only native successful elevation's existing attempt, planned P/S and method evidence, no standalone business setter. | Exact level/nullable shape/authentication/A within base E; base/primary/step bound live. No refresh from a passed-in wall clock. |
| Workspace stores / active bootstrap / account-state transition | Non-credential root with current actor/policy; G complete P actor/targets/personal owners, S actor; W complete owner-workspaces/memberships, both sides of moves. Existing trigger person SHARE revisits held P, owner UPDATE revisits W. | Intended workspace/membership/person state and epochs, refreshed actor/policy, exact active-owner invariant after drain. No owner transfer shortcut, no new credential. Future account-state/admin source needs a distinct grant; table is not a new public operation. |

### Action-specific schedule bindings

These are separate cases, not aliases claiming one callback test covers begin.
For every bound listed, include F just below, equal and just above it, plus an
observed downstream wait and canceled wait. Unknown-address/rate-cap no-op cases
retain generic output and stage no authority rows.

| Action | Required individual schedule and asserted output |
| --- | --- |
| Email request/registration delivery | W11/W24-email: actual material/job uniqueness and owner-constraint waits; exact unconsumed E, material and job deadlines; failure leaves no pending account/workspace/challenge/job or acknowledgement of committed issuance. |
| Email Confirm | W10: contact/person/owner wait after initial token validation; consumed transition rolls back at original E; no verified-success response. |
| Reset Request | W24-reset-request: challenge/material/job waits; compare complete prior challenges plus new unconsumed row; expired delivery/failed completion restores prior rows and publishes no committed-delivery claim. |
| Reset Complete | W09: credential/session/constraint wait after consume staging; original E equality rolls back hash/epoch/full revocations/consume. |
| Password Change | W08/W09-change: current Verify then replacement Hash; downstream wait at original actor/primary bound; no renewed actor time and complete rollback. |
| Magic Request | W24-magic-request: challenge/browser/material/job binds, no consumed flag or issued session required; valid request publishes browser-flow cookie only after commit; failed completion publishes none. |
| Magic Confirm | W12: same-browser and explicit-other-device cases, actual session uniqueness wait, original E equality; no partial cookie/consume/revocation. |
| MFA BeginEnrollment | W14/W24-mfa-begin: factor uniqueness/vault/constraint wait; actor/primary/P equality; valid case requires no code and returns seed once after commit. |
| MFA ConfirmEnrollment | W13-confirm: original pending P, actor, primary, accepted K and A each tested; original pending bound survives removal of pending columns; zero output on failure. |
| MFA Challenge | W13-challenge: active factor/replay plus original actor, primary, K, A; no pending-factor prerequisite, no second Verify. |
| MFA Status | W19-status: read waits at actor/policy dependency; actor expiry denies, live actor with pending P=F returns Pending=false; no mutation, password, step or cookie. |
| Federation BeginLogin | W24-begin-login: flow uniqueness and connection FK waits, unchanged enabled connection + unconsumed flow; valid case has no provider Exchange invocation or session creation. URL/cookie buffered. |
| Federation BeginLink | W25-begin-link: flow uniqueness/FK wait crosses original actor I/E/auth+15m/assurance; check after insertion, even though no provider proof/binding exists. URL/cookie absent on failure. |
| Federation CallbackLogin | W15/W25-callback-login: binding/connection change before G, then actual session uniqueness/FK wait; original flow/provider/receipt bounds, consumed transition and session required. |
| Federation CallbackLink | W16/W25-callback-link: actual binding uniqueness/FK wait; every original flow/provider/receipt/actor bound, no cookie issuance, no detached fresh actor. |

New begin/request deadlines (minimum one minute, or ten/thirty minutes) exceed
the five-second root ceiling. With a normally advancing DB clock, a real newly
created record cannot age all the way to that bound during a permitted root.
The real-service wait cases must therefore also show the earlier root timeout
wins. Equality at those otherwise unreachable final bounds is a separately
labeled deterministic final-clock fault test over real staged rows, not a claim
that a thirty-minute root was allowed or that synthetic clock injection measured
real expiry. Existing-token/actor/flow deadlines can be near expiry on entry;
those require actual observed waits with real clock sampling. Preserve both
kinds of evidence distinctly. F must never accept backward/future chronology
from the test seam merely to make a schedule pass.

## R4: CounterOnlyDenied is a separate commit branch

`Success`, `DeniedRollback`, `CounterOnlyDenied`, `UnavailableRollback` are closed
internal outcomes, not inferred from `errors.Is(ErrDenied)` or error text.
CounterOnlyDenied is selectable only by MFA ConfirmEnrollment/Challenge after
valid actor/current primary/policy, locked exact factor and local vault/code work,
**before any success mutation**. Determine replay from held LastUsedStep before
AcceptStep; a no-row AcceptStep is not permission to commit arbitrary work.
Already locked factor, malformed input, failed primary/policy, stale factor or
ordinary denial rolls back without counter changes.

For bad code or replay, freeze DB T after vault/code work and snapshot the entire
factor. Require matching scope, pending/active state, pending epoch/P if relevant,
actor and exact current primary, unlocked at T. Set N=1 if old locked_until<=T
and non-null, otherwise min(old.failed_attempts+1,5). Set L=T+15m if N>=5, else
NULL. The only permitted durable mutation is exactly this factor's
`failed_attempts=N, locked_until=L, updated_at=T`; require one affected row.
No seed/ciphertext/state/pending/last_used_step/activation changes, no credential
rehash, session issuance/rotation/revocation, delivery, workspace or other writes.
MFA primary Verify passes nil rehash persistence so it cannot dirty a credential
before choosing this branch. Attempt.RestrictCounter is selected before the write and
RecordMutation records every store/inline mutation in the attempt journal; a counter branch requires that journal to contain exactly
one allowed factor update. Store capability checks also prohibit success helpers
after counter selection. This is trusted native enforcement, not a SQL sandbox.

Drain the same constraints then sample F. Its **counter fence** checks complete
factor-row equality except those three exact changes; original actor base/required
assurance, primary V+15m, current person/epoch/contact/hash/policy and pending P
remain live, and F<T+30s. The thirty-second counter-attempt bound is fixed even
for bad code with no matching step; do not impose successful-TOTP/aal2/activation
predicates on a denial. Replay reason also binds observed last_used_step and
attempted matched step. No timestamp or deadline is reset at F.

Only a successful counter fence selects CounterOnlyDenied and returns nil from
the owning storage callback. Root.Run commits, invalidates the attempt and then
returns a counter Completion. Only then map the bound reason to ErrBadCode or
ErrReplay with zero issued result. A constraint/clock/policy failure, cancellation,
ordinary denied result or failed/unknown commit returns unavailable or denied
rollback as appropriate, never public committed-counter denial or success.
No headers/body/cookie/seed are emitted from the callback. A driver cannot prove
rollback after an indeterminate commit; return unavailable/no output and record
that limit, never report a counter as committed without completion evidence.

## R5: finite entry coverage

Scoped source search covers non-test Go in identity, workspace, apphost, delivery
and jobs at the pinned main and PR74 source (identical application source), plus
the PR76 signIn delta. The list below names every current authority-table mutator
or row-locking helper found. `I`=identity store NewWriter, `IS`=identity store NewIssuer with wp.Issuance,
`M`=MFA NewWriterStore,
`W`=workspace store NewWriter, `B`=personal NewWriter, `N`=native inline adapter
using Attempt.ParticipantTx. All require same live root/plan and the shown
acquisition prerequisite before SQL. Invalid/unrooted/legacy/wrong-phase access
is unavailable before SQL; legitimate stale authority is denied rollback. Read
helpers without locks remain nonauthoritative until their action's final fence.

| Current entry / owner | Owning root and capability / prerequisite |
| --- | --- |
| identity/store CreatePendingAccount, CreatePendingRegistration | Registration root, I; ordered P person insert, C email/credential inserts, H challenge insert. Compound method must advance substeps, not execute children before parent phase. PendingRegistration binds live attempt. |
| identity/store LinkExternalIdentity | Federation-link root, I/C after P and connection; exact reserved binding; no independent proof constructor. |
| identity/store AdvanceSecurityEpoch | Reset/change or separately admitted account-state root, I/P; full affected sessions/owner-workspaces must already be planned and acquired before staging. |
| identity/store CreateSession | Native issuing root, IS/S only via session StageWriter with wp.Issuance; planned new ID/old union. |
| identity/store RevokeSession, RevokeSessionScoped | Signout/revoke or issuing root, I/S; discover digest owner before P; scoped W1 wrapper rejects unscoped arbitrary target. |
| identity/store FindActiveSession (locking SELECT and renewal UPDATE) | Middleware renewal root, I/S after P; ActiveSessionAssurance shares this attempt, no separate renewal tx. |
| identity/store CreateChallenge, CreateMagicChallenge (including browser UPDATE) | Registration/email/reset/magic request root, I/H; exact new challenge and browser binding; D follows. |
| identity/store ConsumeChallenge (locking SELECT and UPDATE) | Email/reset/magic completion root, I/H; no early standalone consume transaction. |
| identity/store MarkEmailVerified | Email confirmation root, I/C; staged only after full acquisition; original H challenge evidence retained. |
| identity/store SetSessionAssurance | Native elevation root, IS/S plus same-attempt wp evidence checked by session; direct business use rejected. |
| identity/store FindCurrentPassword (joined p,c lock) | primaryproof called inside MFA/change root, I/C; replace joined locking with planned P then C; exact selected verified contact joins plan. No nested root. |
| identity/mfa CreatePending (expire old pending + insert) | MFABegin root, M/H; all old pending and active-absence set under G; new ID reserved. |
| identity/mfa ActivatePendingAndConsumeStep, AcceptStep | MFAConfirm/Challenge root, M/H after full acquisition; Success branch only. |
| identity/mfa RecordFailure | Same root, M/H CounterOnlyDenied branch only, exact factor and three-column transition. Find/FindCurrent are plain reads today; W1 adds planned H locking before their use in mutations. |
| workspace/store CreatePersonalWorkspace | Workspace-create/bootstrap root, W/W after owner P; exact reserved workspace. |
| workspace/store CreateOrganizationWorkspace, AddMembership, insertMembership | Workspace root, W/W after actor/target P and S; reserved IDs, complete target workspace/member set and active-owner invariant. |
| workspace/store UpdateMembership | Workspace root, W/W after P/S; membership plus OLD/NEW workspaces held before staging. |
| workspace/store SetWorkspaceState | Workspace root, W/W after affected owner/person union P and actor S; cannot start at workspace UPDATE then enter identity. |
| workspace/store lockWorkspace, lockActiveOrganization, lockPerson, lockActivePerson | Internal helpers under W: existing planned locks only (W or P respectively); P helpers may only revisit held P after W. Missing plan fails, no new reverse-order lock. |
| workspace/personal Bootstrap, bootstrapActivePerson | Active-bootstrap root or existing caller root, B/P for current person recheck then B/W + W methods. No nested root/commit. |
| workspace/personal BootstrapPending (person lock + workspace INSERT) | Registration root, B/P then B/W; live same-attempt pending registration, no tx-pointer-only provenance. |
| login.register inline expiry read; signIn rehash UPDATE | Registration N/H time captured once; signIn N/C same-tx rehash under held credential. PR76's separate rehash tx and SHARE issuance are legacy-only, explicitly not W1. |
| email.lockPendingContact, queueChallengeTx joined c,p,e lock | Email request/registration root, N/P,C,H separately, then D. IssueVerification and QueueExistingChallengeTx map here. |
| email.Confirm challenge lock, email UPDATE, person UPDATE | Email-confirm root, N/P,C,H acquisitions first; staged mutations revisit these rows; owner W planned before activation. |
| recovery.request joined p,e lock and prior challenge UPDATE | Reset-request root, N/P,C,H; complete prior challenge set, exact new challenge and D. |
| recovery.complete joined p,e / p,e,c locks, credential UPDATE, bulk sessions UPDATE | Reset-complete root, N/P,C,H,S then policy W; I ConsumeChallenge/AdvanceSecurityEpoch inside same attempt. No truncated session set, LIMIT, environment filter or SKIP LOCKED. |
| recovery.change joined p,c lock, credential UPDATE, bulk sessions UPDATE | Password-change root, N/P,C,S,W with actor and exact primary; full revocation across environments including expired/unrevoked sessions. |
| magiclink.request joined p,e lock + challenge INSERT; confirm joined p,e,c lock | Separate magic-request/confirm roots, N/P,C,H; request D, confirm S/W; I consume and session staging share root. |
| federation.checkConnection SHARE; begin flow INSERT | Separate BeginLogin/BeginLink roots, N/C then H; link P/S/actor fence also required. |
| federation.Callback joined flow/connection lock; final flow UPDATE | Separate CallbackLogin/CallbackLink roots, N/C then H; final consumption staged before drain, F is read-only and validates exact consumed transition. |
| federation.insertBinding joined p,s lock + binding INSERT | CallbackLink root, N/P then C then S acquisition (no joined lock); binding insertion revisits reserved C after all acquisition; unique wait precedes F. |
| federation.findLoginProof person UPDATE lock | CallbackLogin root, N/P already held with binding C; construct wp receipt, not detached wall-clock proof. |
| session.issueTx / Issue / IssueForRequest / IssueForRequestTx, Middleware, SignOut | Public legacy issuance refused in W1. Native StageWriter / renewal / signout roots dispatch only the I methods above; root begins before first leaf lock. |

No current separate admin/background mutator of these authority tables was found
in that scoped source search. This is a bounded source statement, not a claim
about external SQL or future services. Auth-limit admission/cleanup only changes
`identity_auth_limits` through Limiter.Allow/Cleanup; email material Cleanup only its table; job claim/finish/
maintenance only `amos_jobs`. They remain separate roots and may never acquire
identity/workspace locks afterward. Native material Put plus EnqueueTx are D
participants in the caller root; local vault Seal/Open add no SQL locks; native
policy reads are repeated at F. Apphost development binding insertion touches
runtime binding only and is not admitted in production W1 composition.

Any future invitation, admin, cleanup, provider-connection configuration or
account-state entry touching authority tables must add an exact action/row plan,
capability and schedule before W1 startup admits it. Unknown native variant or
unrooted writer is a construction failure, never a discovered-at-runtime fallback.
Arbitrary privileged SQL, migration-owner edits, hostile code holding a raw pool
or an old replica are explicitly outside this application's guarantee. The
[composition cutover](current-session-composition.md#r5-w1-profile-and-no-old-replica-cutover)
is mandatory; an application marker cannot constrain arbitrary SQL.

## Policy, trigger and completion closure

[Native password/MFA policy](../../apphost/password_policy.go) depends on active
person, active personal workspace, absence of non-left organization membership
and absence of enterprise binding. G serializes every writer of those predicates;
P gates the person and W holds positive workspace dependencies. Evaluate these
exact EXISTS/NOT EXISTS predicates after acquisition and again at F using the
staged transition's intended state. Do not translate them into configured owner
ID equality. Full organization/enterprise policy remains separately unimplemented
where the existing host says unavailable; W1 neither supplies nor removes it.

The concrete native callback suffix is finite. The
[TOTP keyring](../../identity/mfa/vault/vault.go) performs local AES-GCM work and
ignores its transaction argument; it adds no SQL lock. Preserve its exact
scope/factor/state/expiry/purpose AAD and copied key configuration; sample time
after its work. The [material store](../../delivery/email/materialstore/store.go)
encrypts then inserts `email_delivery_material`; the
[email enqueuer](../../delivery/email/email.go) validates/encodes the request then
calls [jobs EnqueueTx](../../jobs/sqlstore/store.go), whose idempotency insert may
wait and whose conflict path compares the request hash. Thus native D is material
row first, then job idempotency row, preserving the same transaction and exact
request hash/deadline. At F check material and job deadline against the original
challenge bound; neither transaction-start material time nor the job's
pre-insert clock suffices. Native delivery workers resolve material separately
and do not enter identity; a future worker that does must acquire G before any
job/material lock. Job-only/material-only maintenance may stay outside G only
when it never acquires identity/workspace locks and cannot change the authority
predicates being certified. Test actual material/job unique waits as W06/W11.
Audit/business participants are not called by these native producer methods;
adding them requires the composition owner's declared D order before use.

The membership kind/person trigger and three deferred owner triggers in
[workspace SQL](../../migrations/fragments/workspace.sql) remain authoritative.
After all mutations/callbacks, execute `SET CONSTRAINTS
workspace_active_org_owner_workspace, workspace_active_org_owner_membership,
workspace_person_state_owner_guard IMMEDIATE` (schema-qualified names in actual
trusted composition). Leave them immediate. Include any additional deferred
constraint introduced by a participating migration in the declared completion
plan; unknown deferred work blocks that composition. A constraint violation
rolls back, never produces success; deadlock/cancellation/database failure is
unavailable. PostgreSQL documents the retroactive checking performed by
[SET CONSTRAINTS IMMEDIATE](https://www.postgresql.org/docs/16/sql-set-constraints.html).

F follows this drain and all outbox/material/audit/business work that can wait.
The final read-only method fence checks staged consumed/revoked records against
attempt evidence; it must not accidentally demand that its own consumed challenge
remain unconsumed or that its intentionally revoked actor remain live. The
pretransition authentication check and the posttransition completeness check are
distinct. Other supported non-mutating authority readers use the reader candidate
again after resource waits and forward the refreshed principal explicitly.

The linearization boundary is successful F under held locks, conditional on
successful commit. Database locks prevent intervening state transitions through
commit; wall-clock validity is measured at F, not guaranteed at an unknowable
future client receipt instant. No transaction can promise that time stops during
commit or network delivery. A cookie already expired on arrival confers no
subsequent authority. If an operation requires a different completion-time
validity promise, that is an unresolved operation contract, not an invented
commit timestamp guarantee.

## Required real-service schedule matrix

These schedules are **required and unrun**. Use real PostgreSQL with the intended
TLS runtime role, exact migrations and two distinct backend connections A/B.
Use channels/hooks at named phases and database-observed lock waits/transaction
IDs, bounded deadlines and cancellation; no sleeps as synchronization. Record
source head, role/mode, barrier observation, exact final row sets, typed result
and absence of premature cookie/output. Observer inspection must not mutate
state. Each normal case runs A-first and B-first unless its row specifies a
one-sided fault. A fixture deliberately bypassing G is a labeled adversarial
lock holder, not a supported production writer.

`Allow` means only successful method/result after commit; `Deny` means authentic
invalid/stale proof or policy rejection using that route's existing public
mapping; `Unavailable` means failed establishment/dependency/mode/cancellation.
`Rollback` includes every staged session/challenge/factor/credential/workspace
and output, except the explicitly isolated MFA denial-counter branch.

| ID | A / barrier | B / release or competing action | Required result and rejected-protocol negative |
| --- | --- | --- | --- |
| W01 | Reader holds person SHARE before session SHARE | Each W1 root attempts P; reader finishes | B waits before any child lock; reader Allow, then B proceeds. Negative: leaf-added person gate after old-session UPDATE must expose inversion/deadlock or fail order assertion. |
| W02 | Writer holds P and stages disable/epoch increment | Reader starts; A commits or rolls back | Commit: reader Deny; rollback: original reader Allow if time live. Also cancel reader: Unavailable, no consumer invocation. |
| W03 | Reader holds exact session SHARE | Renewal, signout, scoped revoke and assurance writer attempt | Writer waits at P under W1, not with reversed session/person locks. After reader releases, expected renewal/assurance Allow or revocation success; later reader sees revoked Deny or bounded assurance. Negative: direct leaf bypass rejected. |
| W04 | Rotation target P and old owner Q, both UUID orders; repeat P=Q | Reverse Q→P rotation and reader on old session | Serialized writer gates, no deadlock; complete old-row revocation/new-row issuance on commit only. Bad-realm/missing old cookie changes no foreign row. Negative: discover old owner only in leaf fails. |
| W05 | Fixture holds referenced person key-changing UPDATE; producer waits at P; separately fixture holds KEY SHARE | Release by commit/rollback | Producer rechecks state/epoch after P; Deny stale, Allow unchanged/live. Actual insertion tests exercise both person-id and scoped FK. Reader SHARE + FK KEY SHARE compatible; UPDATE-reader mutant demonstrates conflict. |
| W06 | Fixture inserts colliding session ID/digest, challenge digest, email comparison key, external subject, factor key, material key or job idempotency key without commit | Producer reaches actual unique wait; fixture commits or rolls back | Commit: defined conflict/non-enumerating denial; rollback: insert may succeed, but expired proof/challenge/flow at F Deny + Rollback. Inject callback pause after insert to test equality. Negative: CreateSession's pre-insert clock alone wrongly allows. |
| W07 | Password login waiting before G/C, then exactly one Verify under C | Change hash/revoke credential/unverify or replace contact/disable/advance epoch before acquisition; also attempt change after C | Before: current held tuple governs Verify, old password Deny; after: writer waits. Same-epoch replacement covered. W1 rehash atomic with session; late failure rolls both back. Negatives: detached pre-G proof, epoch-only comparison or second Verify rejected. v1.25 standalone remains distinct. |
| W08 | Reset/change holds G and complete sessions include multiple environments plus expired/unrevoked rows | Issuer waits at G; A commits/rolls back | Commit revokes exact entire prior unrevoked set and increments epoch once; waiting issuer's old evidence Deny. Rollback restores full set/hash/epoch. Negative: LIMIT, environment filter or SKIP LOCKED misses rows. |
| W09 | Reset/challenge consumed and hash/epoch staged; pause before F | B holds declared later workspace/outbox dependency; release after challenge/proof deadline | Deny + Rollback all reset/change writes; no consumed-token-only commit. At strict equality also Deny. Negative: consumption-only freshness fence allows. |
| W10 | Email Confirm locks/stages email and person | B delays owner constraint completion; challenge reaches expiry before F | Deny + Rollback verification/activation/consumption. Valid completion Allow. Negative: landed narrow consumption fence alone is insufficient after later trigger wait. |
| W11 | Registration/resend staged | Competing same address registration or delayed queue/constraint | Exactly one account/workspace, generic duplicate response; queue failure/expired challenge rolls back all staged rows. Negative: pending bootstrap/queue nested commit leaks rows. |
| W12 | Magic confirmation verified browser/contact/challenge and staged rotation | Delay policy/workspace/material or old-session phase past original challenge deadline | Deny + Rollback challenge/new session/old revoke; live same-browser and explicit other-device Allow; absent confirmation Deny. Negative: transaction_timestamp proof or early consume allows expiry. |
| W13 | MFA password validated; factor/vault work or rotation paused | Deadline/window/pending expiry passes; or second request accepts same step first | Deny + Rollback activation/session/rotation; replay commits only bounded counter branch. Valid pending and active variants Allow. Negative: pre-vault clock, unlocked factor read or refreshed primary deadline permits stale elevation. |
| W14 | MFA enrollment seed/ciphertext staged | Fail final policy/constraint/commit; competing active-factor enrollment | No seed disclosure on failure; no duplicate active/pending authority; valid commit returns bounded seed enrollment. Negative: output before commit leaks provisional secret. |
| W15 | Federation exchange completed; callback blocked before G/P/C | B disables connection, changes binding, consumes flow or advances person epoch | Deny, no binding/cookie; SQL failure Unavailable. Exact unchanged valid flow Allow. Negative: provider result or flow preflight treated as final proof allows. |
| W16 | Link binding INSERT waits on external-subject unique contender; repeat login session insertion wait | B rolls contender back after flow/authentication/assurance/provider-proof deadline | Deny + Rollback binding/session/flow consumption. Valid uniqueness conflict remains non-enumerating. Negative: separate person lock only inside insertBinding or omitted final flow fence fails. |
| W17 | Two owners of one active organization; A disables/removes owner one | B disables/removes remaining owner after G release | First transition Allow; last-owner transition constraint rejection + Rollback. Repeat opposite workspace traversal orders and membership OLD/NEW move: no lock cycle, both workspace invariants preserved. Negative: deferred locks outside planned W or missing gate fails. |
| W18 | Native recovery/MFA policy observes organization/enterprise absence | B inserts non-left membership/enterprise binding or suspends personal workspace | B waits on G if A first; if B first, A's policy Deny. No stale absence allow. Negative: person SHARE-only membership gate permits predicate race. |
| W19 | Reader authority acquired, then waits on declared workspace/resource | B releases unchanged resource after A/C/E or idle boundary | Reader returns candidate downgrade/denial correctly, consumer reevaluates required level; all four assurance rows and strict equality tested. Fresh persisted elevation never upgrades admission. Negative: reuse original principal allows. |
| W20 | Any root reaches constraint drain/F/commit | Inject query/scan/mode/timeout/cancellation/commit failure and callback attempt to open tx or acquire earlier lock | Unavailable + Rollback, zero output; unsupported mode denied before mutation. Bad stored shape Unavailable, valid stale authority Deny. Negative: swallow callback/commit failure reports success. |
| W21 | Capture attempt/evidence/staged Issued | Reuse after rollback/commit, different tx/service/context, duplicate cookie or fabricated public principal | Reject before authority use; no SQL for failed private provenance admission where specified. No partial cookie. Negative: tx pointer alone or detached VerifiedCredential accepted. |
| W22 | Request middleware transaction succeeds or fails admission/commit | Invoke reader with absent/cross-instance proof, mutated constructor origins, duplicates | Candidate outcomes hold, no proof after failure, defensive configuration copy effective. Repeated recheck only reacquires held compatible rows. Negative: minted-before-commit or mutable origins succeeds incorrectly. |

| W23 | Runtime role executes exact G lock; A holds G | B attempts G then A releases; separately try INSERT, DELETE, TRUNCATE, UPDATE(id), ALTER/DROP, role escalation, and actual unsupported modes | Positive serialization with SELECT(id)/UPDATE(lock_slot); all prohibited actions fail. Privileged fixture removes row: root unavailable, no domain writes. Cancel B: rollback/no output. Missing UPDATE(lock_slot) mutant fails positive admission; broad grant mutant fails denies. |
| W24 | Each request/begin stages challenge, pending factor or flow then waits on actual unique/FK/material/job/constraint dependency | Release just before, at and after each original bound (use a controllable DB-time test seam only where labeled, never sleeps) | Email/reset/magic request require unconsumed exact record and live delivery; MFABegin requires pending factor/primary/actor/P, no TOTP/session; BeginLogin requires live unconsumed flow/enabled connection only. Valid cases commit URL/cookie/seed/ack, equal/expired rollback with zero provisional output. A completion-fence-on-begin mutant fails the valid case. |
| W25 | BeginLink after flow insertion; CallbackLink after binding insertion; CallbackLogin after session insertion | Separate observed unique and FK wait schedules crossing flow E, actor I/E, auth+15m, assurance, provider/receipt bounds where applicable | BeginLink rechecks actor/connection/unconsumed flow without provider proof; callbacks require consumed transition/provider proof. Every applicable equality denies and rolls back. Missing begin final fence mutant wrongly publishes URL/cookie; no begin test is credited from callback-only W15/W16. |
| W26 | MFA bad-code and replay select counter branch at exact held factor | Delay drain/F across primary/actor/pending/T+30s; inject policy failure, cancellation, failed/unknown commit | Valid counter commit returns bound ErrBadCode/ErrReplay only afterward; complete rows differ solely in failed_attempts/locked_until/updated_at. Expired/stale/policy denial rolls back; dependency/commit failure unavailable/no result. Check all sessions, credentials, factors and workspace/delivery rows, not counters alone. Broad ErrDenied-swallow and post-success counter mutants fail. |
| W27 | Call every R5 entry through old New(tx), rooted tx via old New, zero/foreign/closed Attempt, unplanned row or wrong phase | Try W1 startup after Legacy object; try old replica with legacy DB access, then exact reviewed no-old-replica cutover | All application capability negatives reject before SQL; legacy binary with access explicitly demonstrates bypass and blocks admission until cutover denies it. Native inventory normal paths retain full supported suite. Unknown background writer/configurable callback rejects construction. |
| W28 | Hold G before root launch; also delay Verify, vault, each phase and completion under original deadline | Test absent/zero/expired/short/long request deadlines, contention across unrelated persons and environments; cancel waiting root | Five-second total ceiling or earlier request wins, no reset/fallback/retry/output. DB freshness equality and all R6 individual bounds tested with original evidence unchanged. One Verify counter asserted; detached pre-G password and post-wait deadline refresh mutants fail. Measure intended-load admission/timeout behavior; safe timeouts alone do not qualify useful throughput. |

For every negative, the independent reviewer must observe the intended bad
outcome/assertion with the rejected protocol, restore exact reviewed bytes and
rerun the normal schedule. SQL deadlock victims returning unavailable are not a
passing liveness proof. A bounded unique/FK fault returning unavailable is safe
failure but does not establish the intended successful schedule. Missing service
prerequisites fail visibly, never SKIP or fixtures-as-provider qualification.

## Remaining gates and exact ownership needed

This document resolves a concrete proposed ordering and producer protocol; it
does not close these delivery gates:

1. Independent combined design review/adoption, including the explicit singleton
   serialization tradeoff and versioned seam compatibility. The reader candidate
   and frozen amendment/ADR remain integrator-owned and unchanged here.
2. Trusted same-database/schema/runner composition, transaction completion owner,
   current workspace/resource/policy handoff and declared downstream participant
   closure. Arbitrary injected callbacks/providers are not qualified by a list of
   native callers; missing bounded evidence or undeclared locks blocks that
   composition. Federation exchange must supply its real validation deadline.
3. Explicit source ownership for **all** rows above. Integrator owns migration
   gate/privileges/ordering, shared seam and executable composition; identity
   owners separately own session/store, login/rehash, primary proof, recovery,
   email, magic, MFA/vault and federation adapters; workspace owner owns stores,
   bootstrap and trigger changes; policy/host owner owns callback composition.
   Delivery/outbox/audit/resource owners must grant their completion adapters.
   No leaf assignment may change another owner's caller or shared schema.
4. Implement the finite entry inventory above and re-run its scoped source search
   at the exact source head; newly found writers require explicit admission. No
   concurrent legacy mutation remains during adoption. Deployment must coordinate transition to W1 across replicas.
   An unaudited writer is a blocker. No application protocol proves safety against
   arbitrary privileged SQL bypass; runtime privilege/composition evidence is
   separately required.
5. Implement and run W01–W28 plus ordinary focused tests/race/lint, independent
   negatives/restoration and fresh landed checks with exact-source evidence.
   Validate singleton contention/timeout behavior for the intended load; this
   design intentionally trades write concurrency for a finite safe protocol.
6. Existing product-method/provider gaps remain visible: this does not implement
   passkeys, organization policy, external-provider validation or acceptance for
   them. Their future producers must satisfy the closed W1 evidence API, not mint
   generic detached proof.

No real-service schedule, runtime, provider call, source implementation or task
acceptance was performed to produce this artifact. Original/current/product
registries and foreign source ownership remain unchanged.


## B1 bounded password adapter proposal

[The exact computation adapter](current-password-work.md) closes the proposed
R6 implementation direction using the existing hasher and a globally bounded,
context-responsive pure-computation worker. It explicitly distinguishes caller
cancellation from Argon2 CPU termination and grants no authority to late results.
This additive proposal requires independent review before source; the complete
writer, caller ownership and real W28 integration gates remain open.
