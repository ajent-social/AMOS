# Pure continuity scenarios, revision 1

Status: proposed; independent exact-head review and guarded adoption required
before implementation. T-RPL-GUIDE remains an application-owned source task;
this contract does not accept a host, interface or replacement rehearsal.

## Boundary and ownership

`examples/continuity/guide` is a pure Go package with no I/O, clock, global mutable
state, model, provider, permission decision or effect. It uses the adopted domain
package and standard library. Application scenarios and text belong here, never
in framework packages. The caller owns authenticated retrieval and supplies a
bounded selection of current records. A selection is not a complete inventory;
no response may claim universal coverage, current authority or actual completion.

Exact API:

```go
type Topic string
const (
    Attention Topic = "attention"
    ExplainCase Topic = "explain_case"
    SpendingAuthority Topic = "spending_authority"
    Handover Topic = "handover"
    OwnerDraft Topic = "owner_draft"
)
type Request struct { Topic Topic; CaseID string }
type Source struct { ID, Title, Body, SHA256 string }
type Selection struct { Cases []domain.Case; Sources []Source }
type Item struct { CaseID, Text string; SourceIDs []string }
type Response struct {
    Title, Summary string
    Items []Item
    Draft *domain.Draft
}
func ParseQuestion(question, selectedCaseID string) (Request, error)
func Answer(request Request, selection Selection) (Response, error)
```

Sentinels are `ErrInvalid`, `ErrNotFound` and `ErrUnsupported`. Failures return
zero output and the exact appropriate sentinel; no partial response. Sources,
case IDs and draft values are selectors/data, never authority. No context,
principal, transaction, browser credential or HTTP type appears in this API.

## Validation

All strings are valid UTF-8. IDs are canonical lowercase RFC4122 UUIDv7 strings.
A selection has 0..100 distinct cases and 0..100 distinct sources; duplicate IDs
within either set fail. Cases validate the full adopted domain shape, including
positive revision (MaxInt64 remains valid for reading), finite status, trimmed
bounded title and 1..32 distinct source IDs. Every case source ID resolves in the
provided source set. Missing references fail ErrNotFound, not invented content.

Sources have 1..200-code-point titles after trimming, with no controls; bodies
have 1..65536 UTF-8 bytes and nonempty trimmed content. Body ASCII controls other
than tab/newline are invalid, matching the domain convention. SHA256 is exactly
the lowercase hexadecimal SHA-256 of the original, untrimmed UTF-8 body bytes.
A mismatch is ErrInvalid. Hash verification does not prove source authenticity,
truth or authority. Validate the entire supplied selection before composing any
answer, including unused records; do not quietly omit corrupt values.

For ExplainCase and OwnerDraft, CaseID is required and must be valid; an absent
matching case yields ErrNotFound. For other topics CaseID must be empty, preventing
an ignored selector from appearing to constrain a response. Unknown topics yield
ErrUnsupported. Malformed field shapes yield ErrInvalid. Request validation
precedes selection validation, and selection validation precedes case lookup.
Inputs are never mutated. Returned slices, including nested source IDs and draft
source IDs, do not alias input slices or one another.

## Finite question parser

ParseQuestion validates question UTF-8 and a 1..240-code-point length after
trimming; controls are rejected. Case-insensitive matching permits only these
exact trimmed phrases (one final question mark may be removed):

| Phrase | Topic |
|---|---|
| What needs attention today | Attention |
| Explain this case | ExplainCase |
| What is the spending authority | SpendingAuthority |
| Help with a handover | Handover |
| Prepare an owner update | OwnerDraft |

No substring search, prompt execution, arbitrary question answering or external
fallback exists. Other well-formed questions yield ErrUnsupported. CaseID rules
are identical to Answer; the UI must supply a selected case only for a case topic.
Procedure or source text cannot add phrases, change scenario logic or confer
permissions. This finite parser is an explicitly limited convenience, not an AI.

## Response semantics

Answers are deterministic for the same values, including when selection input
order differs. Sort case-derived items by canonical case ID and source references
by ID. Use ordinary plain text, never HTML or Markdown with embedded links;
the later renderer escapes all values and resolves links within authorized scope.
Titles and summaries use application-owned fixed wording plus validated values.
Items contain only an existing selected case ID, bounded descriptive text and
copied resolvable source IDs. No invented source, quote, price, recipient, booking,
payment, screening result, spending limit or permission may be introduced.

- Attention lists every non-completed case in the supplied selection, with title
  and recorded status. Empty results say no open cases in the supplied selection;
  they cannot claim that the business has no outstanding work.
- ExplainCase describes the selected case's title and recorded status, with all
  its linked source IDs. An awaiting-owner status is an operator record, not proof
  of legal authority, consent or a completed external action. Source text is
  available to inspect; this package does not infer new facts from its prose.
- SpendingAuthority states that spending authority is not recorded by these
  scenario inputs and must not be inferred from status, source text or quotes.
  It returns no case items or draft and grants no authority to another operation.
- Handover lists non-completed selected cases and fixed reminders to review
  original sources, record decisions, keep final application decisions with a
  person and arrange access/recovery separately. This is a bounded briefing,
  never a certification of succession readiness.
- OwnerDraft returns a preview produced through domain.PrepareDraft for the
  selected current case. Its plain-text body identifies the case and its recorded
  status and requests human review of linked sources before action. It contains
  no address, recipient, send status or delivery intent. The response summary
  clearly says preview only, not saved or sent. Actual saving must pass the
  separately qualified repository's current-revision check. All other topics
  return a nil Draft.

Response text must stay bounded: Title 1..200 code points, Summary 1..2000,
item Text 1..1000, at most100 items and at most32 references per item. Successful
Items are non-nil (possibly empty). OwnerDraft and ExplainCase have exactly one
item identifying the selected case. Fixed guidance cannot promise external action.

## Required checks and later gates

Tests exercise every topic, every finite case status, empty/partial selection,
unknown question/topic/ID, duplicate and missing sources, corrupt hashes, UTF-8
and control/size bounds, revision extremes, permutation determinism and deep
non-aliasing. A meaningful isolated mutation permitting unresolved sources,
wrong digests or an invented authority answer must fail its intended assertion;
restore exact source and rerun. Normal/race/vet/pinned lint and different exact-head
review apply. The package needs no service; its tests do not qualify persistence,
HTTP, source authorization or provider retrieval. No application, shared contract,
migration, generator or executable wiring outside the owned package is source-owned.
