# ADR043: build diagnostics from a finite public vocabulary

Status: proposed for independent review; adopted on design landing.

Adopt T14.1's [public diagnostic profile](../contracts/upstream-diagnostics.md)
and closed wire schema as amendmentv1.29. Default-off reporting uses a finite,
reviewed public component baseline, fixed error/signature/frame identifiers and
bucketed counts. No raw log, source, metadata extension or private symbol can
become a default export field. Unknown/private frames are dropped whole.

The component manifest covers selected public code bytes and excludes this
registry, avoiding self-referential release pins. It is not an installed-build
attestation; future trusted provenance must establish the executing match.
Separate strict finite-profile checks supplement standard JSON Schema bounds.

The privacy ceilings and owner controls are requirements for later construction,
spool/export and collector work. This design includes rejected fixtures and a
finite profile checker, not a sanitizer, sender, consent system or deployment.
No AMSL baseline, external destination or transmission is approved.
