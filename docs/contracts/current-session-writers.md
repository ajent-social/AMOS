# Current-session writer and producer protocol (CP2 candidate)

Status: proposed finite design resolution, **not adopted, implemented, qualified,
source-ready or accepted**. Source inspected at
`fa6c5a35e61ed4f866652acc66addec1d76fb368`. This supplements the
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

W1 adds a migration-owned singleton `identity_writer_gate` with exactly one
immutable primary key `id=1`. Every transaction that mutates persons, contacts,
credentials, external bindings, challenges, factors, federation connections or
flows, sessions, workspaces or memberships first executes a separate
`SELECT id FROM identity_writer_gate WHERE id=1 FOR UPDATE`. Missing gate or
unsupported transaction mode is unavailable. Runtime code cannot insert, delete
or change its key. The migration owner assigns the migration number and grants.
This gate spans the trusted database, including all installations/applications
and environments: existing token/subject uniqueness and person-wide revocation
are not all environment-local. It is not a distributed service or new credential.

The root acquires this gate before **any** domain row lock, mutation, foreign-key
insertion, policy callback or deferred constraint work. Plain nonauthoritative
lookup and bounded password/provider computation may happen before the root,
but their results must be bound and validated below. No upgrade from an existing
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

## Exact producer seam and lifetime

The existing `VerifiedCredential` contains person/realm/epoch/method/time and
assurance, not the credential row/version or a finalization callback.
`Issue`/`IssueForRequestTx` cannot recover that evidence from the value.
The source correction must add an **identity-internal versioned** producer
transaction seam; business code cannot implement it or manufacture its inputs.
The following names define the proposed semantics, not declarations already present:

The shared ordering capability belongs in an integrator-owned repository-internal
package (proposed `internal/authoritywriter`), usable by identity and workspace
packages without violating Go's `identity/internal` import boundary. Its attempt
type may be exported **inside that internal package**, with private fields and
root-only construction. Method credential evidence and producer registration stay
inside identity's internal boundary. No application extension implements a
producer. The root, acquisition, staging and finalization interfaces are shared
source ownership, not a permission for this document's author to add them.

- `RunProducerV2(ctx, requestAdmission, methodInput, priorCookie, producer)` owns
  one W1 transaction, with the configured trusted runner. Session code alone
  parses the configured opaque cookie, rejects duplicates, computes its digest
  and resolves the realm-scoped old row. `methodInput` is the selected native
  method's bounded input, never a caller-asserted person/epoch/assurance value.
- `DiscoverV2(ctx, tx)` returns a private plan: exact realm; target and actor
  IDs resolved from authoritative records; exact email/credential/challenge/
  factor/flow/connection/binding IDs; prior-session digest binding; all session,
  workspace and callback dependency sets; selected method. It runs under G,
  before P, without locks or writes. A plan is not authority.
- Root acquisition returns a sealed `WriterAttemptV2`: exact root/service
  instance and `*sql.Tx`, single-use attempt nonce, immutable plan, held-phase
  bitmap and live/failed/finalized state. It never attests the database behind
  an arbitrary `*sql.Tx`; composition binds that separately. No public context
  setter, DTO, serialization or configurable app parser is added.
- `VerifyAndStageV2(ctx, attempt)` runs once after acquisition. It returns private
  `ProducerEvidenceV2` and staged issuance, not an authenticated principal.
  Evidence contains exact source row IDs and compared values, realm/person/epoch,
  verified method, database verification instant, absolute proof deadline,
  method-specific replay/binding values, and the attempt nonce. Plaintext
  passwords, seed material and raw cookies are transient adapter inputs only;
  they are not stored in evidence, SQL, logs or the public result.
- `FinalizeV2(ctx, attempt, evidence, dbNow)` is mandatory after every staged
  blocking participant and constraint drain. It reads held rows only, compares
  the exact predicates in the table below and returns a sealed private proof
  or a typed denial/unavailable error. It cannot acquire locks, write, call a
  provider, rerun password hashing or refresh the original proof deadline.
  `dbNow` is sampled by the root with a separate `clock_timestamp()` query after
  all waits; callers cannot supply it. Strict expiry is `dbNow < deadline`.
- `FinishV2` invalidates attempt/evidence on any error, rollback, cancellation or
  successful use. Staged cookie/CSRF results stay private until the runner reports
  commit success. No capability survives transaction end or travels to another
  transaction, service or request. A supplied-Tx participant requires an existing
  root capability **before its caller's first lock** and cannot complete the root.

The root stages session fields using the verification instant and original proof
bounds, then validates them at F; it does not extend validity to compensate for
waits. `issued_at` and idle bound must remain coherent with the schema and final
clock; an expired staged session rolls back. Set elevated assurance while staging
and validate level, nullable shape, authentication time and expiry at F. Do not
fall back to aal1 when issuance requires successful step-up; reader downgrade is
a different operation.

For an authenticated mutation, evidence also carries the exact admitted actor
session ID/digest binding, original method/authentication time and the minimum
applicable idle/absolute/assurance/fresh-auth deadline. Validate that live session
before staging any self-revocation. At F require the original time bounds still
live and distinguish only this attempt's intentional revocation from prior
revocation; creating a new session cannot refresh stale input authority. The
same rule applies to password change, MFA rotation and authenticated linking.

For federation, the additive provider result must return exact issuer, subject,
nonce binding, validation-completed instant, strict `ValidUntil`, and method/
connection binding in an adapter-owned immutable result. `ValidUntil` is the
minimum of actual provider-proof validity and the adopted method's exchange
receipt lifetime; the flow deadline is an additional bound. The present
[federation Identity](../../identity/federation/federation.go) has only issuer,
subject, nonce and email, so this bound cannot be reconstructed. Mapping that
bound for each concrete provider is an explicit remaining adapter contract gate;
this design does not invent a token parser, provider expiry or remote revocation
guarantee. Exchange failure is unavailable; verified stale evidence denies.

Legacy exported signatures may remain as compatibility adapters, but a detached
proof-only call cannot establish V2 freshness. Migrate every native caller to the
root seam in the same adoption program. Keep the old path explicitly unqualified
until that migration is complete; do not expose the candidate host with selected
methods silently removed. A separately distributed credential adapter with only
legacy proof is an unresolved integration until it supplies the specified native
producer evidence. No source grant follows from this proposal.

## Required corrections by actual native path

Links name source files at the baseline above; symbols identify the exact graph.
Each row requires G–F, not just a lock inserted into its last function.

| Path / source graph | Required correction and final evidence |
| --- | --- |
| Registration: [login](../../identity/login/login.go) `register` → [store](../../identity/store/store.go) `CreatePendingAccount` / [registration capability](../../identity/store/registration.go) → [pending bootstrap](../../workspace/personal/registration.go) → [email queue](../../identity/email/email.go) | Acquire G before person/contact/credential/challenge insertion. Preserve atomic pending person + personal workspace + protected material/outbox, and the exact transaction-bound pending capability. Split queue's joined child/person locks; all needed identity rows are already held before W. At F require exact pending/contact/challenge binding, unconsumed unexpired verification challenge, active personal workspace and queued material deadline. A duplicate scoped address yields the existing non-enumerating accepted response after rollback, no second person/workspace and no session. A callback/constraint failure rolls back every staged row. |
| Password login: [login](../../identity/login/login.go) `signIn` → password Verify / persist-rehash → session IssueForRequest | Existing read transaction and outside verification are not current issuance evidence. Retain bounded prehash verification only if its private receipt binds exact credential ID/hash and verified email ID/comparison key/person/realm. Under G/P/C compare all values, active state and current epoch; changed hash/contact denies and requires a new request, not an extra budget-consuming Verify. Prefer performing one verification under held C with the existing admitted budget. Rehash must be a conditional update in this same W1 attempt; evidence records old verified hash and exact newly staged hash. Remove the nested rehash transaction. Sample database verification time after actual Verify. At F compare credential not revoked, exact staged hash, verified contact and state/epoch; original proof age/lifetime remains bounded. |
| Issuance and old-cookie rotation: [session](../../identity/session/session.go) `Issue`, `IssueForRequest`, `IssueForRequestTx`, `issueTx` → [CreateSession](../../identity/store/store.go) / [scoped revoke](../../identity/store/magic.go) / [assurance write](../../identity/store/assurance.go) | Resolve target AND old-cookie owner under G before any person lock; lock sorted union at P even when old cookie belongs to another person. Missing/malformed prior token retains existing no-prior behavior; duplicate cookies deny; other realm cookie cannot revoke. Revalidate immutable digest/person/realm binding at S; revoke only exact old row and insert new row atomically. Validate target method evidence after actual session FK/unique/assurance waits. No person lock or credential callback first acquired inside `issueTx`. Any insertion/fence/commit failure restores old session and emits no cookie. |
| Reset request and completion: [recovery](../../identity/recovery/recovery.go) `request`, `complete`, `resetPreflight` → shared ConsumeChallenge → password update → AdvanceSecurityEpoch → session bulk UPDATE | Preview/preflight is advisory. Move G/P before contact/challenge locks; separate joined clauses. Acquire credential before challenge, sessions before policy workspace work. Preserve exact challenge purpose/digest/person/email and verified contact. Stage new hash, one epoch increment and **all** `identity_sessions WHERE person_id=$1 AND revoked_at IS NULL`, across environments, including expired rows; no LIMIT/SKIP LOCKED/batch commit. Under G no issuance can enlarge the set. Consume challenge after blocking staging, then drain constraints and at F require its original expiry still live, exact consumption by this attempt, binding unchanged, credential equals staged hash and epoch equals old+1; new revocations equal the complete pretransition live set. Original active-account predicates remain required. |
| Password change: [recovery](../../identity/recovery/recovery.go) `change` / [primary password](../../identity/primaryproof/password.go) / [password store](../../identity/store/password.go) | Bind preverified hash exactly, acquire actor and target P before credential locks, and actor session S before workspace policy. Require private request admission/current session, not a principal value alone; verify current session before staging revocation and preserve its pretransition authority as attempt-bound evidence. Split `FOR UPDATE OF p,c`. At F verify original credential receipt, new staged hash, incremented epoch, complete revocation set, policy and proof deadline; do not require the intentionally revoked actor session to remain active after the transition. A stale/revoked actor before staging denies. |
| Verification/resend: [email](../../identity/email/email.go) `Confirm`, issue and queue helpers → ConsumeChallenge | Current Confirm locks challenge before email/person; move discovery before P and actual child locks into C/H. Preserve the landed shared-consumption-after-email/person correction. At F after owner constraints, require exact staged verification/activation and consumed challenge binding, strict original expiry and consumption by this attempt. Resend/invalidation and mail queue also use W1; expire/revoke the full selected open-challenge set atomically and fence new delivery expiry. No session is minted. |
| Magic request/confirmation: [magiclink](../../identity/magiclink/magiclink.go) `request`, `confirm` → policy → ConsumeChallenge → IssueForRequestTx | Replace joined person/email/challenge locking with P/C/H; resolve cross-person cookie before P. Lock verified contact, challenge and complete session set before policy work. Preserve browser digest or explicit different-device confirmation, purpose/digest/recipient/realm, current active state/epoch and one consumption. Replace transaction-start proof time with post-verification database time; preserve original challenge deadline. At F require same challenge consumed by this attempt and not expired, contact still verified, exact browser confirmation, policy and newly staged session/proof lifetime. Failure after consumption/rotation rolls both back. |
| MFA enrollment and step-up: [MFA](../../identity/mfa/mfa.go) `BeginEnrollment`, `verifyAndStepUp` → policy/primary → [factor store](../../identity/mfa/store.go) → vault → issuance | Move discovery/G/P/C/H/S ahead of policy and vault work. Verify password once under C; current actor session is required and checked under S. Lock pending/active factor set before reading ciphertext. Vault callbacks are bounded local work on declared capability, no nested transactions. Sample DB time after password/vault work, validate primary freshness, pending expiry/epoch, lockout and replay before staging. At F require original pending deadline still live for enrollment confirmation, unchanged factor identity/ciphertext provenance, accepted TOTP step still within the configured time window at final DB time, exact staged last_used_step/activation, primary freshness and aal2 deadline. Never accept a new step at F or consume the password budget twice. Rejected code/replay may commit only the intended failure counter/lockout as a distinct denial outcome; no session/activation/rotation may survive that branch. Status is read-only but needs separately current policy; enrollment seed is disclosed only after commit. |
| Federation begin/login: [federation](../../identity/federation/federation.go) `begin`, `Callback`, `findLoginProof`; [schema](../../identity/federation/schema.sql) | Provider exchange remains outside W1; bind verified issuer/subject/nonce and provider validation lifetime to exact flow/connection, never email matching. Resolve binding/person/old-cookie union under G. Move flow UPDATE/connection SHARE out of joined early clause into C/H after P; lock exact binding at C. At F require enabled exact scoped connection/provider/issuer, unchanged subject binding, flow state/browser/nonce/intent/realm and strict flow/provider-proof deadlines; consume exactly once and validate staged session. An adapter whose exchange result lacks a bounded validation deadline needs the versioned evidence addition before adoption; local wall time cannot invent token validity. |
| Federation link: same Callback → `insertBinding` → final flow UPDATE | Link begin also enters G, resolves bound person/session, locks P then S and rechecks fresh admitted authority before storing intent. Callback resolves the same tuple and locks person separately before session; remove unordered `FOR UPDATE OF p,s`. Preserve exact flow/session digest, person/realm/epoch, live base/assurance and 15-minute authentication requirement. Stage binding uniqueness; after that wait and constraint drain, run final flow/session/person/connection fence with one fresh DB instant. Retain every existing final UPDATE predicate; SQL failure is unavailable, valid stale/missing authority denies, uniqueness conflict stays non-enumerating. No cookie issuance for link. |
| Renewal: [store](../../identity/store/store.go) `FindActiveSession` → [ActiveSessionAssurance](../../identity/store/assurance.go) → middleware | Existing leaf session UPDATE then unlocked person is not itself a conflicting reverse edge, but W1 integration moves root G/P before S, including middleware's transaction. Resolve digest under G, lock person, then session, sample fresh time, retain strict idle/absolute and current state/epoch checks. Validate again at F; renewal cannot resurrect a session which expired while waiting. Middleware proof is minted only after commit/admission, with candidate duplicate-cookie/config-copy rules. RecheckCurrentTx itself remains non-renewing. |
| Signout/revoke/assurance: [session](../../identity/session/session.go), [store](../../identity/store/store.go), [scoped revoke](../../identity/store/magic.go), [assurance](../../identity/store/assurance.go) | Root G/P/S even for these formerly session-only leaves; no caller retains a session lock before entering G. Signout uses configured realm and duplicate-cookie rejection, preserves origin/CSRF and idempotent missing-session result. A positive revocation cannot be reported before commit. Assurance update requires producer evidence, fresh final base session and strict elevated bounds, never transaction-start expiry. Bulk account/credential revocation uses the same complete-set rule as reset. |
| Workspace/bootstrap and account state: [workspace store](../../workspace/store/store.go), [active bootstrap](../../workspace/personal/bootstrap.go), [owner triggers](../../migrations/fragments/workspace.sql) | `SetWorkspaceState` cannot start at workspace UPDATE then enter identity. Discover actor/targets/personal owner and affected owner-workspaces under G, acquire P and any actor S first. Prelock complete W set sorted, including every active organization where a state-changing person is an active owner and both sides of membership moves. Membership trigger's person SHARE only revisits P. Deferred owner function's workspace UPDATE only revisits W. Preserve active-owner invariant, no owner transfer shortcut. Re-evaluate owner existence after waits under G and drain constraints before F. All workspace writers, including invitation/admission/admin/job paths when implemented, must enter W1; no bypass via direct store calls. |

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
| W07 | Password verified/prehash receipt captured before G | Change hash/revoke credential/unverify or replace contact/disable/advance epoch commits | Login Deny, no cookie; unchanged tuple Allow. Repeat with same-epoch hash replacement. Rehash success atomic with session; injected later failure rolls both back. Negative: epoch-only detached proof allows stale password. |
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
4. Inventory and migrate administrative, cleanup and background writers touching
   the listed tables, not just HTTP routes; no concurrent legacy mutation remains
   during adoption. Deployment must coordinate transition to W1 across replicas.
   An unaudited writer is a blocker. No application protocol proves safety against
   arbitrary privileged SQL bypass; runtime privilege/composition evidence is
   separately required.
5. Implement and run W01–W22 plus ordinary focused tests/race/lint, independent
   negatives/restoration and fresh landed checks with exact-source evidence.
   Validate singleton contention/timeout behavior for the intended load; this
   design intentionally trades write concurrency for a finite safe protocol.
6. Existing product-method/provider gaps remain visible: this does not implement
   passkeys, organization policy, external-provider validation or acceptance for
   them. Their future producers must satisfy V2, not mint generic detached proof.

No real-service schedule, runtime, provider call, source implementation or task
acceptance was performed to produce this artifact. Original/current/product
registries and foreign source ownership remain unchanged.
