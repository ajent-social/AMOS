# Quarantined intake admission v1

Status: adopted `amos-contract-v1.37` admission design through independently
reviewed PR152. No handler, migration, witness, enrollment, storage adapter
or deployment is implemented by this document. Original T14.4 is not accepted.
This contract resolves the policy questions in
[issue145](https://github.com/ajent-social/AMOS/issues/145), supplements
[upstream configuration](upstream-service.md) and retains the finite
[diagnostic profile](upstream-diagnostics.md). Collection remains disabled until
all named authorities and required-service checks are independently qualified.

## Initial profile and authority

The initial runtime profile is anonymous-only. Both enrolled allowances must be
zero. Any Authorization header, enrollment credential or enrollment selector is
denied; a credential-looking value cannot fall back to anonymous intake. A later
issuer/verifier/current-revocation contract must independently establish stable
principal identity across credential rotation before enrolled intake is admitted.
No caller public key, verified flag, callback or request-selected provider supplies
that authority. This is a conservative runtime restriction of the declarative
configuration, not an alteration of existing accepted T14.13 schema behavior.

Use a separately approved upstream TLS listener. The only route is
`POST /v1/upstream/diagnostics`; no retrieval, deletion, enrollment or publication
route is introduced. Browser CORS is not enabled; no cookies are consumed or set.
Only the actual socket peer supplies anonymous source identity. Parse the socket
address without DNS; reject zone-scoped/invalid addresses and normalize mapped
IPv4 to IPv4. Never use Forwarded, X-Forwarded-For, hostnames or payload identity.
A reverse proxy without a separately qualified peer contract collapses clients
into its socket identity; the implementation must not infer original clients.

The listener has no job/issue/release credentials, diagnostic-selected network
client, command runner or template evaluator. Deployment/ordinary application
credentials cannot acquire operator or witness authority. Normalized reports are
untrusted quarantine data even when their wire baseline is recognized.

## Resource accounting and anonymous continuity

Retain existing ceilings: compressed and decoded bodies8192 bytes each, total
request deadline5seconds, concurrency at most4, global admitted attempts at most64
per rolling24hours, anonymous at most16 globally and1 per source per rolling24hours.
The configured lower limits and shared anonymous/enrolled sum also apply. Enrolled
is disabled, so its unused allocation is not borrowed by anonymous senders.

A network arrival is not a durable attempt. Bound headers/TLS/body-read time,
connections and in-memory requests before admission. Reject overload, unavailable
clock/witness/store, missing required source keys or exhausted identity capacity
without parsing a body or claiming a durable charge. No unbounded waiter queue or
per-rejected-arrival database record exists. Every request that crosses durable
budget admission consumes its anonymous and shared rolling allowance before body
processing, including invalid input, duplicates and later storage failures. It is
never refunded. Exhausted requests are rejected before that admission; reporting
must distinguish these from charged attempts. No promise that every packet or
rejected network arrival was durably counted is made.

Use witness-authorized UTC time, not calendar-day reset counters. An admission at
`t` blocks the same source until strictly after `t+24h`; equality still denies.
Global limits count all admitted attempts whose24hour interval has not strictly
expired. Persist the bounded active window, not an unbounded arrival log. Quota
checks, source mapping and attempt insertion are one serialized fenced mutation.

Anonymous keys are HMAC-SHA256 using upstream-only secret-manager keys, with a
fixed domain separator, key-epoch ID and canonical socket-address bytes. Each epoch
is one UTC day, at most one new epoch per day. The key registry is authoritative
witness state; the sender cannot choose an epoch. Retain at most9 epoch records:
current plus preceding8 UTC epochs. For every admission, derive aliases under
ALL still-required epochs and complete the bounded lookup before deciding that a
source is new. A match resolves to an existing random private ledger principal;
store only the needed current-epoch alias, never the raw address. Multiple matches
to different principals hold intake as corruption rather than merging quotas.

Each principal record, materialized alias row and retained key-epoch record consumes
ONE entry in the SAME anonymous identity-state capacity: configured128 by default,
hard256. References are fields of these charged rows; no uncharged alias/reference
maps or persistent copies are permitted. At most9 aliases are derived transiently
per request. Allocate needed slots transactionally before admission, or deny.
This conservative accounting can deny before the numerical source limit; it must
not silently multiply that limit by9. Receipt/attempt indexes have separately
bounded fixed entries per retained row and cannot create identities for free.

Retire an epoch's KEY only strictly after its UTC end plus168hours plus the5second
request deadline, AND after the witness verifies no retained principal, alias,
receipt, active attempt or pending operation references it. Inclusive boundaries
are retained. A request in the last second of a day can retain a receipt for at
most168hours; its deadline remains covered by this horizon. Nine daily epochs
cover this overlap. The key-retirement margin is not permission to retain a
source alias/receipt beyond its own applicable24hour/168hour deadline. Destructive
erasure removes expired source references even while an opaque fence stays held. If delayed/uncertain state still references an oldest epoch,
no tenth epoch or unsafe retirement is allowed: hold admission. Configured lower
retention does not permit a key needed for an unexpired24hour attempt to disappear.
Remove a principal/alias only when all its quota and receipt references have
strictly expired. Missing old keys, incomplete lookup, lost registry, rotation
capacity failure or clock rollback holds intake; none creates a new identity.

Keys are retrieved by exact versioned upstream abuse-purpose secret references,
not raw values or caller URLs. The qualified operator control process registers
new references with the witness before the day boundary. Intake can read only the
bounded admitted key set; cannot create, overwrite or retire key versions. Key
material and socket addresses are transient and excluded from logs/traces/dumps.
This is pseudonymous abuse state, not an anonymity guarantee.

## Independently current witness and restore hold

A witness is an EXPLICIT UNQUALIFIED prerequisite, not an available implementation.
It must be a separately operated, linearizable service whose durable state and
signing keys are outside the intake PostgreSQL/object backup and restore domains.
Its custodian is the upstream operator control authority, authenticated with MFA
for provisioning and suspension. Intake, deployment and restored database roles
cannot initialize, reset, replace or roll back it. The initial bootstrap requires
an independently inspected empty ledger/object namespace; it is not available as
an automatic fallback for a nonempty or previously enrolled installation.

The future activation descriptor must bind one installation namespace, exact
witness TLS origin/server-name/CA and signing-key ID, metadata PostgreSQL TLS
identity, object account/bucket/KMS identity, key registry and caller mTLS client
identity. These are operator-controlled installation values outside restored
application data; there are no default endpoints or public fixtures selected as
resources. Current config `mode=enabled`, matching digests, callbacks and supplied
approvals do not replace qualification of this descriptor and its real service.
The existing closed configuration schema gains no implicit new accepted fields.

The witness stores the current activation epoch, monotonically increasing commit
sequence, digest of CURRENT bounded live ledger/object metadata, authoritative
UTC clock floor, bounded key registry and at most one pending operation. It does
not retain an unbounded historical log or hashes of invalid bodies. Its authenticated
service clock must resist rollback across restart; any regression or inability to
establish a current floor holds intake. PostgreSQL time is checked against that
floor and cannot supply a weaker clock. No process startup or operator boolean
can advance the floor merely to expire budgets.

Witness replies are authenticated, audience-bound and bind namespace, epoch,
sequence, operation nonce, prior/current digest and expiry. Use pinned service
identity and actual protocol verification; never accept a generic function result,
caller-generated key or self-asserted certificate as a verified witness. A
separately scoped metadata-only witness credential under the existing storage
purpose may inspect canonical metadata and write activation/pending fences. It
cannot read payload objects, issue identities or modify operator membership. This
subscope does not add a fifth declared deployment purpose or lend operator payload
credentials to intake. Effective grants and service identity require actual proof.

Every authority-granting ledger mutation, including attempt charge, reservation,
publication and capacity release, follows this serialized protocol. The sole
exception is destructive privacy erasure below: it cannot grant capacity,
publication or activation and may deliberately leave a permanent head mismatch:

1. Witness PREPARE checks live epoch, clock, expiry and previous sequence/digest;
   independently reads the metadata database over verified TLS, and installs one
   nonce-bound pending fence. Its own durable pending state blocks other mutations.
2. A restricted database operation locks that fence, verifies epoch/nonce/previous
   head and performs its single allowed mutation. It writes the next sequence and
   deterministic digest of the complete current bounded metadata in the SAME SQL
   transaction. Intake cannot write a committed marker independently of the change.
3. Witness FINALIZE independently reads the COMMITTED marker and recomputes the
   canonical metadata digest using its real restricted database connection. It
   advances its durable head only on an exact match. An intake-supplied digest or
   write acknowledgement alone is insufficient. The response/read gate remains
   closed while either side is pending or heads differ.
4. On lost acknowledgement, timeout or cancellation, keep the operation uncertain
   and hold. Initial profile NEVER CLEARS a prepared operation from absent rows,
   missing fences, an apparent rollback or a new database epoch: a snapshot restored
   between SQL commit and witness FINALIZE could otherwise erase the charge. Only
   a match at the original nonce/head with independently current original-database
   incarnation proof may finalize. A restored database cannot supply that proof.
   Qualified non-restorable incarnation/recovery authority is a hard prerequisite
   for any such recovery and is not supplied here. Without it, pending stays held
   indefinitely. No automatic pending-clear/retry path exists in this profile;
   never refund an attempt, reuse a nonce or allow another commit to leapfrog it.

The digest format and verifier SQL must be specified and independently tested in
the source admission before this protocol is qualified; no opaque fake verifier
is accepted. Metadata includes live quota/alias/receipt/capacity/fence state and
object identity/deadline, but no raw input. Its storage is bounded by the declared
row capacities and fixed-size fields. Expired metadata is removed through fenced
retention; the witness retains only its current digest, not historical per-source
material. Single pending operation is an intentional availability limit.

INITIAL PROFILE: NO RESTORED SNAPSHOT MAY ACTIVATE. A replaced/old database,
mismatched head, changed storage namespace or missing witness enters HOLD before
intake or operator payload inspection. A new epoch cannot repair lost attempts,
revocations or deletion state. No reset, re-enrollment, operator approval or new
namespace is a restore shortcut. Replay/reconciliation permitting restored
activation needs a separate reviewed protocol and is unqualified here. The actual
older-snapshot test must demonstrate denial, not fabricated recovery success.
Suspended state cannot inspect/export restored records, infer cleanup state from
an old snapshot, or use it to change revocations. A separately qualified deletion
service uses independently retained exact object/deadline inventory, outside the
restore domain, to continue expiry deletion while intake is held. This inventory
and tombstones count against the SAME record/byte capacities and require
provider-proven write quiescence before releasing reservations. Their sensitive
fields expire under the normal retention ceiling; the minimal opaque fence
exception below does not retain diagnostic/source/receipt information. It is NOT an unbounded second payload ledger. Missing
current inventory/clock prevents activation. Missing witness never authorizes
expired reads or defers the retention obligation: failures are explicit retention
incidents and admission stays held, not a claim that physical deletion succeeded.

## Strict HTTP and canonical projection

After durable attempt admission, accept only exactly one `application/json`
Content-Type (optional `charset=utf-8` only), known nonnegative Content-Length at
most8192, and absent/identity or one gzip Content-Encoding. Reject duplicate or
combined header values, transfer encodings/trailers and unsupported parameters.
The listener rejects ambiguous HTTP framing and bounds total headers to8192bytes.
Header/TLS overload failures before durable admission are not reported as charged.
Read the declared compressed bytes under the original5second deadline; incomplete
bodies fail. Gzip must have one member, a valid checksum and EOF with ZERO trailing
bytes. Disable concatenated-member acceptance, independently cap output at8192,
and reject a second member, garbage, truncated stream or expansion beyond the cap.
No raw compressed/decompressed body is persisted or hashed as rejection evidence.

Strict UTF-8/JSON decoding rejects duplicate decoded keys, unknown/case-aliased
fields, trailing JSON values, invalid Unicode/scalars and noncanonical registry
values under `diagnosticcontract.DecodeAndValidate` plus the frozen schema.
Construct fresh finite typed values and encode compact JSON in fixed field order,
with the existing unique sorted metric/error/frame rules. Hash ONLY these validated
normalized bytes. Never normalize arbitrary text into an approved signature or
truncate an instruction/URL into an accepted value. Unknown source baselines and
instruction-like signatures fail generically; there is no fetch, command or job.

A recognized wire baseline is NOT proof of the executing component or honest
aggregate collection. Every accepted report has private metadata classification
`unverified`; no caller provenance flag is consumed. This quarantine admission does
not implement the trusted build/source provenance required for later export,
analysis or release. No new freeform wire field or source registry admission occurs.
Rejection persistence is limited to a finite category counter within the active
budget window; no body digest, raw IP, URL, header, credential or arbitrary error
message is retained. Output errors contain no caller-controlled text.

## Metadata, encrypted objects and publication

PostgreSQL stores bounded attempts, principals/aliases, quotas, receipt/dedupe
metadata, object reservations, capacity, deadlines and fences ONLY. Diagnostic
payload bytes never enter PostgreSQL, its WAL or its backups. Payload custody stays
in the declared private `normalized-quarantine-v1` object namespace with mandatory
SSE-KMS using the configured exact upstream KMS key. A database-only payload table,
plaintext spill, local file spool or mock encryption flag is not compatible.

Require verified TLS database connections and distinct restricted credentials.
Intake has EXECUTE only on finite reviewed admission/reservation/publication SQL
functions; no payload SELECT, arbitrary table DML, DDL, job insert or export.
SECURITY DEFINER functions have fixed safe search_path, qualified objects, bounded
results, revoked PUBLIC execution and an offline bootstrap owner inaccessible to
ordinary deployment. Witness metadata/fence, retention maintenance and MFA-backed
operator inspection use distinct narrowly scoped credentials. Operator inspection
uses a gated service, never exported object credentials or long-lived presigned
URLs; every read checks current witness/head and expiry. Intake object authority
is conditional create with the exact namespace/KMS requirement, without read/list,
ACL/public policy, deletion or jobs. Retention has bounded metadata/list/delete
capability, not payload read. Real grants, KMS and IAM remain qualification gates.

After canonical validation, fenced SQL reserves one record and8192 payload bytes
against BOTH pending and accepted capacities before any object write. Set one
immutable expiry at this FIRST reservation, no later than its witness-authorized
creation time plus configured retention (maximum168hours). READY, acceptance,
retry, duplicate and failed/uncertain operations NEVER extend that deadline.
Expired reservations cannot start a new PUT or become accepted. Every pending,
ready or uncertain orphan consumes its reservation; quota is never freed on mere
PUT timeout. Allocate server-random object key/receipt, not request-selected paths.
Create the immutable object conditionally, with exact SSE-KMS request and verified
provider acknowledgement; do not overwrite or publish an existing different object.
Then a fenced SQL mutation records READY receipt metadata, canonical digest/length,
exact object identity and original deadline. Only finalized matching witness/DB
heads plus successful exact-object acknowledgement make it ACCEPTED externally.
No cross-service atomic transaction is claimed. Operator reads are held during
pending/inconsistent state, including READY-before-witness-finalization.

An object success followed by DB/response failure is uncertain, never201. Store its
bounded reservation for retention to reconcile/delete; retries do not create new
objects in that operation. Disable SDK/transport replay: at most one PUT is issued
per reservation. Client cancellation/deadline is NOT provider write quiescence.
An absent HEAD/list result or successful DELETE alone cannot release a reservation:
a late conditional PUT could create the object after that observation or deletion.
Before activation, independently qualify a real bounded provider write-quiescence
or creation-fencing mechanism, including its timeout and late-write behavior. A
caller flag, elapsed delay or assumed AWS guarantee is insufficient; no existing
AWS mechanism is qualified by this design. Unknown outcome or missing quiescence
keeps the reservation charged and unpublishable indefinitely, even after expiry;
perform expiry deletion without claiming the unresolved write is settled.
Release capacity only after provider-proven no-future-creation, verified deletion
of the exact object and fenced ledger completion. On uncertainty, keep
capacity charged and deny new acceptance if necessary. Record/byte ceilings include
pending/orphan payload reservations;8192 is canonical payload capacity, not a claim
about provider billing/encryption/index overhead. Fixed-size metadata/index bounds
are independently accounted. No raw rejected input enters this lifecycle.

Initial storage forbids payload versioning, replication and backups until their
full deletion/restore behavior is qualified. Verify effective configuration/policies
before collection, not a supplied `encrypted` flag. Declared SSE-KMS acceptance is
not cryptographic/provider qualification. Local tests may exercise real adapters
against an explicitly identified test service but cannot qualify AWS/KMS by mocks.

## Duplicates, finite responses and expiry

Dedupe key is PRIVATE `(stable principal, diagnostic profile, canonical digest)`;
never globally query a sender-visible digest index. Receipt is a server-generated
opaque128bit random token (`r_` plus32lowercasehex); it grants no read authority.
Retain one immutable receipt only until the FIRST reservation deadline, at most168hours
from that reservation; acceptance does not restart retention. Neither duplicates nor retries extend it or its object TTL.
There is no historical receipt lookup after expiry. No caller idempotency token,
request ID, source identity or raw-body hash becomes receipt authority.

A quota-ADMITTED duplicate of an unexpired finalized accepted row returns the SAME
receipt with200 and creates no object/reservation. Its attempt was still consumed.
A quota-DENIED retry returns429 WITHOUT parsing/dedupe lookup or disclosing whether
an accepted receipt exists. Thus the normal same-day retry cannot bypass the
one-attempt ceiling. A later quota-admitted duplicate can obtain the stable receipt.
A pending/uncertain match returns503, never an inferred duplicate success. Different
principals/profiles cannot discover each other's receipt even for identical bytes.

All responses use no-store JSON, no redirects, cookies or diagnostic echo. Exact
body vocabulary (receipt appears only in the two finalized success cases):

| Status | Body | Meaning |
|---|---|---|
| 201 | `{"status":"accepted","receipt":"r_<32hex>"}` | New finalized accepted quarantine record |
| 200 | `{"status":"duplicate","receipt":"r_<32hex>"}` | Same scoped unexpired finalized receipt |
| 400 | `{"status":"invalid"}` | Invalid framing, gzip, UTF-8, JSON or diagnostic profile |
| 401 | `{"status":"enrollment-unavailable"}` | Any enrollment/Authorization supplied; no fallback |
| 404 | `{"status":"not-found"}` | Other path |
| 405 | `{"status":"method-not-allowed"}` | Non-POST on intake path; Allow: POST |
| 411 | `{"status":"length-required"}` | Missing/unknown body length or transfer encoding |
| 413 | `{"status":"too-large"}` | Compressed/decoded cap exceeded |
| 415 | `{"status":"unsupported-media"}` | Unsupported/duplicated Content-Type or Content-Encoding |
| 429 | `{"status":"quota"}` | Known exhausted durable budget; no receipt lookup |
| 503 | `{"status":"unavailable"}` | Overload, authority/store/key/clock/retention hold, deadline or uncertain commit |

A transport failure before an HTTP response may have no body/status; it is not a
successful or charged acceptance. Expiry is strict: data at or beyond its deadline
is never returned. Retention runs independently of admission and can delete while
held, using existing witnessed object identity; destructive release is itself
fenced. At expiry, close inspection and perform verified deletion across all
approved sinks. Failed deletion, unverifiable inventory or metadata cleanup holds
new intake; no eviction, extension, overwrite or public fallback exists. Any future
backup/replica must implement the same deadline and restore suppression before
it is enabled. A restored snapshot remains inaccessible and expired objects stay
deleted/nonservable; being in HOLD never authorizes their inspection.

## Destructive expiry and owner-requested early deletion

A pending witness operation cannot extend sensitive-data retention. A distinct
retention credential can destructively erase expired principal/HMAC alias,
attempt, dedupe, receipt and payload-digest metadata from BOTH held/current
metadata domains and exact payload objects without advancing witness head or
releasing capacity. This path cannot publish, mint identity, refund quota or
reactivate a database. If mutation makes reconciliation impossible, admission
stays held; privacy does not depend on maintaining availability. Key references
are removed with expired aliases; retain an old key itself only within the
bounded retirement rule, never an indefinite source link.

After a failed/uncertain write reaches its immutable deadline, the ONLY retained
reservation material permitted beyond that deadline is a bounded opaque fence:
random operation nonce, exact random object key, storage-generation identifier,
reserved byte/record charge and a deletion-required bit. Remove profile, digest,
principal, source alias, receipt and precise report timing. These opaque fences
occupy the existing charged record/byte slots, with no secondary ledger or
eviction. They allow deletion/no-future-creation checks but cannot be queried as
reports. Keep the namespace held and charge unreleased until actual quiescence,
verified deletion and fenced release are possible. This explicit operational-fence
exception does NOT extend payload, receipt or pseudonymous identity retention.
Missing proof keeps capacity unavailable; no object absence/elapsed delay clears it.

An independently qualified retention worker has current exact object/fence
inventory and a rollback-resistant clock outside the restored database domain.
Its custody/grants, bounded accounting and cross-domain erasure must be proven
before activation. If authoritative time/inventory becomes unknown, stop reads and
intake; conservative early erasure of all sensitive controlled-namespace data is
allowed instead of waiting past a possible deadline. The profile promises no
minimum retention. Preserve only opaque charged fences when late creation remains
possible. Provider unavailability is an explicit deletion failure/incident, never
a claim of deletion. No activation while required erasure/quiescence is unproven.

Owner-requested earlier deletion remains mandatory. No public deletion endpoint
or anonymous receipt-based delete grant is created. The required PRIVATE workflow
uses independently qualified upstream operator authentication/MFA, current scoped
delete authority, exact installation/storage generation and bounded target
reservation IDs. A receipt alone, arbitrary target URL, caller-provided owner
flag, callback or presented public key cannot authorize it. An actual owner request
must be authenticated and associated with the target by a separately reviewed
requester-verification/custody procedure; anonymous HMAC identity is not proof of
installation ownership. That requester binding and operator workflow are currently
UNQUALIFIED HARD ACTIVATION PREREQUISITES, not permission to collect now and add
owner deletion later.

Once an authorized private deletion request is admitted, close target inspection
and acceptance within the same5second command bound, set deletion-required and
fix its deadline to the EARLIER of existing expiry and request-admission time.
No confirmation of deletion is sent before real provider quiescence, deletion and
all-sink sensitive erasure are verified. Pending PUT/SQL/witness outcomes remain
held/charged and cannot revive the target. Failure at5seconds returns private
unavailable/uncertain status, denies intake and records a bounded private deletion
incident under the required qualified operator procedure; it neither restores access nor extends a deadline. Keep only
bounded non-diagnostic audit outcome/opaque charged fence as above. Physical
absence is not claimed while the provider is unavailable. Admission stays disabled
until an actual early-deletion proof covers authority, pending writes, replicas
(if ever admitted), metadata/alias erasure and older restored snapshots.

## Required source and service qualification

Source admission must specify exact metadata schema/functions/canonical witness
digest and actual witness wire verification under this design; separately admit
any new dependency or migration. This document does not grant that ownership.
Acceptance requires real TLS/restricted PostgreSQL and an actual independently
custodied witness fixture, not an in-memory callback. Missing prerequisites fail
explicitly. Exercise actual HTTP statuses/bodies, both byte limits, one-member gzip,
duplicate keys/Unicode/trailing bytes, quota-denied and later admitted duplicates,
concurrent budgets/capacity, restart, midnight/key rotation/retirement boundaries,
full identity capacity, clock rollback, object/SQL/witness commit uncertainty,
late stale writers, deadline, all-sink early deletion and retention failures. Restore an ACTUAL older database
snapshot while witness remains current: prove intake and reads stay held, lost
attempts are not reset and expired data is neither served nor resurrected. Observe
no outbound instruction/URL request and no coding/publication effect, including a
compiled negative/restoration at that boundary. Provider/physical encryption,
effective IAM, deployed isolation, private disclosure channel and hosted operation
remain separate gates. Nothing here enables external intake or accepts full T14.4.
