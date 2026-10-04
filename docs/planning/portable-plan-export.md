# Experimental portable plan export

`go run ./cmd/portableplan` writes a read-only JSON interchange for the frozen
portable plan contract `0.0.1` (`sha256:7582512f122d2f2a9c4461facc7541c9887053f137260d6ebe9c6dea611d039d`).
Run it from the repository root. `-plan` and `-state` may point to other local
copies of the two native inputs. The adapter version is
`amos-portable-plan/0.0.1`; consumers must pin that version and the source
digest rather than assuming later exports have identical meaning.

The authored authority is `docs/planning/plan-data.json`. Each task receives a
stable `amos:task:<native ID>` identity, keeps its native stage and full native
task object, and retains acceptance criteria in source order. Native
dependencies become `domain-accepted` dependencies on an
`amos:acceptance:<native ID>` requirement. The domain is
`amos:task-acceptance`. The source revision is the SHA-256 digest of the exact
plan file bytes, so any authored change yields a different revision.

`docs/planning/execution-state.json` contributes only narrative status and
certification metadata, with its own digest. Its `ACCEPTED` and `REVIEWED`
labels do **not** create portable execution, check, review, landing, deployment,
or domain-acceptance evidence. Every exported task has portable authored status
`pending`; `evidence` and `evaluations` are empty, and there is no execution
snapshot. A consumer must leave requirements unresolved until a separate,
trusted adapter supplies current, subject-bound receipts and evaluations under
an explicit policy revision. This command performs no admission, execution,
provider call, merge, or deployment.

The output is an interchange view, not a new writable plan master. A source
checkout may be dirty; this export makes no clean-source claim. Run the native
plan checker and the pinned portable semantic validator before relying on a
particular generated file. Structural conformance alone cannot qualify
receipts or a live journey.
