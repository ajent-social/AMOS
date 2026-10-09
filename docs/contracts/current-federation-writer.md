# Native federation in the W1 writer protocol

Status: early design for independent review under T3.10; no source grant,
provider qualification, migration allocation or host admission. Source baseline:
`ecab2421c77ae2ad6eee56f46e67903267e6f67a`.
Read with [current-session writers](current-session-writers.md), the
[exact writer API](current-session-writer-api.md),
[private runtime composition](current-session-composition.md) and the
[identity contract](identity.md). Preserve the complete selected identity suite.

## 1. Actual source boundary and decision

`identity/federation` currently owns durable browser-bound begin/callback flows,
legacy `*storage.DB` transactions and explicit external-identity linking. Its
`Provider.Exchange` returns only `Identity{Issuer, Subject, Nonce, Email}`.
There is no provider proof expiry, validation-start/completion time, immutable
native adapter receipt, or W1 constructor in that result. The only concrete
Provider implementation in the inspected source is the test HTTP harness.
Provider implementations remain separate tasks: Google T3.11, GitHub T3.12,
Apple T3.13 and the enterprise OIDC work. T3.10's historical component acceptance
does not qualify those adapters or this W1 composition.

The decision is to compose federation through the existing Root and writerproof
factories, never wrap the old Exchange result with a guessed expiry. A legacy
Identity, email equality, caller-selected person, asserted `ProviderValidUntil`,
or local `time.Now().Add(...)` is not a native provider proof. The absence of a
qualified receipt is an explicit **unavailable callback dependency**. Do not
implement a permissive compatibility adapter or claim an always-unavailable
callback is completed federation work.

The target is additive `NewWithWriter(root *aw.Root, cfg Config)` construction,
with `cfg.DB == nil` and the exact privately retained native session service;
existing method shapes for BeginLogin, BeginLink and Callback remain. The native
composition owns the one RuntimeDB, Root, service and immutable configuration.
It must not accept arbitrary external Root/session pieces as public host input.
Legacy New and supplied-transaction paths must remain excluded from W1 through
the existing profile protocol. Changes to legacy guards require explicit source
ownership, preserve Legacy behavior, and cannot select a second transaction owner.

Constructor validation copies connections/maps/configuration, rejects nil and
typed-nil dependencies, realm/configuration disagreement and unsupported native
provider profiles. Copying a map does not make an arbitrary Provider immutable.
The W1 provider admission manifest names concrete reviewed adapter types and
exact verification versions. The legacy extensible Provider interface may not
be promoted to credential authority by adding a validity callback. No arbitrary
provider, verifier, discovery, finalizer, clock or evidence factory is accepted
from business code. Actual provider construction/credentials remain owned by its
separately assigned native adapter and composition; this document supplies none.

## 2. Provider validity: receipt requirements and remaining ownership

The native exchange path has this fixed sequence before entering writer G:

1. Under the original bounded callback request, use Root.Read on the retained
   runtime to preflight the exact scoped unconsumed flow, connection and browser
   binding, and obtain database validation-start instant Q. End that read
   transaction before network work. This preflight is not final authority.
2. Exactly one qualified concrete provider adapter performs its bounded exchange
   and validation outside every SQL transaction. It verifies the actual artifact
   and exact configured provider/connection/issuer/client/redirect plus the
   flow's nonce/PKCE or explicitly qualified provider-specific equivalent.
3. The native adapter retains a private immutable verified result: stable subject,
   exact issuer/provider/connection/configuration identity, nonce/flow binding,
   actual validated artifact validity limit and the verifier's provenance.
   Do not retain raw tokens, authorization codes, email or provider diagnostics
   in writer evidence. A plain exported struct of asserted fields is insufficient.
4. Immediately after successful validation, sample ValidatedAt through Root.Read
   on that same retained database, still before G. Require Q <= ValidatedAt,
   ValidatedAt < Q+60 seconds and ValidatedAt < ProviderValidUntil. Retain the
   original Q; ReceiptUntil is exactly Q+60 seconds as required by existing
   `writerproof.FlowCallback`. A reconnect, G wait or retried callback cannot
   refresh either instant. No automatic second exchange is authorized.
5. Writer admission revalidates all local facts under the acquired plan. Final
   validity is strict at the original root F against the provider proof bound,
   ReceiptUntil, persisted flow expiry, original link bounds where applicable,
   and the original root deadline. A non-future observation alone does not prove
   provider validity through F.

The 60-second receipt ceiling is a **local nonrefreshable bound**, not the
provider artifact's expiry. The original request deadline can be shorter and
always wins. Preserve the existing bounded callback lifetime; do not give the
exchange a fresh request after preflight or a new budget after G. Root retains
its original five-second total budget, including checkout/G waits, honoring the
earlier parent deadline. Application monotonic time may enforce cancellation;
only the same-database instants control proof freshness.

The existing source cannot populate the receipt correctly. The following are
mandatory native adapter deliverables, not assumptions that they already exist:

| Adapter | Required qualified proof semantics | Unavailable until |
| --- | --- | --- |
| Google / selected OIDC adapter | Verified signed identity artifact, exact issuer/audience/client/nonce and the actual artifact expiry, with bounded key/discovery handling | Concrete verifier and its clock-domain/expiry semantics are reviewed and exercised |
| Apple | The actual verified identity artifact and its expiry/bindings; client-authentication material lifetime must not be confused with user proof lifetime | Concrete adapter, rotation and clock/validity contract are qualified |
| Enterprise OIDC | Exact configured connection/issuer/client and validated user proof, including current connection policy | Tenant-specific verifier/configuration and validity semantics are qualified |
| GitHub OAuth | Actual one-use exchange plus authenticated stable-subject verification and supported revocation/validity semantics | A concrete provider-specific validity contract exists; do not invent an OIDC `exp` or set ProviderValidUntil to Q+60 seconds |

For a signed expiry, a naked parsed claim is not sufficient: the native verifier
must authenticate it and qualify comparison with the deployment's DB time.
A skew allowance must not silently extend the signed expiry. Missing expiry,
unsupported artifact, unestablished clock relationship or revocation semantics
that cannot support this receipt fails unavailable. A native adapter unable to
supply the existing finite ProviderValidUntil requires an explicit shared-contract
amendment; T3.10 cannot change `writerproof.ProviderCheck` to hide that absence.
No live provider access or production qualification follows from signed fixtures.

A future exact provider-receipt API belongs to the provider/verifier owner and
must be reviewed with its concrete verifier. This design intentionally does not
publish `NewVerifiedIdentity(identity, expires)` or an injectable
`func(...) ProviderCheck`: either would simply move the missing trust decision
into a caller assertion. Federation converts only the qualified native receipt
into the existing `wp.ProviderCheck` inside its private concrete callback path.
Until that API exists, callback implementation/admission is held; the legacy
Identity must not be silently accepted for W1.

## 3. Persist original flow and link evidence

Use the existing flow owner and validation helpers; do not create a second cookie
parser, identity store or flow model outside federation. Both begin paths retain
existing state/browser/nonce digests, PKCE S256, safe return-target checks and
exact provider/connection/issuer/realm/intent binding. A W1 insert explicitly sets
`created_at = B` and `expires_at = B + 10 minutes`, where B is Attempt.StartedAt after
G. The existing created_at default is transaction time and cannot be substituted
after a G wait. This uses existing columns; it does not alter migration 16.

There are two additional durable-evidence gaps:

- A link flow currently stores person, authentication time, security epoch,
  assurance level/expiry and session digest, but **not the original session idle
  and absolute expiry**. The writer contract requires those begin-time bounds
  to survive until CallbackLink. A session renewal between requests must not
  extend them. A fresh ActorForWriter result at callback cannot recover a lost
  original bound.
- `wp.FlowCheck` requires callback URL, code challenge/method and authorization URL.
  Some can be recomputed from state/browser material, but the original native
  configuration identity and exact authorization preparation are not persisted.
  Equal provider/issuer strings alone cannot prove an unchanged client/redirect
  or provider configuration after restart/reconfiguration.

Propose an **additive integrator-owned flow receipt** with original link idle and
absolute expiry, immutable native configuration revision, and a bounded digest
of the exact prepared authorization binding. Exact schema/API review must fix
names, nullability and canonical bytes before source; no migration number or SQL
change is assigned here. The digest covers a versioned, length-delimited encoding
of realm, flow ID, intent, provider/connection/issuer, configuration revision,
callback URL, safe return target, PKCE method/challenge and exact authorization
URL. State/browser/nonce digests remain independently compared. Raw cookies,
verifiers, provider tokens and codes are never new persisted fields.

Login rows have absent link-only fields; link rows require consistent original
bounds. Callback reconstructs the locally prepared binding only through the same
immutable native adapter revision and verifies its stored digest before exchange,
then again under G against the locked flow. Unsupported revision, missing receipt
or changed configuration is unavailable; do not guess values for old rows. The
integrator must decide the explicit no-mixed-replica/old-flow transition alongside
the additive migration. Published migration 16 stays immutable.

## 4. Native action plans and staging

All four actions use one Root.Run per mutation with G then B, complete discovery
before SealPlan, and ordered P/C/H/S/W/D/F, including empty phases. Every existing
row is scoped through the fixed Root dispatcher; connection rows use its permitted
ExistingShare, other existing writer rows ExistingUpdate. Generate reserved flow,
binding and session IDs before sealing. Include the complete cross-person union
for a prior login cookie; no late-discovered person is locked after C/H/S.
Every inline mutation calls RecordMutation before SQL with the exact planned rows.
No nested root, detached issuance, arbitrary callback or partial transaction retry.

| Action | Complete plan and native evidence | Staged transition |
| --- | --- | --- |
| BeginLogin | C exact connection SHARE; H reserved flow; P/S empty; `wp.FlowBegin(FederationBeginLogin, ..., nil)` | One FlowWrite of the exact unconsumed login flow; no provider exchange, binding or session |
| BeginLink | Original same-session request admission; DiscoverPrior non-issuing; P exact actor, C connection SHARE, H reserved flow, S actor; current policy W where required; `wp.FlowBegin(FederationBeginLink, ..., &actor)` | One FlowWrite binding original actor and all original validity bounds; no binding or new session |
| CallbackLogin | Outside-G native receipt; G discovers exact flow/connection/existing subject binding and active bound person, unions session.DiscoverPrior for FederationCallbackLogin; P all persons, C connection/binding, H existing flow, S prior/reserved sessions | `wp.FlowCallback`, `wp.ForIssue`, session.StageWriter; exact once-only flow consumption; no account creation/email merge |
| CallbackLink | Outside-G receipt; exact original admitted actor and persisted begin binding; P actor, C connection and reserved external binding, H existing flow, S original actor; W native policy | `wp.FlowCallback` with actor; store.NewWriter.LinkExternalIdentity records BindingWrite; exact flow consumption; no issuance |

Use `session.AdmitWriterRequest` on the actual bounded HTTP request for relevant
session participation. It preserves private proof when native middleware admitted
that request and binds the exact session service. BeginLink must retain POST,
origin and session-bound CSRF validation; admission alone is not a CSRF grant.
CallbackLink's GET uses the one-use browser/state/flow binding and must match the
original session proof, not a fabricated request or principal. Exact callback
middleware/wiring is an integration gate; a legacy route that supplies only a
cookie digest cannot be claimed to provide private same-instance admission.

After complete P/C/H/S acquisition, `ActorForWriter` supplies current actor facts
for links. It must match persisted person/session digest/epoch/authentication time
and not exceed the original begin assurance or newly persisted idle/absolute
bounds. A fresh sign-in cannot replace the actor of an existing link flow.
Do not use `AdmitWriterContext` with invented actions: its current API permits
only MFA begin and password change. The real request path is available; any new
context-only federation action would need a separate session-owner contract.

Native final current-actor comparison remains session-owned. `ActorForWriter`
samples a new clock and cannot be called after F. The current session package
has `CheckStagedWriter` for newly staged issuance, but no general public final
actor method for federation's non-issuing link actions. A separately reviewed
session-owned final actor seam must reuse its private row decoding/current
admission checks, retained transaction and exact F without a new lock or clock.
Do not duplicate session SQL or expose a raw-session selector to federation.

For CallbackLogin, resolve only the exact existing connection/issuer/subject
binding to a current active local person. Never use verified email equality,
provider username, an owner configuration value or a request person ID. For
CallbackLink, use the existing writer-aware identity store for the insert and
its uniqueness semantics; do not call legacy insertBinding's joined `FOR UPDATE
OF p,s` or duplicate its SQL. Connection state, binding identity and original
flow predicates remain required after lock waits.

## 5. Final F and publication

All staged flow/binding/session work, its uniqueness/FK waits, native policy work
and the declared constraint drain precede F. Take `Attempt.DrainAndSample` once. At F, plain native final
SELECTs on the original retained transaction are allowed; new locks, mutations,
clock samples, provider calls and application callbacks are not. Compare:

- Begin actions: exact inserted **unconsumed** flow, B/expiry, prepared authorization
  binding, enabled unchanged connection and original actor bounds for links.
- Callback actions: exact intended **consumed** flow transition once by this attempt,
  all original browser/state/nonce/realm/intent/binding predicates, unchanged
  enabled connection, current person/epoch and exact external binding. Keep every
  final-flow condition previously enforced by the legacy consumption UPDATE as
  a final plain-read predicate against F and the intended transition.
- CallbackLogin: exact new and intentionally revoked prior session states through
  `session.CheckStagedWriter`; do not demand the intentionally revoked prior row
  remain unrevoked. Link: unchanged original actor/session identity and all
  original/fresh validity minima; no session is created or renewed by linking.
- All callbacks: Q/ValidatedAt chronology and strict F before ProviderValidUntil,
  ReceiptUntil and original flow/link bounds. Equality fails; no refresh at F.

Then use existing `wp.Finalize`, `Attempt.Finish` and Root completion. Issuing
callbacks publish only through `session.PublishWriter` with the same staged
session and matching existing `wp.Permit`. Begin/link non-issuing output consumes
exactly one `Completion.TakeRelease` for its captured binding and requires the
same permit's `MatchesRelease` and Success. No new credential permit or invented
FinalizeWriter replaces these checks. A URL/browser cookie/redirect is provisional
until successful commit and matching terminal release. Failure, cancellation or
failed/unknown commit releases none and triggers no automatic exchange retry.

Freshness is measured at F conditional on successful commit; it is not a claim
about future network arrival. SQL errors/malformed or missing native dependencies
are unavailable; established expired/consumed/mismatched flow or invalid current
authority is rejected through the existing non-enumerating public mapping.
Never return success merely because a failed transaction's callback returned a
valid-looking Authorization value or provider Identity.

## 6. Source admission and finite verification plan

No federation implementation follows until different early review and an explicit
file assignment. The next integration decisions are concrete:

1. Provider owners supply a reviewed native verified receipt API and at least one
   actual supported adapter's original validity semantics; an extensible legacy
   Exchange result cannot satisfy it. Other selected providers stay visible as
   unavailable and in full product scope.
2. Integrator adopts exact durable original-flow/link evidence and additive schema,
   preserved migration history, immutable configuration identity and old-flow
   transition. T3.10 does not own schema/root/session/primaryproof changes here.
3. Session owner adopts the finite non-issuing final actor check; integration proves
   same RuntimeDB/session/request custody and exact original-context callback wiring.
4. Federation receives explicit native source paths for copied configuration,
   complete discovery/plans, flow participants, final comparisons and publication.
   Any begin-only prerequisite slice is identified as such; it cannot qualify
   callbacks, full federation or the complete native graph.

Required subsequent real-service tests use actual Root/session admission and
observed lock/unique/FK barriers: wrong browser/provider/issuer/intent; duplicate
cookies; subject/email collision; binding/connection/person change before G;
original link expiry after renewal; changed original configuration; consumed-flow
replay; expired/equality provider and local receipt bounds after waits; cross-person
old-cookie ordering; intended consumed/revoked state at F; rollback and failed
commit with no cookie/redirect; once-only release. Required-service absence fails
visibly. Synthetic signed/provider fixtures remain source/local integration
qualification, not live provider evidence. Pure tests cannot forge a principal or
selection and label it native admission.

Genuine negatives must target these approved flow/receipt predicates, not the
separately blocked native assurance regression or an equivalent implementation.
That regression remains blocked; no containing full-native-suite execution is
prescribed. No runtime/configuration/credential/provider access, worker launch,
cutover, schema mutation or acceptance/count change is authorized by this design.
