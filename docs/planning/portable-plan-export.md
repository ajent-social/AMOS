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
