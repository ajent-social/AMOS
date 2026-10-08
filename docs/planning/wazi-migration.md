# Wazi plan migration

The authored source is docs/planning/wazi-source.json. It materializes both
previous native task collections into one retrievable source with exact-byte
SHA256 binding. The source preserves product arrays/epic context, lifecycle
context, migration input digests and original product acceptance arrays.
Portable acceptance is their original ordered text joined by newlines.
Stages and IDs are retained, including stages unknown to a consumer.

The --sdlc exporter emits amos:plan with stable amos:task:<original ID> identifiers.
The default and --sdlc entrypoints both select the complete current plan.
--historical-product explicitly exports amos:historical-product-plan with
historicalBaseline=true. It cannot silently replace amos:plan. The SDLC journal contributes
unqualified narrative metadata only. Product narrative acceptance is retained
in the source with original records. All portable authored statuses remain
pending; consumers cannot infer readiness.

Contract: experimental Wazi 0.0.1, frozen public specification revision
f04497a3fcde3c1b78d09b683405d4d9f7645efc, digest
sha256:7582512f122d2f2a9c4461facc7541c9887053f137260d6ebe9c6dea611d039d.
Owning validator source revision: bf00c79cc19c4eb5adf4e9ec704b7895e06df3e9.
This is schema/coherence conformance, not source authentication, trustworthy
receipts, scheduling admission or production approval.

## Verification

On the qualified Linux execution host:
- Scoped exporter tests pass, including invalid dependencies, cycles, missing
  acceptance, foreign journal entries, deterministic output and non-promotion.
- Owning wazi-contract fixtures with the pinned digest exits zero.
- Owning validator reports valid true, zero findings and authorityAuthenticated
  false for the 1,501-task export.
- Independent exact-head review, merge and fresh landed verification are pending.

## Delivery tracking

- WAZI-1: preserve complete sources and stable identifiers (implemented).
- WAZI-2: validate actual source and Wazi fixtures (obtained, source-only).
- WAZI-3: independent exact-head review; fix findings and repeat checks (pending).
- WAZI-4: guarded merge and landed checks (depends on WAZI-3).
- RECOVER-1: reconcile worktree custody without deleting source.
- RECOVER-2: finish runtime-binding and session-freshness candidates through
  distinct review, verification and guarded landing; reconcile older PR2.

These entries track this migration; they do not fabricate completed product tasks
or replace existing task/claim ownership.

The whole-repository public scan reports 44 findings in existing source outside this change; it is not recorded as passing. The focused migration-artifact scan is recorded separately. Local original worktree metadata records 118 registrations, 87 present and 31 missing directories; original references and migrated source remain preserved.


## Independent review corrections
Review of a6d20e3 requested changes; it did not approve merge.
- R1: lifecycle-to-product prerequisites retain domain acceptance. Only true
  lifecycle-to-lifecycle ordering becomes execution-complete.
- R2: default export uses the complete current plan; retired export requires an
  explicit flag and distinct plan identity.
- R3: retired renderer/check/release tools refuse invocation when the adopted
  master exists. No stale current views may be rewritten or qualified. Migration
  of their full current projection/release functionality remains explicitly open.
  Each product has one authored representation in native_product_task; its outer
  row supplies identity/epic/narrative metadata only. Duplicate authored fields,
  missing retained task IDs and dangling/cyclic dependencies are rejected.
Current semantic conformance does not imply replacement release tooling exists.
