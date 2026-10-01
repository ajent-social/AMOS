# AMOS

AMOS is an open-source, self-hosted foundation for building and operating paid SaaS applications that people and AI agents can use.

Each application owner runs their own installation. AMOS supplies the shared account, workspace, billing, API and operational capabilities so developers can focus on their business features.

## The intended developer experience

```sh
amos init
amos deploy
```

These commands are the target workflow, not currently available getting-started instructions. The initializer will generate a Go application, default UI, GitHub Actions workflows and infrastructure as code. Deployment will bring the application online at its own domain.

AMOS will handle shared routes such as `/signin` and account settings, while the business application owns its landing page and business routes on the same public domain. The default integrates business code into the Go application; a separate-service option will support other stacks.

## Planned capabilities

- Personal and organization workspaces, invitations, roles and account administration.
- Email/password, magic links, social login, passkeys, enterprise OIDC and MFA.
- Flat-rate, seat-based and usage-based subscriptions through a provider abstraction, starting with Stripe.
- Web interfaces and APIs, with MCP generated from OpenAPI so independently operated agents can connect. Mobile, SMS and end-user CLI clients come later.
- AWS deployment with required Cloudflare integration, offering managed containers/database and a small virtual machine profile.
- Deployment, monitoring, backups, recovery, automated upgrade pull requests and controlled autonomous maintenance.

Application code and Pulumi infrastructure programs use Go. The default UI uses server-rendered HTML, HTMX and plain JavaScript/CSS, and is replaceable. AMOS enforces application authorization; external agents retain their own governance.

## Implementation status

AMOS is under active development. It is not yet a complete runnable SaaS or a qualified production deployment.

Implemented foundations include real PostgreSQL test infrastructure and transactional migrations; configuration and policy validation; durable jobs and an outbox; email delivery and audit components; identity storage, sessions, password hashing, email verification and signup/sign-in components; workspace persistence, personal workspace bootstrap and request context; OpenAPI validation and Go code generation; a tenant-scoped reference todo service; the default UI renderer and protected template overrides; atomic initializer generation; and scoped billing intents and persistence.

These components still need to be assembled into the complete application. End-to-end account flows, organization administration, subscription processing, generated applications, deployment commands, cloud infrastructure, MCP, upgrades and operational recovery remain to be completed. Local tests and provider fixtures do not qualify live services.

See the [current roadmap](docs/roadmap.md) and [execution record](docs/planning/execution-state.json) for task-level progress.

## Development and architecture

- [Development prerequisites](docs/development.md)
- [Migration behavior](docs/migrations.md)
- [Vision](docs/VISION.md)
- [Architecture RFC](docs/rfc/rfc-0001.md)
- [Complete staged implementation plan](docs/plan.md)
- [Contributor/session resume guide](docs/RESUME.md)
- [AMSL reuse strategy](docs/planning/amsl-boundary.md)

## License

Original AMOS code is licensed under [Apache 2.0](LICENSE). Third-party licenses and notices are preserved in [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES.md).
