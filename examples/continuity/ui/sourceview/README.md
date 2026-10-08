# Pure source views

`RenderSources`, `RenderSource` and `RenderSourceError` implement the adopted
[source view contract](../../../../docs/contracts/continuity-source-view.md).
They return fresh buffered HTML or nil bytes and exact `ErrInvalid`. They use
standard `html/template`; no trusted-content conversion is exposed.

The caller supplies already-authorized values and owns current resource authority,
repository retrieval, transaction completion, transport status and headers. Digest
validation establishes only correspondence with the supplied original body bytes.
This package does not authorize disclosure, query storage, send anything or serve
HTTP. List pagination offers an attempt to continue a supplied page, not a count
or assertion that another record exists.

A full document includes `/assets/base.css`, a skip link and main landmark. A
fragment is only `section#continuity-source-content`; the future transport must
choose its HTMX target and swap behavior. Plain source text is never interpreted
as HTML, Markdown, instructions or navigation. The extra initial newline in the
`pre` template compensates for HTML's removal of one initial newline so parsed
body text preserves the original bytes.

## Local checks

Run package tests, race tests, vet and pinned repository lint. The tests inspect
parsed HTML, form/navigation values, invalid input nil outputs, exact body text,
maximum inputs, independent outputs and concurrent calls. Run the synthetic
artifact test with `SOURCEVIEW_STATIC_DIR` set to a new absolute directory to
write detail/list documents for the static browser harness in `testdata/`.

The browser harness uses an explicitly supplied existing Playwright installation
and browser binary. It runs without JavaScript or a server, on synthetic `file:`
artifacts only. It intentionally substitutes a local copied stylesheet reference
for the full document's origin-root asset reference. This proves static
presentation with that stylesheet, not deployed asset routing, authenticated
retrieval, HTTP status/headers, HTMX transport, service behavior or product
acceptance. Source links and search forms are inspected but not submitted because
no server or authenticated caller exists in this check.

The digest negative harness in `testdata/digest-negative.py` uses a Go overlay
that disables the real comparison and requires `TestInvalidDetail/wrong_digest`
to fail its nil-output/exact-error assertion. It then runs the full package without
the overlay and records the original, mutated and restored hashes. It never
modifies production source on disk.
