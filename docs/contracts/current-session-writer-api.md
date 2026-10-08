# W1 package and capability API proposal

Status: proposed alongside [W1](current-session-writers.md), not adopted or
implemented. These are declarations for a finite source assignment, not changes
to the frozen API. Module prefix below is `github.com/ajent-social/amos`.

## R1: acyclic ownership and construction

| Package | Imports within this graph | Owns / may construct |
| --- | --- | --- |
| `internal/authoritywriter` (`aw`) | `storage`; no identity, workspace, session or method package | Ordering-only Root, Plan, Attempt, Completion; transaction lifetime and fixed SQL lock dispatcher. Never credential evidence or principal. |
| `identity/internal/writerproof` (`wp`) | `internal/authoritywriter`, `identity/internal/authproof`; no `identity`, store, session or native service | Concrete private evidence variants, method factories, provisional Issuance and final Permit. Accessible to all native identity siblings; Go internal rules exclude workspace and business packages. |
| `identity/store` | `aw`, `wp`; existing store dependencies | Writer-aware Store and pending-registration value; issuance Store consumes wp.Issuance. No credential factory. |
| `identity/session` | `aw`, `wp`, `identity/store`, `identity`, `authproof` | Private request admission, old-cookie discovery, staged session, cookie publication, renewal/revoke roots. Never imports login/recovery/MFA/federation. |
| `identity/login`, `identity/email`, `identity/recovery`, `identity/magiclink`, `identity/mfa`, `identity/federation`, `identity/primaryproof` | `aw`, `wp`, session/store as needed; existing password/policy/delivery dependencies | Exact method discovery, validation, stage and final comparison functions. No registration of arbitrary producers. |
| `workspace/store`, `workspace/personal` | `aw`, existing identity/store public types as needed; never `wp` | Non-credential workspace participants and standalone workspace roots. Bootstrap accepts scoped pending registration, never password/federation evidence. |
| `internal/runtime` | Concrete native services, workspace participants, `aw`, storage | Sole W1 composition factory; owns runtime pointer and root, fixed native adapters, process profile and shutdown. No import from these lower packages back to runtime. |

`identity` continues importing only its existing internal authproof bridge. In
particular `aw` cannot import `identity/internal/writerproof`; the Go internal
boundary forbids it. No sealed interface is implemented by a sibling package:
all cross-package values below are concrete structs with private fields. Only
`wp` builds evidence; session consumes it. Repo-internal ordering is not identity
evidence, even when business code is in the same repository.

The runtime constructor passes an `*aw.Root` to concrete `NewWithWriter` native
constructors, retains the resulting services privately and exposes handlers.
Those constructors retain copied method configuration and the exact session
service where needed. They accept no producer, discovery, finalizer, credential
factory, arbitrary TxRunner or method-registration function supplied by a
business extension. Provider adapters remain a separately qualified finite
native dependency; their output does not bypass method factories.

## Ordering declarations and immutable plan

The `aw` API uses `context`, `database/sql`, `time`, `storage`, and `google/uuid`.
The following declarations have no credential meaning:

```go
type Realm struct { Installation, Application, Environment uuid.UUID }
type Table uint8
const (
    Persons Table = iota + 1
    Emails; Credentials; Connections; Bindings
    Challenges; Factors; Flows; Sessions; Workspaces; Memberships
)
type Phase uint8
const ( P Phase = iota + 1; C; H; S; W; D; F )
type Access uint8
const ( ExistingUpdate Access = iota + 1; ExistingShare; ReservedInsert )
type Row struct { Table Table; ID uuid.UUID; Access Access }
type Delivery struct { MaterialID uuid.UUID; JobKey string }
type Plan struct { data *planData } // private copied, sorted rows and D keys
func NewPlan(realm Realm, rows []Row, delivery []Delivery) (Plan, error)

type Root struct { state *rootState }
type Attempt struct { state *attemptState }
type Completion struct { state *completionState }
type Release struct { data *releaseData } // terminal committed output only
type Outcome uint8
const (
    Success Outcome = iota + 1
    DeniedRollback
    CounterOnlyDenied
    UnavailableRollback
)
var ErrUnavailable, ErrDenied, ErrUnrooted, ErrPhase, ErrClosed error
func New(runtime *storage.RuntimeDB) (*Root, error)
func ActivateW1(root *Root) error
func SelectLegacy() error
func (r *Root) Read(ctx context.Context,
    body func(context.Context, *sql.Tx) error) error
func (r *Root) Run(ctx context.Context,
    body func(context.Context, *Attempt) Outcome) (Completion, error)
func (a *Attempt) DiscoveryTx(ctx context.Context) (*sql.Tx, error)
func (a *Attempt) SealPlan(plan Plan) error
func (a *Attempt) Binding() (Binding, error)
type Binding struct { state *bindingState }
func (b Binding) Matches(a *Attempt) bool
func (a *Attempt) Acquire(ctx context.Context, phase Phase) error
func (a *Attempt) ParticipantTx(ctx context.Context, phase Phase,
    rows []Row) (*sql.Tx, error)
type DeliveryStep uint8
const ( MaterialInsert DeliveryStep = iota + 1; JobEnqueue )
func (a *Attempt) DeliveryTx(ctx context.Context, delivery Delivery,
    step DeliveryStep) (*sql.Tx, error)
func (a *Attempt) DrainAndSample(ctx context.Context) (time.Time, error)
func (a *Attempt) RestrictCounter(factorID uuid.UUID) error
func (a *Attempt) RecordMutation(kind Mutation, rows []Row) error
func (a *Attempt) Finish(outcome Outcome) error
type Mutation uint8
const (
    RegistrationWrite Mutation = iota + 1; CredentialWrite; ContactWrite
    ChallengeWrite; FactorSuccessWrite; CounterWrite; FlowWrite; BindingWrite
    SessionWrite; WorkspaceWrite; DeliveryWrite
)
func (c Completion) Outcome() (Outcome, error)
func (c Completion) Matches(a *Attempt) bool
func (c Completion) TakeRelease(binding Binding) (Release, error)
func (r Release) Matches(binding Binding) bool
func (r Release) Outcome() (Outcome, error)
```

`Root.Run` is the sole writer-transaction owner of `runtime.WithTx`, explicit writable READ COMMITTED,
G acquisition and commit. Its callback is **ordering plumbing in trusted native
source**, not an injectable credential producer. It accepts/returns no evidence,
principal, `any`, map, string discriminator or extensible payload. No public host
configuration forwards this callback. Native services close over typed local
results; they expose them only after a matching successful Completion. The
callback receives the derived context; the attempt stores its cancellation
function/deadline and request identity, not a reusable context for later calls.
Run derives a context with an aw-private key/token for this attempt, preserving
original middleware/budget context values. Participant methods require that
same private token and an unexpired root deadline; derived shorter contexts are
allowed, background/replaced contexts are not. There is no exported marker setter;
this token is ordering provenance, never credential admission.

Root.Read uses the same runtime and bounded context for non-mutating reader
transactions, with explicit writable READ COMMITTED where row SHARE is needed.
It takes no G and cannot return an Attempt; no reader-to-writer promotion. Native
preview/clock sampling and MFA Status use it; its raw tx is private trusted
plumbing, not a business capability or escape hatch for legacy mutators.

New validates non-nil exact runtime ownership. ActivateW1 freezes one root per
process before any service/store construction (see composition). NewPlan copies
all slices/strings, validates nonzero canonical IDs, supported table/access
pairs and duplicates, and sorts by phase/table/UUID. This finite native profile
allows zero or one Delivery per plan; every current request queues at most one
challenge delivery. Multiple deliveries require a separately ordered extension. `ExistingShare` is allowed
only for Connections; all other existing writer rows use UPDATE. New IDs are
server-generated before sealing; ReservedInsert means an exact absent ID, not
permission to add arbitrary rows. Relation scope predicates are fixed by the
SQL dispatcher (direct realm columns or scoped parent join). Session revocation
plans include every environment of the person, while issue/old-cookie lookups
remain environment-scoped. No SQL text or table name comes from Row.

G precedes DiscoveryTx; this handle is for nonlocking SELECT only in reviewed
native source. SealPlan is once-only before P; no appending after P. Acquire
performs separate fixed scoped row-lock statements in P,C,H,S,W order, including
empty phases; reserved rows are inserted by their native participant at their
phase, never by the lock dispatcher. New parent insertion precedes child
insertion. Existing locks are all acquired before method staging that needs a
later phase; stage may mutate an already-held/reserved earlier row without
acquiring a new lock. Binding is a private-state comparable-by-method lifetime token shared with
identity evidence, never a credential. Binding.Matches/Completion.Matches compare
immutable nonzero identity even after closure, so post-commit publication can
match the finished attempt; they do not report liveness. All SQL/evidence-use
methods independently require live state; a post-commit match cannot reopen it.
It lets wp validate attempt identity
without reading aw private fields. ParticipantTx checks exact live attempt/root/tx, active
profile, completed acquisition through the requested phase and every row against
the sealed plan **before SQL**. It cannot admit a new row or upgrade a reader.
The phase denotes the minimum acquisition prerequisite, not permission to
acquire an earlier lock after advancing. D permits only sealed material/job keys
in material-then-job order through DeliveryTx. MaterialInsert and JobEnqueue
are each single-use, bind exact sealed MaterialID/JobKey, and require W acquisition
complete. DeliveryWrite in RecordMutation uses this current D authorization
rather than an authority-table Row. No other D participant is admitted by this
revision; adding audit/business participants requires a separately frozen order.

A native compound operation (registration, reset, MFA) performs its ordered
substeps with one attempt, not nested Roots. No retry, savepoint, goroutine,
retained transaction or transaction replacement is allowed. Row identity changes,
unplanned rows, duplicate execution or mode failure poison the attempt. Zero
Root/Plan/Attempt/Completion rejects before SQL. Pointer copies share the same
private lifetime state; copying a struct does not clone admission.

DrainAndSample runs the fixed constraint drain, advances to F and samples DB time
once after waits. It records and returns an instant, not identity evidence. Add
`func (a *Attempt) IsFinalSample(t time.Time) bool`: wp.Finalize requires this
exact recorded F instant and rejects a caller-selected replacement.
Finish is once only. Success/CounterOnlyDenied require root-recorded F and
valid phase/journal state. Calling the native finalizer first is a **trusted
native caller obligation**, not a check aw can infer: aw does not import wp or
inspect identity evidence. Root Finish alone is never authentication proof.
DeniedRollback/UnavailableRollback can finish at any earlier phase, do not
require a completed plan or F, and force rollback. This permits ordinary wrong
password/no-current-proof failures without misclassifying them as missing-finalizer
bugs. A native private reason enum retains its route mapping; only the closed
Outcome selects transaction behavior. It forbids
further participant SQL; F reads by the finalizer use the already obtained tx
and may neither lock nor mutate. RestrictCounter is once-only after acquisition and before any RecordMutation;
it binds the sole planned factor. RecordMutation is called before each mutation
by every native inline/store participant. In counter mode only CounterWrite on
that factor is admitted once; all other kinds fail/poison before SQL. Outside
counter mode CounterWrite is refused. Finish(CounterOnlyDenied) requires exactly
that recorded mutation. SQL row-count/full-row checks are still the MFA adapter's
obligation; a journal is not a sandbox against arbitrary raw SQL. A failed
mutation invalidates the root, so the journal cannot be used to bless a later
partial commit. Run requires body outcome == finished outcome;
missing Finish/unknown enum/panic/cancellation/error yields rollback/unavailable.
Success and CounterOnlyDenied alone return nil from the storage callback. The
latter has the separate mutation restrictions in W1; it is not success proof.
Run invalidates all attempt handles on every exit, including failed commit.
Completion is created only after storage reports commit success and can release
one matching native result. A completion for another attempt/service cannot.

### Postcommit release state (R1 lifecycle clarification)

Binding carries an immutable opaque identity token separate from the mutable
attempt state. Completion stores that token, committed outcome and a private
atomic unused/used output bit; it does not retain a live SQL capability.
TakeRelease rejects zero/mismatched binding, noncommitted state, unknown outcome
or an already-used bit, then atomically consumes the bit and returns Release
with that same token/outcome. No Release can be created before successful commit.
Release has no transaction, context, phase, acquisition or evidence-use method.
Its Matches/Outcome getters inspect terminal state only and cannot reopen Run.
Failure/unknown commit never creates Completion or Release.

wp.Finalize creates a Permit containing a separate immutable finalization
snapshot: captured Binding, closed action/outcome, exact F and expected staged
issuance identity where applicable. This terminal snapshot survives root closure
solely for publication matching; Evidence/Issuance live-use rights still expire
on every root exit. Permit has no credential getter or SQL-authorizing method.
The root cannot certify the native predicates and does not claim to do so.
Native finalizers remain responsible for exact read-only row comparisons before
wp.Finalize; session publication requires this wp-issued snapshot in addition
to the root's committed release. Thus root F/Finish cannot alone publish a
cookie even if trusted native code mistakenly omits wp.Finalize.

PublishWriter first validates service/staged/Permit identity, consumes exactly
one Completion.TakeRelease(staged binding), and requires
Permit.MatchesRelease(release) with Success before returning buffered Issued.
The wp finalization snapshot holds the staged Issuance identity copied at F;
publication does not call the now-invalid Issuance.Credential or ParticipantTx.
For seed/URL/ack/counter output, the respective native adapter uses the identical
TakeRelease + Permit.MatchesRelease protocol on its typed buffered result;
CounterOnlyDenied's release matches only its counter Permit and returns no
success payload. Unknown-address/rate-cap no-row acknowledgements use the action's ordinary
generic mapping after the root has closed with rollback and no staged writes;
they do not need or fabricate credential Permit/issuance and cannot assert that
mail was queued. Positive staged delivery acknowledgements use the release
protocol. SQL/completion failure is never relabeled as the generic no-op branch.
A wrong binding/permit consumes no valid output, and any failure releases none.
Replay of PublishWriter or release extraction fails the single atomic bit.
Copying already published public bytes cannot be prevented by a capability API;
no claim about such application copying is made.

No raw transaction check attests its database; private composition establishes it.

## Identity evidence declarations and factory ownership

`wp` imports only `aw`, `authproof`, UUID/time and standard helpers. Its snapshot
inputs are values used by trusted identity code; they are not public host input.
Each factory validates the live sealed attempt, relevant acquired rows, complete
binding/chronology and action. Evidence and Issuance fields are private, including
a shared live-use invalidation token and exact root/attempt/action identity.
Permit fields are separately private terminal finalization snapshots as above;
Session WriterRequest/Staged bind the concrete session service separately; wp
never imports session or pretends to inspect its private fields.

```go
type Action uint8
const (
    Register Action = iota + 1; PasswordSignIn; EmailRequest; EmailConfirm
    ResetRequest; ResetComplete; PasswordChange; MagicRequest; MagicConfirm
    MFABegin; MFAConfirm; MFAChallenge
    FederationBeginLogin; FederationBeginLink
    FederationCallbackLogin; FederationCallbackLink
    Renew; Signout; Revoke; Assurance
)
type Subject struct { Person uuid.UUID; Realm aw.Realm; Epoch int64 }
type Contact struct {
    ID uuid.UUID; ComparisonKey string; VerifiedAt time.Time
}
type PasswordCheck struct {
    Subject Subject; Contact Contact; CredentialID uuid.UUID
    VerifiedHash, StagedHash string
    VerifiedAt, ValidUntil time.Time
}
type ChallengeCheck struct {
    Subject Subject; Contact Contact; ID uuid.UUID
    TokenDigest, BrowserDigest [32]byte
    CreatedAt, ExpiresAt, VerifiedAt time.Time
    DifferentDeviceConfirmed bool
}
type ActorCheck struct {
    Subject Subject; SessionID uuid.UUID; Digest [32]byte
    Method string; AuthenticatedAt, IdleUntil, AbsoluteUntil time.Time
    Assurance string; AssuranceUntil time.Time
}
type FactorCheck struct {
    ID uuid.UUID; State string; Epoch, LastStep, AcceptedStep int64
    CiphertextDigest [32]byte; FailedAttempts int
    LockedUntil, PendingUntil, VerifiedAt, WindowUntil time.Time
}
type FlowCheck struct {
    ID, ConnectionID uuid.UUID; Realm aw.Realm
    Provider, Issuer, Subject, ReturnTo string
    CallbackURL, CodeChallenge, CodeChallengeMethod, AuthorizationURL string
    StateDigest, BrowserDigest, NonceDigest [32]byte
    CreatedAt, ExpiresAt time.Time
}
type ProviderCheck struct {
    ConnectionID uuid.UUID; Provider, Issuer, Subject string
    NonceDigest [32]byte
    ValidationStartedAt, ValidatedAt, ProviderValidUntil, ReceiptUntil time.Time
}
type Evidence struct { data *evidenceData }
type Issuance struct { data *issuanceData }
type Permit struct { data *finalizationData }
type CounterReason uint8
const ( BadCode CounterReason = iota + 1; Replay )
type CounterTransition struct {
    FactorID uuid.UUID; Reason CounterReason
    BeforeAttempts, AfterAttempts int
    BeforeLockedUntil, AfterLockedUntil, UpdatedAt time.Time
}
func Password(a *aw.Attempt, action Action, check PasswordCheck) (Evidence, error)
func Challenge(a *aw.Attempt, action Action, check ChallengeCheck) (Evidence, error)
func Enrollment(a *aw.Attempt, actor ActorCheck, primary Evidence,
    factor FactorCheck) (Evidence, error)
func TOTP(a *aw.Attempt, action Action, actor ActorCheck, primary Evidence,
    factor FactorCheck) (Evidence, error)
func FlowBegin(a *aw.Attempt, action Action, flow FlowCheck,
    actor *ActorCheck) (Evidence, error)
func FlowCallback(a *aw.Attempt, action Action, flow FlowCheck,
    provider ProviderCheck, subject Subject, actor *ActorCheck) (Evidence, error)
func Actor(a *aw.Attempt, action Action, check ActorCheck) (Evidence, error)
func ForIssue(a *aw.Attempt, evidence Evidence) (Issuance, error)
func Counter(a *aw.Attempt, actor ActorCheck, primary Evidence,
    factor FactorCheck, transition CounterTransition) (Evidence, error)
func Finalize(a *aw.Attempt, evidence Evidence, finalDBTime time.Time) (Permit, error)
func (p Permit) Outcome() (aw.Outcome, error)
func (p Permit) Matches(a *aw.Attempt, issuance Issuance) bool
func (p Permit) MatchesRelease(release aw.Release) bool
func (i Issuance) Credential(a *aw.Attempt) (authproof.VerifiedCredential, error)
```

Password is constructed only by login or primaryproof **after the one Verify**
under C; action is restricted to PasswordSignIn, PasswordChange, MFABegin,
MFAConfirm or MFAChallenge. It binds action as well as attempt; a primary-only
MFA/change Password value is not a sign-in issuance. Challenge is called by login/email/recovery/magic native adapters and
admits only their closed action values; an unverified registration contact has
zero VerifiedAt and is not an issuance action. Recovery change combines Actor
and Password evidence inside its private method result, finalizes both and
checks exact intended hash/epoch/revocations. Enrollment/TOTP copy and bind the
same-attempt primary evidence. No reference to a mutable input Actor pointer is
retained. Nil actor is legal only for FederationBeginLogin/CallbackLogin.
Federation native code alone calls FlowBegin/FlowCallback after its respective
local URL or provider validation contract. Actor handles renewal/signout/revoke/
assurance; it does not manufacture new credential authority.

`wp.Finalize` verifies token, the exact root-recorded F instant, original bounds
and action/transition shape. For request/begin actions it returns a Success
permit with **no credential**; completion is still needed for disclosure.
The zero time for optional LockedUntil/PendingUntil/actor fields means absent
only in the documented action/state; unknown enum/state or malformed nullable
shape fails unavailable. All required instants must be nonzero and chronological;
F cannot precede B, verification or any retained transition instant. Each
native adapter first rereads held rows at F and compares **every** final predicate
in the action table; only then calls Finalize. This is a trusted implementation
obligation, not evidence that the ordering package authenticated those facts.
ForIssue accepts only password sign-in, magic confirmation, successful TOTP or
federation callback-login evidence; it rejects requests, begin, link, counter,
legacy VerifiedCredential and every non-issuing action. It produces provisional staging authority,
not permission to publish. Final Permit is required for publication after commit.

`Issuance.Credential` performs the wp-owned translation to the existing
`authproof.VerifiedCredential` only for the exact live acquired attempt and
allowed issuance action, once; session StageWriter calls it. This constrained
identity-internal getter is not an arbitrary credential constructor or business
factory. Permit.Matches binds final evidence to the same staged issuance using captured
immutable identities, including after closure; it does not require or restore
live evidence. MatchesRelease additionally checks the terminal committed binding
and outcome. Only a native factory/finalizer can create a nonzero Permit. Native method strings map through the fixed
Action variant, not caller-selected strings. Subject/contact hashes stay private;
plaintext passwords, seed material and raw cookies are never evidence fields.
No evidence can be serialized, placed in public context or reused across attempts.

## Concrete session and participant seam

Proposed additions (each signature belongs to the named existing package):

```go
// identity/session
func NewWithWriter(root *aw.Root, cfg Config) (*Service, error)
type WriterRequest struct { data *writerRequestData }
type Staged struct { data *stagedData }
func (s *Service) AdmitWriterRequest(r *http.Request) (WriterRequest, error)
func (s *Service) AdmitWriterContext(ctx context.Context,
    action wp.Action) (WriterRequest, error)
func (s *Service) DiscoverPrior(ctx context.Context, a *aw.Attempt,
    request WriterRequest, action wp.Action) ([]aw.Row, error)
func (s *Service) ActorForWriter(ctx context.Context, a *aw.Attempt,
    request WriterRequest) (wp.ActorCheck, error)
func (s *Service) StageWriter(ctx context.Context, a *aw.Attempt,
    proof wp.Issuance, request WriterRequest) (Staged, error)
func (s *Service) PublishWriter(c aw.Completion, p wp.Permit,
    staged Staged) (Issued, error)

// identity/store and workspace/store (each owns its own Store)
func NewWriter(a *aw.Attempt) (*Store, error)
// identity/store only: issuance/elevation methods require this constructor.
func NewIssuer(a *aw.Attempt, proof wp.Issuance) (*Store, error)
// identity/mfa
func NewWriterStore(a *aw.Attempt) (*Store, error)
// identity/mfa read-only Status participant: no mutation methods on ReadStore.
type ReadStore struct { tx *sql.Tx }
func NewReadStore(tx *sql.Tx) (*ReadStore, error)
func (s *ReadStore) Find(ctx context.Context, scope Scope,
    id uuid.UUID) (Factor, error)
func (s *ReadStore) FindCurrent(ctx context.Context, scope Scope,
    state FactorState) (Factor, error)
// workspace/personal
func NewWriter(a *aw.Attempt) (*Participant, error)
// identity/store, additive binding used by workspace/personal:
func (r PendingRegistration) InAttempt(a *aw.Attempt) bool
```

Session Config is already DB-free; NewWithWriter takes no other runtime source. AdmitWriterRequest copies required cookie/origin/CSRF fields, binds
this service/request and rejects duplicate configured cookies. DiscoverPrior
uses only G discovery SQL, returning a copy of exact old-session and owner Rows
and, for an allowed issuing action, its reserved new-session Row. The session
adapter generates that new ID and token before sealing and retains them privately
in WriterRequest; StageWriter cannot generate an unplanned session ID;
the caller unions these before SealPlan. Malformed/missing/foreign old cookies
retain scoped no-foreign-mutation behavior. StageWriter validates the same
service/attempt/request/evidence and planned old/new rows, uses S capability and
buffers cookie/CSRF. It cannot open or finish a root. PublishWriter uses the explicit terminal-release protocol above with a
matching Success Completion, final Permit and Staged value once. The permit's
finalized state remains inspectable for publication but cannot authorize SQL
once its attempt is closed. Counter completion has no Staged value or cookie.

For context-only non-issuing actions, AdmitWriterContext accepts only MFABegin
or PasswordChange and extracts the exact service's private middleware proof from
the original context. It checks the full admitted principal, cancellation and
original request lifetime, and copies the admitted session identity internally.
No public principal alone, context setter or synthetic HTTP request is accepted.
The resulting WriterRequest has a distinct context-only mode bound to that action;
DiscoverPrior accepts only the matching action and discovers the original session
and complete owner/person rows under G, before sealing P/S. It reserves no new
session and does not parse an old cookie. ActorForWriter reads the exact admitted
session under the acquired plan and validates its current state against the
original private admission. StageWriter rejects context-only mode before SQL.

MFA BeginEnrollment and recovery.change call AdmitWriterContext on their original
ctx, compare their explicit principal argument to the admitted immutable principal
inside session-owned ActorForWriter validation, union all action rows with
DiscoverPrior, seal/acquire, then consume ActorForWriter. To make that comparison
constructible without exposing admission, add
`func (s *Service) WriterPrincipalMatches(request WriterRequest, p identity.Principal) bool`;
it performs full immutable equality and exact service/request binding, never
creates proof. A false result aborts before domain SQL. Their final action fences
recheck the original actor bounds; password change permits only its intentional
post-transition epoch/revocation differences. Prescribed negatives cover missing,
fabricated, cross-service, stale/canceled and mismatching-principal admission,
wrong action, and an attempted context-only session issuance.

Exact native constructor/caller declarations supplement the session/store ones
above (names live in each specified package; existing Config/TxConfig types keep
their legacy shapes):

```go
// identity/login
func NewWithWriter(root *aw.Root, cfg TxConfig) (*Service, error)
// identity/email
func NewWithWriter(root *aw.Root, outbox *sqlstore.TxWriter,
    renderer *deliveryemail.Renderer, materials *materialstore.Writer,
    cfg Config) (*Service, error)
func (s *Service) QueueExistingChallengeWriter(ctx context.Context,
    a *aw.Attempt, personID, emailID, challengeID, requestID uuid.UUID,
    rawToken string) error
// identity/recovery: session service is needed for original actor provenance.
func NewWithWriter(root *aw.Root, sessions *session.Service,
    outbox *sqlstore.TxWriter, materials *materialstore.Writer,
    cfg TxConfig) (*Service, error)
// identity/mfa
func NewWithWriter(root *aw.Root, cfg TxConfig) (*Service, error)
// identity/magiclink
func NewWithWriter(root *aw.Root, outbox *sqlstore.TxWriter,
    materials *materialstore.Writer, cfg Config) (*Service, error)
// identity/federation
func NewWithWriter(root *aw.Root, cfg Config) (*Service, error)
// identity/primaryproof: the existing New(hasher) remains usable; this method
// replaces VerifyCurrentPassword(ctx, tx, principal, supplied) in W1.
func (v *Verifier) VerifyCurrentPasswordWriter(ctx context.Context,
    a *aw.Attempt, action wp.Action, actor wp.ActorCheck,
    supplied string) (wp.Evidence, error)
```

Magic/federation Config.DB must be nil; their writer constructors retain only
the root for transaction work. MFA Config.Now must be nil in W1; DB clocks govern
it. Legacy QueueExistingChallengeTx and VerifyCurrentPassword reject in W1;
registration and MFA call the explicit capability variants. Other public native
HTTP/service actions retain their shapes and dispatch private W1 implementations
when built with NewWithWriter; caller-selected principal arguments must match
session-owned admission, never establish it. Existing read previews remain
non-authorizing, nonlocking transactions on the same private runtime. MFA Status
uses NewReadStore only inside Root.Read after the session-owned reader recheck;
ReadStore exposes plain Find/FindCurrent, cannot expire/activate/count failures
or mint wp evidence, and cannot be converted to Store. Its returned Factor is
only input to the final actor/policy/status fence, not authority. NewReadStore
is a separate read-only constructor and does not call SelectLegacy. Close of
the lexical transaction invalidates its SQL handle. This explicitly replaces
Status's old NewStore call rather than making a hidden legacy exception.

The runtime composition, not lower packages, binds concrete native dependencies:
`*password.Hasher`, `*primaryproof.Verifier`, `*session.Service`,
`*vault.Vault`, `*materialstore.Writer`, `*sqlstore.TxWriter`,
`*deliveryemail.Renderer`. In particular MFA must not import its child vault
package (vault already imports MFA types); runtime constructs it and passes the
existing SeedVault interface. MFA's W1 primary/session calls use the exact concrete
Verifier/Service after constructor validation, not an external proof producer.
Other non-credential callback interfaces remain construction-manifest obligations.
The existing `apphost.localPasswordPolicy`/`localMFAPolicy` are unexported and
local-profile-only; runtime cannot instantiate them from another package.
A separately owned native policy adapter must expose the same finite predicates
through the approved host construction, with declared W dependencies and final
re-evaluation. Full organization/enterprise policy remains a finite missing
adapter gate, not an invented import or silently personal-only host.

Provider configuration maps remain the existing legacy shape for compatibility;
W1 copies and closes the configured native provider list at construction. These
are not evidence payloads or credential factory callbacks. Each configured
provider must satisfy the concrete validity gate; no business registration of a
producer/finalizer is accepted. The runtime never exposes root/services/config
callbacks to business handlers.

Workspace roots call Root.Run directly with a non-credential plan, current actor
recheck from the private session service and separately adopted workspace policy.
They cannot call wp factories or session StageWriter. Bootstrap inside signup
receives a same-attempt PendingRegistration whose identity store constructor
binds root, reserved person and live attempt (tx pointer equality alone no longer
suffices). Both bootstrap variants use NewWriter; neither commits its caller.

Store.NewWriter refuses CreateSession and SetSessionAssurance regardless of
phase. Only Store.NewIssuer with an exact live same-attempt wp.Issuance enables
those two methods; session StageWriter constructs it and uses a constrained
Credential getter once. NewIssuer validates without consuming that getter.
Existing session revocation/renewal methods do not require an issuance proof.
Workspace can import store for PendingRegistration but cannot name or construct
wp.Issuance; aw alone cannot mint a credential or enable issuer methods.

All writer stores retain Attempt, not just tx. Every mutating/locking method calls
ParticipantTx with its exact row set/required phase, even after constructor
success. Cross-attempt, expired, zero, unplanned, wrong-phase and unrooted calls
return ErrUnrooted/ErrPhase/ErrClosed internally, mapped to the domain's existing
unavailable error with no SQL/output. Semantic stale/missing method authority
maps to denied rollback. Public sentinel strings do not choose commit behavior.
The [finite inventory](current-session-writers.md#r5-finite-entry-coverage)
is the method-to-capability map.

## Legacy and compile-contract checks

In W1, old `New(tx)`, `NewStore(tx)`, `personal.New(tx)`, detached session Issue,
IssueForRequest and IssueForRequestTx refuse before SQL. Passing a real rooted
tx to an old constructor still refuses: migrate to the capability API. Legacy
constructors work only in the separate legacy process profile. A preexisting
legacy object prevents ActivateW1; a process cannot mix the two profiles.

Required future compile checks: native identity siblings construct each listed
factory input and call session staging; workspace imports only aw/public store;
external/business imports of wp fail; a sibling cannot implement an evidence
interface because none exists; static import DAG has no cycle; no constructor
accepts `any`, maps or external producer callback. Runtime negatives additionally
cover zero values, mutable input copies, cross-root/attempt/service, retained
store after completion, duplicate phase/finalize/publication, unknown action,
legacy constructor bypass and CounterOnlyDenied issuance. These checks are
prescribed, not run by this document author.


## B1 bounded password adapter proposal

[The exact computation adapter](current-password-work.md) closes the proposed
R6 implementation direction using the existing hasher and a globally bounded,
context-responsive pure-computation worker. It explicitly distinguishes caller
cancellation from Argon2 CPU termination and grants no authority to late results.
This additive proposal requires independent review before source; the complete
writer, caller ownership and real W28 integration gates remain open.


## B1-R2: pool-free native delivery participants

The W1 email, recovery and magic constructors use the exact pool-free types above.
Recovery TxConfig.Outbox/Materials and magic Config.DB/Outbox/Materials must be nil;
the separate typed arguments replace those legacy slots. Duplicate sources fail
construction. Other copied scalar, renderer and policy fields keep their declared
meaning; magic Sessions is the exact private concrete session service. Login
continues to receive the privately constructed native email service. No arbitrary
runner is synthesized to satisfy an old constructor.

```go
// jobs/sqlstore: reuse existing NewTxWriter(Config) and TxWriter.
func (w *TxWriter) EnqueueWriter(ctx context.Context, a *aw.Attempt,
    delivery aw.Delivery, intent jobs.Intent) (jobs.Job, error)
// delivery/email/materialstore: config is existing DB-free TxConfig.
type Writer struct { data *writerData }
func NewWriter(cfg TxConfig) (*Writer, error)
func (w *Writer) PutVerificationWriter(ctx context.Context, a *aw.Attempt,
    delivery aw.Delivery, ref email.SecretReference,
    material email.PrivateMaterial, expiry time.Time) error
func (w *Writer) PutSignInWriter(ctx context.Context, a *aw.Attempt,
    delivery aw.Delivery, ref email.SecretReference,
    material email.PrivateMaterial, expiry time.Time) error
func (w *Writer) PutPasswordResetWriter(ctx context.Context, a *aw.Attempt,
    delivery aw.Delivery, ref email.SecretReference,
    material email.PrivateMaterial, expiry time.Time) error
// delivery/email: constructs the same validated Request/Intent as EnqueueTx.
func EnqueueWriter(ctx context.Context, a *aw.Attempt, delivery aw.Delivery,
    store *sqlstore.TxWriter, renderer *Renderer,
    installationID, applicationID uuid.UUID, key string,
    req Request, deadline time.Time) (jobs.Job, error)
```

NewWriter performs the existing material-key/origin/config copying and validation
through a shared private helper, retaining only crypto/config state. It accepts no
DB or TxRunner and exposes no Store, Resolve, Prune, background worker or transaction
method. NewTxWriter already constructs a config-only job participant. Its existing
raw-Tx Enqueue method remains a job-only legacy API; W1 native producers use only
EnqueueWriter. Neither constructor selects a legacy authority-writer profile.

Each material method validates its purpose, scoped configuration, exact
material-reference ID and sealed Delivery, then obtains the sole MaterialInsert
permission from Attempt.DeliveryTx. It reuses the existing purpose-bound encryption
and insertion internals and records DeliveryWrite. The job method requires exact
intent key/installation/application and the matching sealed Delivery, obtains the
JobEnqueue permission only after material insertion, reuses the existing enqueue
validation/idempotency/persistence logic, and records its separate DeliveryWrite.
The native action checks material/job original deadlines and intended rows at F.
To validate realm without accepting an external assertion, add
`func (a *Attempt) Realm() (Realm, error)` returning the copied sealed plan realm
only while live; it grants no credential or SQL authority. Material additionally
checks its configured environment against that realm. Unknown/cross-attempt,
wrong-key, wrong-purpose, closed and duplicate D steps poison the attempt before
participant SQL; a failure never leaves a usable later permission.

No standalone mail dispatcher, material resolver/pruner or job scheduler is
constructed by this finite producer graph. Their future composition is separate;
no fake runner, disabled-success implementation or second runtime is supplied.
At most one effect dispatcher remains an independent operational gate, and zero
during quarantine. The supplied fixtures may inspect durable intent but cannot
claim actual delivery. Source ownership for these new delivery/job adapters must
be delegated before edits; this design correction itself grants none.

Required construction checks instantiate every declared exact type from config
plus the one root and trace native arguments. Actual D schedules include both
material and job unique waits, wrong scope/key/purpose, raw/unrooted/retained attempt,
duplicate step, premature JobEnqueue, failed material, rollback, failed commit and
original-deadline expiry at F. A callback cannot replace a typed participant with
an arbitrary TxRunner. These are prescribed checks, not obtained qualification.

## B1 evidence-factory ordering inspection

Additive proposal for independent review before implementation:

```go
func (a *Attempt) CheckRows(phase Phase, rows []Row) error
func (a *Attempt) StartedAt() (time.Time, error)
```

The context-free `wp` factory signatures above need to validate acquisition
without obtaining SQL or fabricating a request context. CheckRows performs the
same live-root, original cancellation/deadline, sealed-plan, minimum acquisition
phase and exact row/access checks as ParticipantTx, returning no transaction.
It grants no credential, acquires nothing, rejects F/closed state, and poisons
invalid live attempts. Native factories call it on their exact subject and method
rows before constructing evidence; it is not a substitute for native row reads
and final comparisons. No context key or marker setter becomes public.

StartedAt returns the immutable database B already required by R6, sampled
immediately after G. Native request/begin adapters use that one instant for new
record deadlines. It requires a live rooted attempt but need not require a sealed
plan; it cannot choose or refresh B. The root itself records B plus the remaining
original monotonic budget, enforces nonbackward F and strict F before that fixed
database deadline, in addition to the original context deadline. Neither getter
creates a final sample, advances a phase or exposes a runtime handle.

Required negatives include zero/unsealed/wrong-phase/unplanned/closed row checks,
canceled original context despite a detached derived context, and B/F backward
or equality-at-root-deadline faults. These are additional construction checks,
not newly accepted product tasks or obtained runtime evidence.

The identity store's NewIssuer also needs the declared non-consuming validation
without access to wp private fields. Proposed exact method:

```go
func (i Issuance) Check(a *aw.Attempt) error
```

Check validates a nonzero issuing variant, the exact live acquired attempt and
captured binding, and its closed action/subject shape. It returns neither a
credential nor SQL and does not consume or reset Credential's once-only bit.
It remains valid after that getter was consumed only while the same attempt is
live; NewIssuer and each of its issuance/elevation methods repeat this check
alongside their exact row/phase checks. It rejects finalized/F/closed evidence,
another attempt and zero values. Terminal Permit matching continues to use its
separate captured immutable snapshot and never calls Check after commit.
This additive construction detail requires independent review before source.

## B1 existing store lookup construction

Additive proposal for independent review:

```go
func (a *Attempt) PlannedRows(table Table) ([]Row, error)
```

Existing store entry points such as FindActiveSession and RevokeSessionScoped
accept a digest rather than a row ID. A writer store cannot reconstruct the
sealed row inventory from those arguments or fabricate an attempt context.
PlannedRows returns a copied, sorted inventory for one supported table from the
live sealed attempt, including reserved access modes. It exposes no transaction,
credential or context marker, acquires nothing, and does not append to the plan.
Original cancellation/deadline/closure and invalid-table failures still apply.

The native store first obtains ParticipantTx using the complete applicable
existing-row inventory and acquired phase. Any digest/owner lookup is a plain,
scoped non-authorizing read; it must compare the returned immutable IDs to that
inventory and call ParticipantTx again for the exact affected rows before any
locking or mutation. Missing expected authority denies; an unplanned returned
row fails the attempt. No joined or unplanned row lock is permitted during the
lookup. Empty inventories cannot obtain SQL and use the action's existing
no-target denial/no-op behavior. Known-ID methods continue using their exact
arguments directly. This preserves existing public entry-point shapes without
an arbitrary SQL getter or a second discovery phase after P.

Session staging must also match its closed WriterRequest action to the private
issuance action without extracting evidence. Proposed additive comparison:

```go
func (i Issuance) CheckAction(a *aw.Attempt, action Action) error
```

It first applies Check's exact live/acquired/binding/issuing-variant rules, then
requires equality with the captured closed action. It returns no action getter,
credential or SQL and never consumes or resets the credential bit. StageWriter
must call it with the action bound by DiscoverPrior before obtaining any session
SQL or consuming Credential. A wrong action fails unavailable even when the
attempt and subject match. Independent review precedes implementation.

The native session store may add `AssuranceLevel string` and
`AssuranceExpires time.Time` to its Session insertion input. Both zero preserves
the existing legacy insertion behavior. A partial pair or unknown level fails
before SQL. W1 StageWriter sets both from the same once-consumed issuance
credential after CheckAction; it must not accept assurance from request input.
The insertion uses one SQL INSERT carrying base session and assurance columns
and one SessionWrite journal entry for the reserved session row. Elevated
staging must not call SetSessionAssurance after insertion. The original
duplicate-journal rejection remains unchanged. W1 insertion rejects a missing
pair; native staging must compare the subject, realm, method, authentication
time and assurance bounds to that same issuance credential when constructing
the input. Legacy callers with both fields zero retain existing behavior;
nonzero fields require validated closed levels and finite bounds. Independent
early review precedes implementation.
