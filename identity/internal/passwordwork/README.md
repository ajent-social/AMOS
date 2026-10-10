# Native bounded password work

`Verify` and `Hash` reuse `identity/password` with the original context, including
its private transport budget. They require a deadline and admit at most two
computations across both operations. Cancellation returns zero output; an already
running Argon2 computation retains its slot until it actually exits. Exhaustion
fails busy, with no queue or fallback. A returned rehash is only a proposed
encoding, never evidence of persistence or authenticated identity.

This internal component is being integrated under the
[B1 contract](../../../docs/contracts/current-password-work.md). Native writer
callers, database lock release, W1 qualification and composed host admission are
separate gates. It does not stop CPU work or promise hard real-time scheduling.
Configured budgets and blocklists remain trusted native dependencies.

Pure tests cover real Argon2 and deterministic cancellation/capacity barriers.
`TestPasswordWorkRuntimeRequiredService` requires the operator-provisioned TLS
SERVICES profile through `AMOS_SERVICES_RUNTIME_TEST_CONFIG`; missing prerequisites
fail visibly. It creates only uniquely scoped test person/session/rate-limit rows
and removes those exact rows. Its synthetic session setup qualifies the real
protection guard's one-use and ordered password-change budget, not a credential
producer or a full authentication journey. No test provisions schema or owns a
container.
