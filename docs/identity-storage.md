# Identity persistence boundary

`identity/store` is a transaction-scoped PostgreSQL adapter for the frozen
`amos-contract-v1` identity contract and the approved [identity policy](contracts/identity.md).
Construct it from the `*sql.Tx` supplied to `storage.DB.WithTx`. Do not retain a
store after its callback returns. A persistence error must escape the callback
so the account, session, binding, or challenge operation rolls back as one
transaction.

## Records and uniqueness

The additive migration proposal is [`migrations/fragments/identity.sql`](../migrations/fragments/identity.sql).
The integrator assigns its global sequence when composing the migration
registry. Its tables persist people and lifecycle state, email contacts,
credential verifiers, exact external identity bindings, digest-only sessions,
and purpose-bound challenges. Person IDs are immutable UUIDv7 values; the SQL
checks the UUID version and RFC variant for IDs rather than generating IDs in
the database. `store.NewID` uses the pinned `github.com/google/uuid` UUIDv7
implementation.

An email comparison key is created inside the store using the T3.1 rule:
single ASCII mailbox input, lower-case ASCII local part and domain, with no
plus-tag or dot alias collapsing. Its unique constraint is scoped by both
installation and application. The original address is stored separately for
display. Verification is a timestamp on that contact, not an identity key.

External bindings use a unique provider-connection, exact issuer, and exact
verified subject tuple under the database `C` collation. This package accepts
only an already-verified binding; it performs no provider callback validation
and does not search or link by email. Conflicts are returned as stable typed
errors (`ErrEmailAlreadyUsed` or `ErrExternalIdentityBound`) without returning
database diagnostic text.

## Sensitive values and replay

`CreatePendingAccount` stores only an Argon2id v=19 verifier using the approved
64 MiB, three-iteration, one-lane encoding, plus a 32-byte digest of the email
verification secret. Raw passwords and raw challenge secrets are not accepted
or stored. Challenge rows bind person, contact, and purpose. `ConsumeChallenge`
atomically sets `consumed_at` only for a matching digest, purpose, and
unexpired challenge, using PostgreSQL transaction time; replay, wrong purpose,
and expiry share `ErrChallengeUnavailable`. Email verification callers consume
the challenge and mark its returned contact verified in the same transaction.

Sessions store only a 32-byte token digest, installation/application/environment
scope, one of the contract's authentication method identifiers and proof time,
and the account's security epoch.
Creation checks that the person is active, matches installation/application,
and that the supplied epoch remains current; absolute expiry is bounded to 12
hours and idle expiry slides within a 30-minute window. `FindActiveSession`
checks scope, revocation, both expiries, active account state, and epoch equality
in its SQL update/query. Incrementing the epoch invalidates every prior session
without retaining raw tokens. Callers must still resolve current membership
and grants separately at the authorization boundary.

## Test boundary and limitations

The integration tests build a one-fragment registry inside the test package's
random PostgreSQL schema; they do not assign production migration order. They
cover duplicate and concurrent email creation, rollback after the person
insert when the email uniqueness check fails, challenge single use, session
epoch/revocation behavior, and exact external binding uniqueness. Run them with
`AMOS_TEST_DATABASE_URL` pointed at a disposable PostgreSQL database:

```sh
go test -race ./identity/store -run '^TestT3_2_' -count=1
```

This task does not implement password verification, provider verification,
email delivery, authentication routes, account-state transitions, recovery,
or production migration numbering. Those responsibilities remain with their
own tasks. The schema and tests do not establish deployment, provider, or
production-security qualification.
