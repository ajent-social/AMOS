# ADR 024: Explicit codec schema binding

Status: accepted for implementation after independent exact-head review and merge.
Date: 2026-10-06.

## Problem

The landed v1.12 operation contract requires rejection of schema-digest mismatch,
but its codec interfaces expose no schema identity. Rehashing caller metadata
cannot establish whether an input or output codec matches that metadata.

## Decision

Semantic amendment v1.13 adds `SchemaDigest() [32]byte` to both typed codec
interfaces. An immutable codec reports SHA-256 of its exact canonical schema
artifact. `Bind` rejects zero or unequal metadata/codec digests before creating a
definition, then snapshots the accepted metadata. Schema artifact generation and
canonicalization are separately qualified generator responsibilities.

This checks binding consistency between trusted implementations. It is not a
sandbox or proof that a dishonest codec implements the declared schema. Codecs
must remain immutable after binding; source review and codec/generator tests
qualify that behavior. Existing revision, replay scope and conflict rules remain.

## Compatibility and ownership

This is a minor semantic design increment because both codec interfaces gain a
required method. There is no landed operation codec implementation to migrate.
No SQL, migration, wire envelope or durable replay data is changed. The T2.8
worker owns implementation under `app/operation/**`; the integrator retains
policy, module, generator, storage and executable composition ownership.

## Acceptance

Test valid binding, zero metadata and codec digests, independent input and output
mismatches, and copied metadata isolation. A genuine negative mutation must show
a mismatch test failing when its binding check is disabled, followed by restored
passing verification. Runtime executor, real database and transport acceptance
remain separate; this design does not enable dispatch.
