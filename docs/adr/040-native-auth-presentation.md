# ADR 040: expose optional native password-page presentation

Status: proposed; independent design review and landing precede source.

The generic renderer/override packages are accepted components, but the composed
native password pages do not consume them. Consumers currently cannot brand the
working credential journey without duplicating handlers or rewriting responses.

Adopt the bounded additive [v1.26 contract](../contracts/auth-presentation.md):
optional copied brand text and canonical local logo/stylesheet paths, escaped
native markup and self-only presentation CSP, propagated through the local host.
Keep native action/challenge/security behavior and nil-theme output intact.
This gives consumers CSS/asset ownership with no password or authority logic.

This deliberately does not introduce arbitrary HTML/template replacement into
credential pages. Complete page replacement remains a separately versioned public
renderer contract. Production composition, return routing, workspace authority,
organization lifecycle and Desktop delegation retain their own prerequisites.
Historical T6.14 acceptance is preserved; this is a supplemental integration
repair with its own exact design/source review and evidence.
