# Password sign-in credential freshness evidence

The bounded correction was independently reviewed at `fca24649b442d1563dc2ea410679eababed0b69d` and landed through [PR76](https://github.com/ajent-social/AMOS/pull/76) at `09d441ba4cadae9b2658f4673b1a4e0e7d78dfce`. All three source blobs match the reviewed head. The [v1.25 design](../contracts/password-signin-freshness.md) defines this prerequisite; it does not adopt the complete session writer protocol.

Password verification remains one operation. The result is bound to exact person, contact and credential values. A bounded writable READ COMMITTED transaction separately locks and revalidates those rows, samples database time after acquisition, and stages session issuance/rotation in the same transaction. Cookie, CSRF and success are published only after successful completion. Optional rehash remains a separately committed best-effort CAS; it is not claimed atomic with issuance. Duplicate configured cookies now follow the existing transaction helper's generic unavailable mapping.

## Actual independent checks

The independent reviewer ran `go test -json ./identity/login -run 'Test(Login(RuntimeRequiredService|RuntimeConfigFormat|TxRunner)|SigninFreshnessRequiredService)' -count=1 -timeout=3m` against real TLS PostgreSQL with the reviewed runtime role. Race added `-race` and used `-timeout=12m`. Normal, race and exact-restored runs each passed 79 test/subtest events plus the package event, zero failures or skips. Race completed in 339.124 seconds; normal in 41.58 seconds. Scoped vet and pinned lint passed. Fresh landed normal passed the same 80 events with zero failures/skips, and the exact owned fixture was cleaned up afterward.

Checks exercise unchanged and stale person/contact/credential values after separately observed row waits; cancellation at each wait; actual competing writers blocked by held rows; same-epoch changed hash/contact; optional rehash success, loss and failure; generic credential denials; prior-cookie scope/rotation; actual unsupported transaction modes, rollback and failed completion. Success authenticates through the real session middleware. The preexisting required-service suite includes native signup and explicit confirmation.

The reviewer independently reconstructed the original detached-issuance implementation from the adopted baseline and a mutant omitting exact credential/contact comparisons. Both failed the intended same-epoch changed-hash and changed-contact assertions: HTTP200 where generic401 without provisional output was required. Neither compilation failure nor an absent barrier counted as a negative. Exact patched source bytes remained unchanged and the full restored service selection passed.

An earlier independent race run was intentionally interrupted and an earlier normal run hit a 40-second test timeout. Neither is a pass; their retained failure records are superseded for qualification by the complete fresh runs above. An initial lint invocation used a missing executable path; the corrected pinned invocation passed. No timeout or failed invocation is hidden as successful evidence.

## Captured log digests

| Check | SHA-256 |
| --- | --- |
| Independent normal | `d9a41253c5dc32ab74a3a1e2eda1d97be3759a0277c18f5c72cea2e0d47d8e13` |
| Independent race | `e38d4ba8438ce36fb52a370d51f933ffa3bb49bc976d140f5e3397d092430a42` |
| Negative original | `3a13cefc75c2ad07d454d7a4c150bd1a10367cd39396148c29a85e8f7c825431` |
| Negative epoch-only | `7eb4be7a9d8c8cdcf6a3ab8ad00f549b84725aea4a7887ff5e61c22efb73823b` |
| Exact restored | `60c0ed5ec9df1319b41dad4bc45cf092d576dcf0967c3d6bcff597030f5e3862` |
| Scoped vet | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| Pinned lint | `e92606b0bf483111dff0a120c315ea165821348f31365020e2468a0059095c47` |
| Fresh landed normal | `ecf9436b5ee5925e81951587df3ea80bf4ad95f879fd20f6e9dee46cc5ee7fbd` |

## Limits

These are selected local service checks, not hosted CI, complete-package legacy administrator-DSN tests, live providers or deployment. Tests use disclosed forwarding transaction adapters: barriers precede real issuance, and a completion-failure adapter rolls back before the real runtime commit reports its transaction error. This does not prove physical network/WAL unknown-commit behavior or deferred-completion freshness. Synthetic active identities are inserted with exact-owned runtime DML; the original service suite separately covers native signup/confirmation.

Whole-writer ordering, current-session provenance, refreshed workspace/resource authority, HTTP/SSR composition and full replacement qualification remain open. The full public scan's preexisting findings are not cleared or suppressed; production/new-test scoped scanning passed, while the existing runtime test retains three unchanged baseline findings. Existing accepted product count remains59 and the current plan1598; original1501 task retention, full SaaS/billing/generator/provider/portability/release scope and foreign ownership remain intact.
