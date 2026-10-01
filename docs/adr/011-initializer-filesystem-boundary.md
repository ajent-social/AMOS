# ADR 011: Initializer filesystem ownership boundary

Status: Accepted

## Decision

Initializer template version 1.1 binds each resumable stage to its filesystem
identity in the journal. Resume validates that identity, configuration, target,
parent and versions before performing recovery mutations.

On supported Darwin and Linux hosts, finalization walks the complete canonical
parent chain through descriptors without following symlinks. Each component
must be owned by the current user or root. Group or other writable ancestors
are rejected except sticky directories whose selected child is owned by the
current user or root. The final destination parent must belong to the current
user and must not be group or other writable.

The stage must retain its original identity, current-user ownership and mode
0700. Finalization changes its mode through the pinned descriptor, verifies the
source entry, publishes through an exclusive no-replace rename, and synchronizes
the parent. An existing destination is never replaced. Unsupported platforms
fail explicitly. A sync failure after publication reports that publication
occurred; callers must not retry as though no files were created.

## Consequences

Callers must choose a secure canonical destination. Shared writable storage
roots can be rejected even when a child directory is private. AMOS does not
change existing parent permissions to make them eligible. Root-directory
creation is unsupported. Same-user hostile processes are outside this boundary.
Old template journals are rejected rather than silently upgraded.

## Evidence

Real filesystem tests cover source substitutions, insecure parents and
ancestors, symlink ancestors, existing destinations and successful publication.
A recovery-order mutation deleted authored temporary journal material and was
detected; restoring validation before recovery passed the regression.
Independent source review found no remaining blocking filesystem issue.
