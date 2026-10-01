# AMOS roadmap

Updated: 2026-10-01. Public implementation status.

## Current status

The reviewed foundation is merged into `main`. There is no released, complete runnable SaaS and no qualified production deployment yet.

The execution record contains 46 accepted tasks out of 257 planned tasks. Atomic initializer generation, signup/sign-in components, default reference browser flows, billing persistence and replaceable UI overrides are reviewed. Task counts measure accepted work items, not percentage of product completion. Consult the [execution record](planning/execution-state.json) for current evidence.

## Integrated foundation

- PostgreSQL test infrastructure, transactional migrations, configuration/policy validation, dependency provenance and Apache 2.0 licensing.
- Durable jobs and transactional outbox, email delivery boundaries and tenant-scoped audit persistence.
- Identity storage, session/CSRF boundaries, password hashing and email verification components.
- Workspace storage, atomic personal workspace bootstrap and current request workspace resolution.
- OpenAPI validation, typed Go generation, shared/business routing boundaries and a durable tenant-scoped reference todo service.
- Default server-rendered UI shell with browser regression checks, including HTMX fragment swapping.

These are integrated components, not a completed end-to-end product. Required SQL checks use real PostgreSQL; local browser tests exercise the renderer. Provider fixtures do not establish live provider readiness.

## Next working-app milestone

1. Finish local application templates and runtime composition; wire the developer executable.
2. Assemble signup, verification, sign-in and personal workspace flows into the default web app.
3. Connect the reference business feature through the shared route and authorization boundaries.
4. Verify a generated application with real database and browser checks.
5. Add subscription processing and entitlement enforcement, then qualify deployment on AWS with Cloudflare.

A generated runnable application is the next milestone; the full identity, billing, agent and operating architecture remains in scope.

## Remaining product work

- Complete account flows, recovery, social providers, passkeys, enterprise OIDC and MFA.
- Organization creation, invitations, role management and account administration.
- Stripe subscription processing, seat/usage accounting and enforced entitlements.
- Generated repositories, business extension templates and `amos deploy`.
- Go Pulumi infrastructure for managed and small-VM AWS profiles, Cloudflare and CI/CD.
- OpenAPI-generated MCP with the same current application permissions as other interfaces.
- Monitoring, backups, tested recovery, explicit profile migration and release qualification.
- Automated upgrade pull requests and bounded autonomous maintenance with independent verification and release controls.

## Complete staged delivery


- M0 / E1: contract, provenance and real-boundary harness foundation. Owner: integrator.
- M1 / E2-E7: local generated personal paid-app reference. Owners: runtime, identity, tenancy, billing, UI and CLI lanes.
- M2 / E8-E10/E16: managed cloud, provider test-mode and baseline recovery qualification. Owners: platform and integrator.
- M3 / E3-E6/E11-E12: full selected identity, teams, billing models, generated MCP and alternate business service. Owners: application and interface lanes.
- M4 / E7-E10/E14-E15: both-profile operating evidence, explicit profile migration, upgrade PRs and independent security gates. Owners: platform, upstream and reviewers.
- M5 / E13-E16: bounded private/upstream autonomous maintenance and full release acceptance. Owners: maintenance, security and integrator.

## Qualification gates

Live AWS, Cloudflare, email, billing and identity integrations still require real owner/provider prerequisites and obtained test evidence. Backups must demonstrate restoration; deployment and release readiness must be verified separately from local component tests. No deployment date or production-readiness promise is implied by this roadmap.

See the [complete plan](plan.md), [open decisions](planning/open-decisions.md) and [resume instructions](RESUME.md).
