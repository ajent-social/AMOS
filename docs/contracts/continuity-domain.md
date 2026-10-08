# Continuity domain contract, revision 1

Status: proposed. Independent exact-head review and guarded adoption precede
source. Scope: pure application-owned domain rules only. No I/O, database,
HTTP handlers, principals, migrations, shared executor or external effects.

## Ownership and API

New Go package `examples/continuity/domain` owns the following finite model.
Constructors/functions return errors on invalid input; no panics. It may use
standard library and the already pinned UUID dependency. Public sentinel errors
are `ErrInvalid`, `ErrConflict` and `ErrDecisionDisabled`. All strings must be
valid UTF-8; IDs are canonical lowercase UUIDv7 text. Domain IDs are selectors,
not proof of authority. The persistence owner later mints new IDs.

```go
type CaseStatus string
const (
    AwaitingOwner CaseStatus = "awaiting_owner"
    ReadyToArrange CaseStatus = "ready_to_arrange"
    InProgress CaseStatus = "in_progress"
    NeedsAssessment CaseStatus = "needs_assessment"
    Completed CaseStatus = "completed"
)
type Case struct {
    ID, PropertyID, Title string
    Status CaseStatus
    Revision int64
    SourceIDs []string
}
type ChecklistItem struct { ID, Label string; Done, HumanDecision bool }
type Application struct { ID, PropertyID string; Revision int64; Items []ChecklistItem }
type Procedure struct { ID, Title, Body string; Revision int64 }
type Draft struct { CaseID string; CaseRevision int64; Body string; SourceIDs []string }
func ChangeCase(current Case, expected int64, next CaseStatus) (Case, error)
func SetChecklist(current Application, expected int64, itemID string, done bool) (Application, error)
func EditProcedure(current Procedure, expected int64, body string) (Procedure, error)
func PrepareDraft(current Case, body string) (Draft, error)
```

These value functions are proposals for application code, not an authorization
API. They validate the entire supplied current value before computing a new
value. Failure returns the zero output and the appropriate sentinel only.
Input and output slices never alias: caller mutation cannot rewrite retained
state. No function mutates its input. No clock, UUID generation, hidden global
state, source fetch, model call or side effect occurs.

## Validation and mutation semantics

Every revision is positive. Mutations require expected == current.Revision and
reject overflow at MaxInt64. Invalid input takes precedence over conflict;
for valid input a revision mismatch is ErrConflict. Successful mutations always
increment once, including setting an already selected value. Persistence later
uses an atomic compare-and-swap; this calculation alone is not durable replay.

Titles/labels contain 1..200 Unicode code points after whitespace trimming;
canonical output retains trimmed text. Procedure body is 1..6000 code points;
draft body 1..4000. ASCII controls other than tab and newline are rejected in
body text; all controls are rejected in titles/labels. Bodies are stored as
plain text, not trusted HTML or executable instructions. ID lists contain
1..32 distinct canonical IDs. Source IDs are references; a later authorized
repository must resolve them before disclosure or persistence.

A Case validates both IDs, title, status, revision and source list. Any of the
five states may be recorded from any other state in this evaluation app. A state
change records an operator assertion only. It cannot record approval evidence,
book work, spend money or establish verified real-world completion.

An Application has 1..32 items with distinct canonical IDs and valid labels,
exactly one item marked HumanDecision, and that item must remain Done=false.
SetChecklist rejects changes to that item with ErrDecisionDisabled regardless
of requested boolean value. Unknown item IDs are ErrInvalid. Validate the whole
application first, then expected revision, then target decision restriction.
Supporting items do not imply screening, eligibility, consent verification or
an application decision. No score, rank, accept or reject operation exists.

EditProcedure validates existing ID/title/body/revision and the proposed body,
then returns a copied value with the new trimmed body and incremented revision.
Text changes cannot modify scenario logic, permission or effect policy.

PrepareDraft validates the current Case and supplied body and returns only its
case ID/revision, trimmed plain-text body and copied source IDs. It has no
recipient, provider ID, send status or delivery operation. Persistence records
it as unsent and must check the referenced current case revision atomically.
It never generates a fact, permission, quote or message from external sources.

## Verification and later seams

Tests cover all finite statuses, invalid/unknown values, invalid UUIDs, empty and
oversized strings, invalid UTF-8/control characters, duplicate/missing sources,
unknown/duplicate checklist IDs, final-decision rejection, stale/zero/overflow
revisions and slice non-aliasing. A meaningful isolated negative removes a
conflict or human-decision guard, observes failure, restores exact source and
reruns. Normal/race/vet/pinned lint and independent exact-head review apply.

This package requires no service. Its checks cannot qualify a database or UI.
The later app repository owns transactional CAS, atomic activity/draft storage,
current authorization, source resolution and safe public errors. Host source
waits for an adopted composition contract with actual session, workspace, CSRF,
revocation and required-service evidence. API/MCP generation stays gated on the
shared invocation contract. No shared migration number is allocated here.
