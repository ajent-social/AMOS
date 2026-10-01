# ADR 014: foundation migration composition

Status: accepted for implementation. Contract amendment v1.5.

The integrator assigns the initial immutable global order through
`migrations.Core`: identity foundation (1), jobs foundation (2), workspace
foundation (3), audit foundation (4), billing foundation (5), authentication
limits (6), protected email material (7). Application fragments start at 8. The
reference application embeds its original todo fragment as sequence 8. Fragment
SQL bytes are embedded from their existing files; business SQL is not copied into
the foundation. Future upgrades append migrations rather than rewrite these IDs,
sequences or SQL bytes. Previously isolated test registries do not constitute
released installation migration histories.

The current-person personal workspace selector requires an explicit application
realm and a current active owner with an active personal workspace. Callers must
obtain the person selector from authenticated authority; a UUID is not authority.

## Obtained evidence

Independent review found no blocker. Real PostgreSQL applied the composed core
and an application fragment twice without duplicate ledger entries and rejected
sequence collision. Current personal lookup denied foreign person/realm,
suspended resource and disabled owner. Removing the current-person predicate
made the corresponding regression fail; restored checks passed. Scoped store,
storage and migration tests passed; lint reported zero issues. Generated app and
production migration/deployment qualification remain separate work.
