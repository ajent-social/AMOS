# Continuity domain values

This application-owned package implements the [continuity domain contract](../../../docs/contracts/continuity-domain.md).
Its four functions validate the entire supplied value, return canonical trimmed
text and fresh slices, and return a zero value plus an exact sentinel on failure.
Positive revisions are required; overflow is invalid and a valid stale revision
conflicts. Successful mutations increment once, even for unchanged selections.

Cases record operator assertions. Supporting checklist items never establish a
human decision, and the human-decision item cannot be changed. Procedure and draft
bodies are plain text, including any markup supplied by a caller; a rendering
adapter must escape them. Drafts have no delivery operation. IDs are selectors,
not authority, and sources are unresolved references.

There is no I/O, persistence, HTTP, principal, clock, identifier generation,
scoring, booking or sending. Later authorized application composition must resolve
sources and perform transactional revision checks and persistence. Package tests
qualify only these pure value rules, not services or replacement acceptance.

Run scoped tests with `go test ./examples/continuity/domain`, race tests with
`go test -race ./examples/continuity/domain`, and vet with
`go vet ./examples/continuity/domain`, under the repository execution guard.
