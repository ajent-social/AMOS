# ADR 021: Current workspace and extension composition profile

Status: accepted for implementation and design qualification.
Date: 2026-10-02.

## Decision

The finite shared application composition surface owns GET/POST /workspaces.
The SQL-backed selector resolves current person/security epoch, workspace state
and personal ownership or active membership in an immutable deployment scope.
The browser hint never grants authority; each protected resource request must
resolve it through workspace/context. Responses containing tenant state are
private/no-store and browser history must revalidate authority. Native local
authentication/CSRF guards protect selection mutations.

The current supported extension design profile consumes identity.Principal's
immutable person Actor, deployment IDs, security epoch and current assurance.
Workspace authority is a separate current workspace/context.Selection, not
inferred from a principal getter or browser input. Machine principals and a
callable shared policy evaluator are unavailable until their owning tasks qualify
them; extensions fail closed when required dependencies are absent. This
profile reconciles the staged public APIs without introducing public credential
construction or reinterpretation of the eventual frozen actor/grant model.

The integrator adopts internal/amosgen/business/** as managed consumer output
for T12.2, conditional on journal preimages and authored-path separation.
The stable unsupported extension feature error is extension.unsupported_feature
(HTTP 422), preserving request-ID and safe-message requirements. Its mapping
is a design requirement until the transport generator is qualified.

Shared policy operation/requirement names reject ASCII controls. This corrects
JSON Schema engines' terminal-newline anchor behavior to match the intended
canonical public identifiers; the business descriptor retains its stricter
bounds and no authority-bearing string may be normalized into a new identifier.

## Evidence boundary

Design fixtures and route-composition checks qualify the design seam only.
Real PostgreSQL and browser checks separately qualify workspace composition.
No generated extension registry, policy engine, machine credentials, provider
operation or production deployment is implied.
