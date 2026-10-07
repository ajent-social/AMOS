# ADR 030: private handler completion boundary

Status: proposed. Supersedes no public operation contract.

The landed callback database guard cannot be invalidated around the existing
private invocation closure because that closure also runs output codecs.
Retained transaction handles must become unusable at handler return, including
error and panic, before any output processing.

Adopt the private two-stage binding and owner-transaction helper specified in
[proposed v1.19](../contracts/callback-invocation.md), subject to independent review
and landing. Preserve exported APIs and the existing private compatibility path;
future executor wiring must explicitly select the guarded helper. Failed cleanup
prevents output completion and requires owner rollback. Commit and authority stay
outside this prerequisite.

This makes the lifetime boundary directly testable with actual PostgreSQL while
leaving current-session authority, replay, audit/effects and transport composition
unimplemented. No raw handler, definition or helper confers operation authority.
