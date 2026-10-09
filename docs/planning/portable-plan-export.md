# Wazi plan export

The current authored authority is docs/planning/wazi-source.json. Run
go run ./cmd/portableplan (or the equivalent --sdlc mode) from the repository root
to export the complete product and delivery graph as amos:plan. The source's
exact bytes bind definition revision/digest. Preserve native task IDs and stages;
product-target dependencies use domain-accepted requirements, while untyped
lifecycle-to-lifecycle dependencies use execution-complete. Stage names supply
no approval, landing or deployment authority.

The frozen Wazi 0.0.1 digest is
sha256:7582512f122d2f2a9c4461facc7541c9887053f137260d6ebe9c6dea611d039d.
Validate with its owning offline validator, not a copied schema.

--historical-product is the explicit retired 257-task baseline export. Its plan
identity is amos:historical-product-plan and metadata historicalBaseline is true.
It is not the current plan. Old native inputs remain preserved historical data.

No snapshot, execution evidence or satisfied evaluation is emitted. Narrative
acceptance remains unqualified. The SDLC journal is separately digested.
The retired renderer, plan-check and release-evidence commands refuse current
invocation until their replacements are qualified; do not bypass that guard to
rewrite live projections from stale baseline files. See wazi-migration.md for
actual checks and remaining delivery/tooling gates.

## Discoverable Markdown compatibility view

The current inventory contains **1,598 nodes: 257 product tasks and 1,341 delivery
rows**, retaining the original 1,501 IDs. Product acceptance remains **59/257**.
The native Markdown view is `docs/plan.md` plus direct `docs/plans/*.md` files.
The existing product epics each retain their original authored text, IDs,
dependencies, acceptance, scope and external gates. Generated delivery shards
contain the remaining IDs exactly once. Nested historical SDLC files remain
preserved but are not additional scanner inputs.

Product milestones `S0` through `S5` are preserved as `product-milestone`;
`Stage: implement` routes product work into the display's Build column without
claiming lifecycle advancement. Delivery stages preserve `preflight`,
`implement`, `verify`, `review`, `merge` and `verify-landed`. Display aliases map
`author` to `implement`, and `landed`/`accept` to `verify-landed`; `authored-stage`
retains the exact original label. Unknown labels fail native generation until
an explicit mapping is reviewed. The JSON projection still preserves unknown
stages and all authored statuses remain pending.

Checkboxes are a **reported registry display**, never execution authority:
`ACCEPTED` products and `COMPLETE` delivery records use `[x]`; `IN_PROGRESS` uses
`[~]`; `BLOCKED` uses `[-]`; `PLANNED` and absent records use `[ ]`.
Each row identifies `status-source`, `reported-status` and display-only authority.
The consumer can additionally mark unfinished dependencies blocked. Its combined
node completion percentage is not product acceptance or release readiness.
Receipts, admission, claims, independent review and provider gates remain separate.

The Python current-source pipeline remains supported; the earlier retired-command
paragraph above describes the historical migration gate. Default JSON/table
output is unchanged. Native output is an explicit additional mode:

```sh
python3 scripts/render-plan.py --native-markdown --output-dir ./native-plan-preview
python3 scripts/render-plan.py --native-markdown --check --output-dir ./native-plan-preview
python3 scripts/check-plan.py --native-root .
python3 scripts/check-plan.py --native-root ./native-plan-preview --consumer-root "$PLAN_CONSUMER_ROOT"
python3 -m unittest scripts.tests.test_current_plan_tooling
python3 scripts/tests/test_current_plan_tooling.py --consumer-root "$PLAN_CONSUMER_ROOT"
```

`PLAN_CONSUMER_ROOT` must name a separately obtained actual consumer checkout
containing `scripts/plans.mjs`, `src/plan-parser.mjs` and `src/demo.mjs`; Node.js
must be available. The explicit consumer check fails if any prerequisite is
missing. It imports and executes the real `scanPlans`, `parsePlan` and `laneFor`,
without a copied parser, dependency installation or UI build. Ordinary Python
source checks need no consumer checkout and establish no consumer compatibility.
Exact consumer revision and file hashes belong in the qualification receipt;
these checks do not establish native UI acceptance.

Rendering requires a new directory and never overwrites an existing directory,
symlink, baseline or registry. Output contains a staged `docs/` tree. The plan
owner reviews and installs only its Markdown files, then rechecks the final tree
against its exact canonical source and registry bytes. Rendering a projected
product template again produces identical bytes. The checker detects stale
source/registry digests, missing or extra discoverable files, altered metadata
and authored product text. Files stay below the consumer's 1 MB/file and 256
split-file limits. Product preservation pins cover original authored bytes with
checkboxes normalized and only generated metadata/delimiters removed; intentional
authored changes require a reviewed pin update. These pins preserve text, not
receipt authenticity. Lifecycle titles and acceptance use bracket delimiters so
metadata-like prose cannot silently truncate current authored wording.

The actual-consumer regression entry point checks the full current inventory,
all supported display lanes, preserved dependencies/acceptance/milestones,
registry-only completion, duplicate IDs, missing discovery, unsupported stages,
status corruption and acceptance corruption. Source tests also cover no-overwrite,
idempotence, missing consumer prerequisites and stale registry projections.
The legacy `tests/planning/test_retired_plan_tools.py` has three pre-existing
harness failures: obsolete diagnostic expectations and an isolated release-tool
fixture missing its checker dependency. They are not passing evidence; the
explicit historical structure/self-test commands remain the historical checks.
