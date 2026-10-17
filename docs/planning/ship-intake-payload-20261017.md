# Strict intake payload component

Requested ceiling25; actual member1 bounded T14.4 source component. The inherited
engineering session selection is unchanged. Model name, token/cost and exact stage
duration telemetry are unavailable. One implementer and one independent reviewer
are used; no change of provider or billing mode is inferred.

| Batch | Outcome/member | Prerequisites | Ownership | Verification and delivery |
|---|---|---|---|---|
| intake-payload | T14.4 strict bounded wire decoder component | Adopted v1.37 and existing finite diagnostic profile | `internal/upstreamintake/payload/**` only; root owns records | Actual byte/header boundary, deterministic corpus, bounded fuzz, compiled negative/restoration, race/vet/lint, independent exact-head review, guarded merge and fresh landed scope |

## Boundary

The component accepts bounded bytes and parsed HTTP metadata, rejects ambiguous
headers/length/encoding, enforces both8192-byte caps and exactly one valid gzip
member with no trailing bytes, reuses the finite diagnostic validator and returns
fresh canonical data. A recognized baseline is not proof of executing provenance.
No network reader, timeout goroutine, receipt, storage, budget admission, witness,
collector route or publication capability is added. Listener framing/header/time
bounds and durable-attempt ordering remain the future endpoint's obligations.

Base: `17130246b58aaffda1887236361e29240e4b6414`. Root task claim:
`9aea236447be484143d4aa91571b31095002e8dc`; integrator claim:
`911d7984825f2d73685aa3cc245838f71b7c5928`. The authored1607-task graph and
original product scope stay unchanged. T14.4 and source preflight remain
IN_PROGRESS/NOT_RUN; no original implementation/review stage is falsely completed
by this component. Actual independent review attribution will be recorded.

## Checks and limitations

Actual source and scoped checks are recorded below. This pure decoder needs no database or
provider service; it cannot qualify those missing durable-intake authorities.
Shared Mac load exceeds the build threshold; bounded existing Linux capacity will
run the heavy checks. No deployment or external collection is authorized here.

## Review map

Production changes are confined to the new payload package. Review the metadata
case/duplicate/length checks, gzip single-member EOF/trailing guard, reuse of the
finite profile validator and fresh canonical-copy boundary. Deterministic cases
cover equivalent JSON, exact caps, malformed encodings and generic no-echo errors;
fuzz checks bound output, canonical stability and copy isolation. The compiled
negative removes the trailing guard and must fail its actual corpus case before
exact restoration. No synthetic provider fixture is used as service evidence.

Independent component reviewer `generation_review` holds reservation
T-SDLC-14-4.4, `8bbacbc8ca7a454d3511888956fff9b06c6adda2`. This attributes the
component review without marking the original complete-task review stage done.
Final exact-head approval remains pending.

## Verified candidate

Source `7140126e297a59093d1fd903bfd8feb8f91f1130` passes Go1.27.1 Linux ARM64
scoped race checks:64 named cases including subtests and fuzz seeds, seven
top-level, zero failures/skips. One30second single-worker `FuzzDecode` run
completed1,207,085 executions without failure. These are bounded corpus/fuzz
observations, not proof for all possible inputs.

The actual compiled trailing guard was changed to an impossible comparison.
`TestPayloadGzipRejectTrailing` failed its garbage, second-report and empty-second-
member cases. Exact source restoration compares cleanly with Git, and all64 named
race cases pass again. Scoped vet passes. Golangci-lint2.13.2, checked against its
release checksum before execution, reports zero issues.

The initial compressed-bound test fixture used a gzip comment longer than the Go
reader permits; it was corrected to a legal FEXTRA field that actually reaches
the8192-byte boundary. Production decoding and its cap were not weakened. A README
relative link was corrected before final checks. No linter waiver or skip was added.

Independent exact-head full candidate review, guarded merge and fresh landed
behavior remain pending. Full T14.4 and external collection remain unaccepted.
