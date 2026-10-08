# Current-source plan tooling

The Python tools now consume `docs/planning/wazi-source.json`, the adopted
1,501-task source. They do not fall back to `plan-data.json`. This is a bounded
source and projection check, not a replacement for the pinned Wazi validator,
receipt authentication, execution admission, independent review or release gates.

## Safe invocation

```sh
python3 scripts/check-plan.py
python3 scripts/render-plan.py --check
python3 scripts/render-plan.py --output-dir ./current-plan-preview
python3 scripts/render-plan.py --check --output-dir ./current-plan-preview
python3 scripts/check-plan.py --projection-dir ./current-plan-preview
```

Rendering requires `--check` or an output directory. Without an output directory,
`--check` validates generation in memory only; it does not claim any existing
projection is fresh. Writing requires a new directory whose parent exists.
Existing directories, files and symlinks are refused. No task contracts, existing
plan projections, use-case manifests, execution registries or receipts are
rewritten. An interrupted write can leave a partial new directory; freshness
checks reject it. Use another new directory for the next render.

Output is `current-plan.json` and `current-plan.md`. The JSON retains the entire
authored source and both original registry objects, with exact input-byte SHA256
bindings. Registry records are preserved separately, without reconciliation or
promotion. Existing product acceptance remains in the authoritative execution
registry; this tool neither revokes nor re-certifies it. The Markdown is a compact
index of every task; acceptance text and complete context remain in JSON.

All derived task authored statuses are `pending`. Empty evidence and evaluation
arrays mean that this projection creates no qualification. Dependency targets
that are product tasks retain `domain-accepted` and their acceptance requirement
IDs; lifecycle targets use `execution-complete`. These predicates describe
requirements, not satisfied gates. The output is a local projection format, not
a claimed Wazi export. `cmd/portableplan` remains the Wazi exporter.

## Validation boundary

The current path checks schema/contract identity, the full retained ID inventory,
unique task IDs, single authored product representation, product identity,
nonempty title/stage/acceptance, duplicate or dangling dependencies and cycles.
It preserves unknown stage labels. The adopted ID set is independently pinned in
`check-plan.py`: removing a task and its retention entry together, or replacing
an ID while keeping the same count, fails. Future inventory changes require an
explicit reviewed update of that boundary, not automatic acceptance of a smaller
input. This pin protects retention, not source authenticity or task content.

Both registries are required. Unknown registry task IDs, malformed product
status, missing reviewed product acceptance records and mismatched journal task
identities fail. Receipt contents are retained without authenticating review,
commands, claims or deployment evidence. No status text, including `ACCEPTED` or
`COMPLETE`, becomes qualified evidence. Explicit non-pending authored status is
rejected. Projection checking compares both files against newly derived bytes,
catching altered statuses, missing IDs and stale source or registry digests.

Inputs are limited to 16 MiB each; duplicate JSON keys and non-finite JSON numbers
fail. Graph traversal is iterative. This bounded checker does not restore the
old whole-doc link/public scan, product wave scheduling/ownership validation,
contract section checks or terminal-release coverage assertions. Those checks
must not be inferred from its pass message. The existing whole-repository public
scan has 44 reported pre-existing findings; this change does not claim a passing
whole-repository scan.

## Release tooling remains unqualified

`python3 scripts/check-release-evidence.py` validates the current source and then
exits 1 with an explicit blocker: full current-source release coverage and receipt
qualification are not implemented. Invalid or absent current input exits 2.
`--strict`, `--structure-only` and `--self-test` cannot silently choose the retired
baseline. No historical matrix can qualify the current plan.

Retained historical parser checks require `--historical-product` together with
`--structure-only` or `--self-test`. Their success is labelled historical-only;
strict historical release qualification is blocked. The retained evidence-check
helpers are historical implementation, not an independently qualified current
release service. Migration of capability/story/profile coverage and trusted
receipt binding remains a separate assignment.

## Obtained verification

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts/tests -p test_current_plan_tooling.py -v
python3 scripts/check-plan.py
python3 scripts/render-plan.py --check
python3 scripts/check-release-evidence.py --historical-product --structure-only
python3 scripts/check-release-evidence.py --historical-product --self-test
```

The 27 regression tests pass using the actual full inventory and disposable
mutations. They cover missing/substituted IDs, retired baseline rejection,
duplicate fields, missing acceptance, dangling/repeated dependencies, cycles,
registry absence/foreign IDs, narrative non-promotion, altered projection status,
input-byte staleness, safe rendering and current release refusal. The initial
cycle fixture did not construct a cycle; it was corrected to an actual self-cycle
and the full suite then passed. A further regression rejects reclassifying a retained
product task as a lifecycle task. These are tooling checks only. No provider,
production, full-build, independent-review or merged-state qualification follows.
