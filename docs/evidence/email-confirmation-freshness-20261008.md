# Email confirmation post-wait freshness evidence

The bounded confirmation correction was independently reviewed at `2ef8798e1994ee0105f178cf9a11b48379a48996` and landed through [PR71](https://github.com/ajent-social/AMOS/pull/71) at `7fb8f4d932fdff57e8a341b715cc666cae754985`. All four changed file blobs match the reviewed head. The adopted design is [email confirmation freshness](../contracts/email-confirmation-freshness.md). The original worker candidate PR69 is preserved and superseded by the combined integration.

Confirmation explicitly uses READ COMMITTED. It locks the bound challenge, applies guarded contact/person updates, then invokes the shared challenge consumer last. Expiry is checked with database time after waits; a denial rolls back earlier writes. Success is returned only after transaction completion. The separate integration correction updates the legacy transaction-options assertion to require the adopted isolation.

## Actual selected checks

The independent reviewer executed real TLS PostgreSQL normal, race and restored runs of `go test -json ./identity/email -run '^(TestConfirmationFreshnessRequiredService.*|TestEmailTxRunnerOptionsAndErrors)$' -count=1 -timeout=90s` (race adds `-race`). Each passed 32 test/subtest events plus one package event, with zero skips. A fresh landed normal run passed the same 33 events, zero skips. Scoped vet and pinned lint passed. The source author's narrower normal/race/restored selection passed 31 test/subtest events plus one package event; its historical legacy-options assertion failure was corrected and independently rerun in the combined head.

Wait tests cover challenge, contact and person locks, expired/live outcomes, cancellation and statement timeout. Denials cover binding, state and replay; concurrent confirmation permits one success. Snapshots verify rollback and successful cases check post-wait consumption time. Completion adapters include intentional isolation/read-only substitutions and injected rollback/commit failure outcomes; these do not prove naturally occurring unknown-commit behavior.

The reviewer independently extracted the original inline implementation and separately moved shared consumption before guarded writes. Both overlays failed the intended expired-wait assertion: confirmation succeeded when challenge unavailability was required. Neither setup failure nor compilation failure was counted. Exact source remained unchanged, and a restored real-service run passed.

## Reproducible receipt digests

| Check | SHA-256 of captured log |
| --- | --- |
| Independent normal | `8e694f7e8dbacc828efd5aa45947a206f3755823f68a1dd89f27d7241f23c6e0` |
| Independent race | `113942c03217174579936f8f36229777a81dbcd19dc626c9a7b330025806f7b6` |
| Independent negative-original | `eff1621f31ea06879a9a7f5c6d3bb813cc8f178dc3ca9a94d099c994698271ca` |
| Independent negative-early | `4f74719f5733a4da8c20b1bb3a6205bdecab8711582b8f3b3bc7737118c203e8` |
| Independent restored | `0d0d21fe86a3adfd9173dcc08c73da179b4adb728ca5e86f3e4c9b675a9e1fa7` |
| Independent vet | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| Independent lint | `e92606b0bf483111dff0a120c315ea165821348f31365020e2468a0059095c47` |
| Fresh landed normal | `aabd9512f8513af7bdc146ab64572f64ed7548063a5965097cb7e0809e5cb2f5` |

## Limits

These are selected local service checks, not hosted CI or a full-suite claim. The existing challenge/contact/person lock chain is preserved; full producer and writer compatibility, deferred workspace triggers, session provenance, refreshed principal and resource authority remain open. Scoped public scanning passed; no full-repository green claim follows. Mail delivery, provider activation, HTTP/SSR hosting, rehearsal and product acceptance are not qualified. The canonical plan remains 1592 tasks and accepted product count remains 59.
