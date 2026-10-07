# Email and MFA transaction constructor follow-ons

These supplemental v1.16 leaves retain legacy APIs and authentication operation
behavior. They grant no production host, provider or operation-executor authority.

## Email

[PR38](https://github.com/ajent-social/AMOS/pull/38) landed independently reviewed
candidate `fbfdcffc67d16eccbe27e1a539c4ae9165f1595f` as
`22275ac9a445ff11f6bd5d32fe565c29873591fe`. Reviewed base was `c3caa036f89101cd336535a9ae2dc92f32fb6bd1`;
immediate live base was `f599c0dedbe67fbb28e286c444d6c9564a433e73`. Intervening changes
were reviewed records and separate MFA files. All three landed files equal the
reviewed source; original author `67ebbdde98ff1b50f1c755c3a6d95277ac7f6d76`
and separate correction commits remain preserved.

Independent scoped normal/race each passed 19 entries, with vet/build and pinned
lint passing. Nil/options mutations failed intended assertions and exact
restoration passed. Review found that cancellation returned before commit and did
not prove commit-error handling. A separate author added a test-only completion
failure: successful SQL verifies staged challenge/job/material writes, rolls back
the real transaction, and returns nil so the runtime attempts commit. It requires
a transaction error with a live context, no acknowledgement and no persisted rows.
Different final review cleared the source and required actual TLS PostgreSQL
normal/race each passed one comprehensive test in the same database.

The first completion mutation matched an earlier joined cancellation error and
was not counted as completion evidence. A coordinator who authored neither email
source nor correction independently narrowed it to direct transaction-error
equality with a live context. The intended completion acknowledgement assertion
failed, and exact-source restored service passed. Fresh landed required-service
verification passed its one test, with no skips/failures or cleanup errors.

## MFA

[PR39](https://github.com/ajent-social/AMOS/pull/39) landed reviewed candidate
`435c78b8fa8af06a9543c78d5fd3055c824d1f48` as
`f599c0dedbe67fbb28e286c444d6c9564a433e73`. Reviewed base was `c3caa036f89101cd336535a9ae2dc92f32fb6bd1`;
immediate live base was `b159639928b77e96c36078262ac396d54da43b00`, adding records only.
All three landed files equal reviewed bytes; original author
`9011aa7a9439dce4df7e7bf94090e334bdaaeab7` remains preserved.

Independent selected normal/race each passed 22 entries; vet/build and pinned
lint passed. Nil/default/options mutations failed intended assertions and exact
restoration passed. Actual dedicated TLS PostgreSQL normal/race each passed four
entries, with zero skips/failures. A staged-credential output-suppression mutation
produced the intended HTTP 200 instead of 503 after real callback rollback;
exact restoration passed all four required entries. Fresh landed service also
passed four entries, including bounded exact UUID cleanup. The suite uses real
session/protection/password/vault components with explicitly synthetic policy
and test setup; it does not qualify all authentication or production policy.

## Runtime prerequisite and remaining work

Finite email and MFA profiles received different exact-source review, offline
negative/restored checks and actual local TLS/schema/role/DML/denial checks.
Email binds identity, jobs and material to one database. Required absence failed
visibly before compatible profiles existed. No provider or hosted CI success is
claimed; source expressions in public scans are classified without inventing an
automated full-scan pass.

Login and recovery constructor leaves remain to implement under v1.16. Magic-link
requires a separate constructor assignment. Current-session renewal timing,
refreshed-principal handoff, complete writer lock order and producer freshness
remain amendment gates before executor dispatch. Production database roles,
mail/provider credentials, budget, DNS and deployment remain separate decisions.
