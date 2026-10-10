# Transaction-time current session (v1.24 draft)

Status: concrete design candidate, not adopted and not source-ready. Independent
review must resolve the finite writer and producer gates below before any source
assignment. This is the next contract artifact in the
[continuity integration path](../planning/replacement-integration-path.md), not
another completed audit or a host/executor acceptance claim.

Source baseline: `7659cfa8972741578a1a34a04c1733f2dfff3cdc`.
The existing [session preflight](../planning/session-recheck-preflight.md) and
its unresolved findings remain authoritative until this candidate is adopted.

## Exact proposed API and capability boundary

Add only this exported method to `identity/session.Service`:

```go
func (s *Service) RecheckCurrentTx(ctx context.Context, tx *sql.Tx) (identity.Principal, error)
```

The supplied transaction comes from the same trusted database capability as this
service. The integration owner must prove that binding in each composition; a
`*sql.Tx` does not itself prove which service opened it. This API accepts no raw
cookie, token, session ID, person ID, caller clock, workspace or policy assertion.
It neither opens nor completes a transaction, renews a session, sets a cookie,
mutates context, invokes a callback nor grants resource access. Results remain
provisional until caller commit. Missing prerequisites fail closed.

Inside session middleware, after successful authentication commit and all
applicable origin/CSRF checks, mint a private immutable context value containing
this exact `*Service`, authenticated session ID and the admitted principal
snapshot. Keep its key/type and construction/extraction private to the session
package. Retain no raw token or cookie. No exported context-injection function,
serialization or reference DTO is added. A principal alone is not this proof.
The caller must retain the original request context and its bounded lifetime.

Middleware must reject duplicate configured session cookies rather than choosing
one. Constructor configuration must copy AllowedOrigins so later caller slice
mutation cannot change admission. These are explicit compatibility/immutability
corrections; existing cookie names, formats, lifetimes and public error mapping
stay unchanged. Proof is never minted after failed commit or request admission.

## Proposed recheck algorithm

1. Nil service/transaction/context returns zero principal and ErrUnavailable.
   Absent private proof, a different service instance, or mismatching current
   context principal returns zero principal and ErrUnauthenticated before SQL.
   Compare the entire admitted immutable principal value; public caller fields
   cannot substitute. Database failures return zero principal and ErrUnavailable,
   preserving sanitized errors.
2. Require actual READ COMMITTED and a transaction that permits row locking.
   Do not silently change isolation or retry. Unsupported isolation, closed or
   canceled transactions and failure to establish mode are unavailable.
3. Lock the exact admitted person in configured installation/application with a
   separate `SELECT ... FOR SHARE`; then lock the exact session ID with a separate
   `SELECT ... FOR SHARE`. Do not use a joined multi-table locking clause as an
   ordering guarantee. Read and retain all fields needed for the checks below.
4. Only after both lock statements finish, sample `clock_timestamp()` separately.
   Require active person, matching current person/session/admitted security epoch,
   exact person/session/installation/application/environment binding, unchanged
   authentication method/time, no revocation, and strict idle/absolute expiry
   after that instant. Invalid or absent current proof is unauthenticated; query,
   scan, malformed persisted state or unestablished freshness is unavailable.
5. Resolve current assurance using that same sampled instant. Expired elevated
   assurance downgrades to aal1; expired base session is unauthenticated. Do not
   upgrade beyond the admitted snapshot or extend its assurance validity. The
   exact case table below governs expiry and an already downgraded snapshot.
   With PersistAssurance disabled, no optional-column read is allowed and only
   the existing aal1 profile is available.
6. Build and return the refreshed principal through identity's internal verified
   credential bridge, with zero output on every failure. Do not overwrite the
   original context proof. Repeated calls in the same transaction are permitted;
   they acquire no stronger locks and resample time after later blocking work.

For assurance, let E be current absolute session expiry (idle validity is checked
separately), A be admitted assurance expiry and C be current elevated expiry.
The sampled instant must be strictly before every selected validity bound.

| Admitted snapshot | Current durable assurance at the sampled instant | Returned assurance |
| --- | --- | --- |
| aal1 | Any level | aal1 through min(A,E); reject as unauthenticated if that bound has elapsed. No upgrade. |
| aal2/aal3 with A elapsed | Any level | aal1 through E. The expired step-up does not terminate an otherwise live base session. |
| aal2/aal3 with A live | Elevated current level with C live | Lower of the admitted/current levels through min(A,C,E). |
| aal2/aal3 with A live | Current aal1 or expired elevated C | aal1 through E. |

Unknown levels, inconsistent nullable assurance fields, invalid chronology or
bounds beyond the schema's allowed lifetime are unavailable, not downgraded
success. No row is repaired by this read. Tests must cover every table row,
strict equality at A/C/E and a fresh higher persisted assurance after admission.

The authority caller passes the returned principal into workspace/policy/handler
logic. It must not keep using the original snapshot after an Allowed decision.
After later workspace/resource/policy waits, recheck and reevaluate the relevant
requirements before disclosure/mutation. This method alone does not know an
operation's minimum assurance, permissions, entitlement or resource predicate.
No `TransactionAuthorizer` return-type change is silently adopted here.

## Lock protocol candidate and exact unresolved writers

For a non-mutating authority reader, person SHARE then session SHARE is the
candidate. Unlike the earlier exclusive-person proposal, a shared person lock
can coexist with the key-share protection used by a session insertion's foreign
key while still conflicting with person state/epoch updates. PostgreSQL's
[documented row-lock conflicts](https://www.postgresql.org/docs/16/explicit-locking.html#LOCKING-ROWS)
support this distinction; they do not qualify the complete AMOS transaction graph.

| Existing path | Required resolution before adoption/source |
| --- | --- |
| Middleware renewal: session UPDATE lock, then unlocked person read | Prove it does not request a conflicting person lock after session admission; retain post-lock renewal/assurance checks. Reader does not call renewal. |
| Standalone issuance/rotation: old session update, then new-session person foreign key | Test actual same-person and cross-person cookie paths against shared-person readers. Do not introduce exclusive person locking only inside issuance. Confirm exact FK lock behavior and current credential/state/epoch admission after waits. |
| Recovery complete/password change: parent person before bulk session revocation | Prove the held parent gate excludes a same-person authority reader before any child-session lock. Preserve the entire revocation set; no truncation, arbitrary sorted subset or repeated batch commits. Include caller-held contact/credential/challenge and policy edges. |
| Magic and MFA: person/credential/challenge/factor work before session rotation | Qualify actual callbacks, proof/assurance time after waits and old-cookie ownership. A session reader cannot repair proof minted from an invalid producer. Existing foreign ownership remains. |
| Federation link: `insertBinding` uses `FOR UPDATE OF p,s` | This clause does not prove person-before-session order. A concrete candidate correction is a separate scoped person lock before the existing session/binding logic, with all predicates and the final post-wait flow fence preserved. No source edit or adoption is authorized here; qualify the complete callback transaction, not only this leaf. |
| Signout/scoped revocation and assurance update | Session-only writes add no later person lock by themselves; prove reader serialization and failure mapping rather than labeling every session writer incompatible. |
| Email confirmation, registration and issuance | Include their actual parent/contact/challenge and deferred workspace-trigger edges. v1.23 correction and earlier primitive fixes are bounded inputs, not full writer qualification. |
| Workspace/resource/callback work after identity | Its separate contract must forbid an order that re-enters conflicting identity locks after resources. Include role/absence predicates and deferred owner checks. Repeated identity recheck may only resample already-held compatible rows. |

Adoption is blocked until the proposed assurance case table, same-database composition,
all supported producer freshness, and complete selected writer/callback/trigger
protocol have independently reviewed resolutions and prescribed actual schedules.
A proven subset can be a separately reviewed explicit host profile; it cannot
silently disable existing product scope or permit unsupported routes.

## Required evidence and source ownership after adoption

The integrator owns this API and exact source assignment; session source falls
under T3.3, current-authority integration under T2.8. No foreign flow source is
delegated by this draft. Do not implement this candidate while the gates above
remain unresolved. A later source grant must name exact files/methods and the
reviewed contract revision; schema, module, workspace, executor and host wiring
are separate ownership boundaries.

Required actual TLS runtime-role checks must exercise exported middleware and
recheck together: absent/fabricated/cross-instance proof, duplicate cookies,
failed authentication commit/origin/CSRF, copied configuration, supported modes,
current revocation/state/epoch, strict idle/absolute/assurance boundaries during
observed person/session waits, downgrade and no-upgrade, unchanged session data,
caller rollback, errors, cancellation and reacquisition after resource waits.
Prove denied and unavailable are distinct and no stale principal reaches an
allowed consumer. Test the exact supported writers above with database-observed
barriers and complete row sets. No sleep, mock or source-read result qualifies
those schedules. Independent rejected-protocol mutations, restored checks and
fresh landed service checks remain mandatory. Host/executor/provider/release
qualification is not implied by the primitive.


## Concrete composition and writer resolutions under review

The [database composition proposal](current-session-composition.md) selects one
privately retained runtime capability and makes the trusted-caller limitation
explicit. The [writer/producer proposal](current-session-writers.md) specifies
complete transaction-root admission, cross-person discovery, ordered row sets,
method evidence and final constraint/freshness fences with finite schedules.
Its database-wide singleton is a substantive serialization/migration proposal,
not an adopted default. Its exact shared Go surface, complete participant
coverage and provider evidence remain review gates. Neither document grants
source ownership, closes the workspace/resource contract or qualifies actual
writers. Adoption review must decide these concrete proposals rather than
repeat a generic source audit.
