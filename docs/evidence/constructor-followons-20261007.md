# Email and MFA transaction constructor follow-ons

These supplemental v1.16 leaves retain legacy APIs and authentication operation
behavior. They grant no production host, provider or operation-executor authority.

## Email

[PR38](https://github.com/ajent-social/AMOS/pull/38) is staged at corrected candidate
`fbfdcffc67d16eccbe27e1a539c4ae9165f1595f`, preserving original author
`67ebbdde98ff1b50f1c755c3a6d95277ac7f6d76`. Initial independent source review
passed normal/race (19 entries each), vet, build and pinned lint; nil/transaction
option mutations failed intended assertions and exact restoration passed.

Review found that the cancellation case returned before commit and did not prove
commit-error handling. A separate author added a test-only completion failure:
a successful SQL callback verifies staged challenge/job/material writes, rolls
back the real transaction, and returns nil so the runtime attempts commit. It
requires a transaction error with a live context, no acknowledgement, and no
persisted scoped rows. Different corrected-head review and actual same-database
required-service verification remain pending. No merge or landed claim yet.

## MFA

[PR39](https://github.com/ajent-social/AMOS/pull/39) is staged at candidate
`435c78b8` from original author `9011aa7a9439dce4df7e7bf94090e334bdaaeab7`.
The three-file slice preserves legacy configuration and operation bodies while
storing only a transaction capability and database-free configuration.
Independent selected normal/race each passed 22 entries; vet and pinned lint
passed. Nil/default/options mutations failed intended assertions and exact
restoration passed. Actual dedicated required-service verification remains
pending; no merge or landed claim yet.

## Runtime prerequisite and remaining work

The new finite email and MFA profiles have different exact-source review,
offline negative/restored checks and actual local TLS/schema/role/DML/denial
checks. Email binds identity, jobs and material to one database. These fixture
probes are prerequisites, not service or provider qualification. Required-service
absence failed visibly before a compatible profile was available.

Login and recovery constructor leaves remain to implement under v1.16. Magic-link
requires a separate constructor assignment. Current-session renewal timing,
refreshed-principal handoff, complete writer lock order and producer freshness
remain amendment gates before executor dispatch. Production database roles,
mail/provider credentials, budget, DNS and deployment remain separate decisions.
