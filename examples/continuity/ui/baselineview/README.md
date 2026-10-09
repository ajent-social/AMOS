# Baseline presentation

Pure server-rendered HTML for supplied properties, cases, applications,
procedures, activity and deterministic guide results. The caller owns retrieval,
authority, CSRF verification, HTTP behavior and committed publication. Rendered
forms name later host binding targets; this package exposes no handler or effect.

Every renderer returns newly owned bytes or nil with exact `ErrInvalid`.
Complete raw bounds precede record copying and guide composition. All supplied
fields are validated, including fields not displayed in list summaries. HTML
uses automatic contextual escaping and a 512 KiB output cap. The last positive
revision is readable; rendering never increments it. Saved draft revisions and
source references remain separate from the current case and a stale draft has
an explicit warning. No sending or human-decision form is exposed.

The final boolean chooses a full document or the sole
`continuity-baseline-content` section. Full pages reuse `/assets/base.css`.
Ordinary forms and links require no JavaScript. Authentication, routes, mutation
services, persistence, browser policy and actual host journeys remain unqualified.

```sh
go test ./examples/continuity/ui/baselineview
go test -race ./examples/continuity/ui/baselineview
go vet ./examples/continuity/ui/baselineview
```

`TestStaticArtifacts` always checks synthetic document rendering. An explicit
`AMOS_BASELINE_ARTIFACT_DIR` optionally writes those files into a new directory
whose parent exists; existing directories are refused. After independent harness
review, `testdata/browser.cjs` can inspect those files with an already available
Playwright module and Chromium executable. It disables page JavaScript, checks
keyboard access and narrow/wide layouts, and records CSS text scaling separately
from browser zoom. The sole HTML substitution binds the existing stylesheet to
a local copy. It submits no forms and establishes no authenticated journey,
HTTP/HTMX behavior, stored changes or complete replacement acceptance.
