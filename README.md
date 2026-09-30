# AMOS

AMOS is being designed as an open-source application foundation for building and operating paid SaaS applications with humans and agents.

The target experience is `amos init` followed by a reviewed `amos deploy`: a complete owner-hosted web app with accounts, workspaces, subscriptions, APIs, generated MCP, infrastructure and operating workflows. Developers add their business features in Go or a privately routed service. Shared UI is fully replaceable; upgrades preserve authored work.

**Status: architecture and implementation plan. There is no working AMOS CLI, supported release or production-qualified application yet.** Commands above describe the intended product, not commands available today.

- [Vision](docs/VISION.md)
- [Architecture RFC](docs/rfc/rfc-0001.md)
- [Complete staged implementation plan](docs/plan.md)
- [Contributor/session resume guide](docs/RESUME.md)
- [AMSL reuse strategy](docs/planning/amsl-boundary.md)

The public record contains no private application source or customer information. Dependency and original-work licensing/publication decisions are tracked in the plan before code imports or release.

## Implementation status

The foundation now includes the Go module, real PostgreSQL test harness and transactional migrations, strict configuration/policy schemas, and public-artifact checks. The complete application, developer commands, cloud deployments, and provider integrations remain under construction. See [development prerequisites](docs/development.md), [migration behavior](docs/migrations.md), and [current roadmap](docs/roadmap.md).
